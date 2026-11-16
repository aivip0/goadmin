package plugins

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
)

type storedConfig struct {
	Public     []byte
	Secret     []byte
	KeyVersion int
}
type verifiedPackage struct {
	Manifest        Manifest
	ManifestHash    [32]byte
	KeyID           string
	MigrationNames  []string
	Migrations      map[string][]byte
	MigrationHashes map[string][32]byte
}
type Service struct {
	repo      *Repository
	root      string
	keys      []ed25519.PublicKey
	configKey []byte
	hooks     map[string]LifecycleHook
	providers map[string]RouteProvider
}

type Extension struct {
	ID       string
	Hook     LifecycleHook
	Provider RouteProvider
}

// LifecycleHook is implemented by trusted, build-time extensions only. It is a pure
// preflight validator: it must not perform side effects because those cannot join the
// PostgreSQL transaction. It never receives SQL handles or filesystem paths.
type LifecycleHook interface {
	BeforePluginLifecycle(context.Context, string, Manifest) error
}

type ConfigReader interface {
	Public(context.Context, string, string) (json.RawMessage, error)
	Secret(context.Context, string, string) ([]byte, error)
}

// RouteProvider is registered in application code at build time; manifest route keys can
// dispatch only to a registered provider, never to uploaded executable code.
type RouteProvider interface {
	HandlePluginRoute(context.Context, string, string, json.RawMessage, ConfigReader) (any, error)
}

type configReader struct{ service *Service }

func (c configReader) Public(ctx context.Context, id, key string) (json.RawMessage, error) {
	records, err := c.service.repo.Config(ctx, id)
	if err != nil {
		return nil, ErrUnavailable
	}
	record, ok := records[key]
	if !ok || record.Public == nil {
		return nil, ErrNotFound
	}
	return append(json.RawMessage(nil), record.Public...), nil
}
func (c configReader) Secret(ctx context.Context, id, key string) ([]byte, error) {
	records, err := c.service.repo.Config(ctx, id)
	if err != nil {
		return nil, ErrUnavailable
	}
	record, ok := records[key]
	if !ok || len(record.Secret) == 0 {
		return nil, ErrNotFound
	}
	if record.KeyVersion != 1 || len(c.service.configKey) != 32 {
		return nil, ErrUnavailable
	}
	block, err := aes.NewCipher(c.service.configKey)
	if err != nil {
		return nil, ErrUnavailable
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil || len(record.Secret) < gcm.NonceSize() {
		return nil, ErrUnavailable
	}
	plain, err := gcm.Open(nil, record.Secret[:gcm.NonceSize()], record.Secret[gcm.NonceSize():], configAAD(id, key))
	if err != nil {
		return nil, ErrUnavailable
	}
	return plain, nil
}

func NewService(repo *Repository, root string, keys [][]byte, configKey []byte) (*Service, error) {
	if repo == nil {
		return nil, errors.New("plugin repository is required")
	}
	if root != "" {
		st, err := os.Lstat(root)
		if err == nil && (!st.IsDir() || st.Mode()&os.ModeSymlink != 0) {
			return nil, errors.New("plugin package root must be a non-symlink directory")
		}
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, errors.New("plugin package root is unavailable")
		}
	}
	service := &Service{repo: repo, root: root, configKey: append([]byte(nil), configKey...), hooks: map[string]LifecycleHook{}, providers: map[string]RouteProvider{}}
	for _, key := range keys {
		if len(key) != ed25519.PublicKeySize {
			return nil, errors.New("invalid plugin trust key")
		}
		service.keys = append(service.keys, ed25519.PublicKey(append([]byte(nil), key...)))
	}
	if len(service.configKey) != 0 && len(service.configKey) != 32 {
		return nil, errors.New("invalid plugin encryption key")
	}
	return service, nil
}

func (s *Service) RegisterHook(id string, h LifecycleHook) error {
	if !pluginIDPattern.MatchString(id) || h == nil {
		return ErrInvalid
	}
	if _, exists := s.hooks[id]; exists {
		return ErrConflict
	}
	s.hooks[id] = h
	return nil
}
func (s *Service) RegisterProvider(id string, p RouteProvider) error {
	if !pluginIDPattern.MatchString(id) || p == nil {
		return ErrInvalid
	}
	if _, exists := s.providers[id]; exists {
		return ErrConflict
	}
	s.providers[id] = p
	return nil
}
func (s *Service) RegisterExtension(extension Extension) error {
	if !pluginIDPattern.MatchString(extension.ID) || extension.Hook == nil && extension.Provider == nil {
		return ErrInvalid
	}
	if _, exists := s.hooks[extension.ID]; exists {
		return ErrConflict
	}
	if _, exists := s.providers[extension.ID]; exists {
		return ErrConflict
	}
	if extension.Hook != nil {
		s.hooks[extension.ID] = extension.Hook
	}
	if extension.Provider != nil {
		s.providers[extension.ID] = extension.Provider
	}
	return nil
}
func (s *Service) List(ctx context.Context) ([]Plugin, error) { return s.repo.List(ctx) }
func (s *Service) Events(ctx context.Context, id string, limit int) ([]Event, error) {
	if id != "" && !pluginIDPattern.MatchString(id) {
		return nil, ErrInvalid
	}
	if limit < 1 || limit > 100 {
		limit = 50
	}
	return s.repo.Events(ctx, id, limit)
}

func (s *Service) Packages(ctx context.Context) ([]Package, error) {
	if s.root == "" {
		return []Package{}, nil
	}
	entries, err := os.ReadDir(s.root)
	if errors.Is(err, os.ErrNotExist) {
		return []Package{}, nil
	}
	if err != nil {
		return nil, ErrUnavailable
	}
	out := make([]Package, 0)
	for _, entry := range entries {
		if !entry.IsDir() || !pluginIDPattern.MatchString(entry.Name()) {
			continue
		}
		id := entry.Name()
		versions, err := os.ReadDir(filepath.Join(s.root, id))
		if err != nil {
			continue
		}
		for _, version := range versions {
			if !version.IsDir() || !validVersion(version.Name()) {
				continue
			}
			pkg, e := s.verify(id, version.Name())
			if e == nil {
				out = append(out, Package{Manifest: pkg.Manifest, Version: pkg.Manifest.Version, KeyID: pkg.KeyID})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Manifest.ID == out[j].Manifest.ID {
			return compareVersion(out[i].Version, out[j].Version) > 0
		}
		return out[i].Manifest.ID < out[j].Manifest.ID
	})
	return out, nil
}

func (s *Service) Install(ctx context.Context, actor, id, version string) error {
	pkg, err := s.verify(id, version)
	if err != nil {
		if pluginIDPattern.MatchString(id) && versionPattern.MatchString(version) {
			_ = s.recordFailure(ctx, actor, id, version, "error")
		}
		return err
	}
	if h := s.hooks[id]; h != nil {
		if err = h.BeforePluginLifecycle(ctx, "install", pkg.Manifest); err != nil {
			return ErrUnavailable
		}
	}
	err = s.repo.install(ctx, actor, pkg)
	if err != nil {
		_ = s.recordFailure(ctx, actor, id, version, "install")
	}
	return err
}
func (s *Service) Lifecycle(ctx context.Context, actor, id, action string) error {
	if !pluginIDPattern.MatchString(id) {
		return ErrInvalid
	}
	p, err := s.repo.Get(ctx, id)
	if err != nil {
		return err
	}
	var pkg *verifiedPackage
	if action == "upgrade" {
		available, e := s.Packages(ctx)
		if e != nil {
			return e
		}
		for _, candidate := range available {
			if candidate.Manifest.ID == id && compareVersion(candidate.Version, p.Version) > 0 {
				verified, verifyErr := s.verify(id, candidate.Version)
				if verifyErr != nil {
					return verifyErr
				}
				pkg = &verified
				break
			}
		}
		if pkg == nil {
			_ = s.recordFailure(ctx, actor, id, p.Version, "upgrade")
			return ErrNotFound
		}
	}
	if h := s.hooks[id]; h != nil {
		if err = h.BeforePluginLifecycle(ctx, action, p.Manifest); err != nil {
			return ErrUnavailable
		}
	}
	err = s.repo.lifecycle(ctx, actor, id, action, pkg)
	if err != nil {
		_ = s.recordFailure(ctx, actor, id, p.Version, action)
	}
	return err
}
func (s *Service) recordFailure(ctx context.Context, actor, id, version, kind string) error {
	_, err := s.repo.db.ExecContext(ctx, `INSERT INTO admin_plugin_events(plugin_id,plugin_version,actor_user_id,event_type,outcome,failure_code) VALUES($1,$2,NULLIF($3,'')::uuid,$4,'failed','operation_failed')`, id, version, actor, kind)
	return err
}

func (s *Service) Config(ctx context.Context, id string) ([]ConfigValue, error) {
	p, err := s.repo.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	stored, err := s.repo.Config(ctx, id)
	if err != nil {
		return nil, err
	}
	out := make([]ConfigValue, 0, len(p.Manifest.Config))
	for _, field := range p.Manifest.Config {
		v := ConfigValue{Key: field.Key, Label: field.Label, Type: field.Type, Required: field.Required, Secret: field.Secret}
		rec, ok := stored[field.Key]
		if ok {
			v.Configured = true
			if !field.Secret && rec.Public != nil {
				if err = json.Unmarshal(rec.Public, &v.Value); err != nil {
					return nil, err
				}
			}
		}
		out = append(out, v)
	}
	return out, nil
}
func (s *Service) SaveConfig(ctx context.Context, actor, id string, input map[string]json.RawMessage) error {
	p, err := s.repo.Get(ctx, id)
	if err != nil {
		return err
	}
	fields := map[string]ConfigField{}
	for _, f := range p.Manifest.Config {
		fields[f.Key] = f
	}
	if len(input) > len(fields) {
		return ErrInvalid
	}
	public := map[string]json.RawMessage{}
	secrets := map[string][]byte{}
	for key, raw := range input {
		field, ok := fields[key]
		if !ok || !json.Valid(raw) {
			return ErrInvalid
		}
		var value any
		if err = json.Unmarshal(raw, &value); err != nil || !validConfigValue(field, value) {
			return ErrInvalid
		}
		if field.Secret {
			if s.configKey == nil {
				return ErrUnavailable
			}
			text, ok := value.(string)
			if !ok {
				return ErrInvalid
			}
			enc, e := encrypt(s.configKey, []byte(text), configAAD(id, key))
			if e != nil {
				return e
			}
			secrets[key] = enc
		} else {
			public[key] = raw
		}
	}
	for _, field := range p.Manifest.Config {
		if field.Required {
			if _, ok := input[field.Key]; !ok {
				existing, e := s.repo.Config(ctx, id)
				if e != nil {
					return e
				}
				if _, present := existing[field.Key]; !present {
					return ErrInvalid
				}
			}
		}
	}
	err = s.repo.SaveConfig(ctx, actor, id, public, secrets)
	if err != nil {
		return err
	}
	return nil
}

func (s *Service) Route(ctx context.Context, id, key, method string, body json.RawMessage) (any, error) {
	if !pluginIDPattern.MatchString(id) || !keyPattern.MatchString(key) {
		return nil, ErrInvalid
	}
	p, err := s.repo.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if p.Status != "enabled" {
		return nil, ErrNotFound
	}
	for _, r := range p.Manifest.Routes {
		if r.Key == key {
			allowed := false
			for _, candidate := range r.Methods {
				allowed = allowed || candidate == method
			}
			if !allowed {
				return nil, ErrNotFound
			}
			provider := s.providers[id]
			if provider == nil {
				return map[string]any{"route": r, "status": "provider_unavailable"}, nil
			}
			return provider.HandlePluginRoute(ctx, key, method, body, configReader{service: s})
		}
	}
	return nil, ErrNotFound
}
func (s *Service) RoutePermission(ctx context.Context, id, key string) (string, error) {
	p, err := s.repo.Get(ctx, id)
	if err != nil {
		return "", err
	}
	if p.Status != "enabled" {
		return "", ErrNotFound
	}
	for _, r := range p.Manifest.Routes {
		if r.Key == key {
			return r.Permission, nil
		}
	}
	return "", ErrNotFound
}

func (s *Service) verify(id, version string) (verifiedPackage, error) {
	var result verifiedPackage
	if s.root == "" || !pluginIDPattern.MatchString(id) || !validVersion(version) {
		return result, ErrNotFound
	}
	base := filepath.Join(s.root, id, version)
	if !within(s.root, base) {
		return result, ErrInvalid
	}
	for _, p := range []string{s.root, filepath.Join(s.root, id), base} {
		st, err := os.Lstat(p)
		if err != nil || !st.IsDir() || st.Mode()&os.ModeSymlink != 0 {
			return result, ErrNotFound
		}
	}
	manifestBytes, err := readRegular(filepath.Join(base, "manifest.json"), 1<<20)
	if err != nil {
		return result, ErrInvalid
	}
	if err = json.Unmarshal(manifestBytes, &result.Manifest); err != nil || validateManifest(result.Manifest) != nil || result.Manifest.ID != id || result.Manifest.Version != version {
		return result, ErrInvalid
	}
	sigBytes, err := readRegular(filepath.Join(base, "manifest.sig"), 4096)
	if err != nil {
		return result, ErrInvalid
	}
	sig, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(sigBytes)))
	if err != nil || len(sig) != ed25519.SignatureSize {
		return result, ErrInvalid
	}
	trusted := false
	for _, key := range s.keys {
		if ed25519.Verify(key, manifestBytes, sig) {
			digest := sha256.Sum256(key)
			result.KeyID = hex.EncodeToString(digest[:8])
			trusted = true
			break
		}
	}
	if !trusted {
		return result, ErrInvalid
	}
	result.ManifestHash = sha256.Sum256(manifestBytes)
	result.Migrations = make(map[string][]byte, len(result.Manifest.MigrationSHA256))
	result.MigrationHashes = make(map[string][32]byte, len(result.Manifest.MigrationSHA256))
	if len(result.Manifest.MigrationSHA256) > 0 {
		dir := filepath.Join(base, "migrations")
		st, e := os.Lstat(dir)
		if e != nil || !st.IsDir() || st.Mode()&os.ModeSymlink != 0 {
			return verifiedPackage{}, ErrInvalid
		}
	}
	result.MigrationNames = sortedMigrationNames(result.Manifest)
	for _, name := range result.MigrationNames {
		content, e := readRegular(filepath.Join(base, "migrations", name), 2<<20)
		if e != nil {
			return verifiedPackage{}, ErrInvalid
		}
		sum := sha256.Sum256(content)
		expected, _ := hex.DecodeString(result.Manifest.MigrationSHA256[name])
		if !equalBytes(sum[:], expected) {
			return verifiedPackage{}, ErrInvalid
		}
		result.Migrations[name] = content
		result.MigrationHashes[name] = sum
	}
	return result, nil
}

func readRegular(path string, max int64) ([]byte, error) {
	st, err := os.Lstat(path)
	if err != nil || !st.Mode().IsRegular() || st.Mode()&os.ModeSymlink != 0 || st.Size() < 0 || st.Size() > max {
		return nil, ErrInvalid
	}
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, max+1))
	if err != nil || int64(len(data)) > max {
		return nil, ErrInvalid
	}
	return data, nil
}
func within(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
func configAAD(pluginID, configKey string) []byte {
	return []byte("goadmin-plugin-config:v1:" + pluginID + ":" + configKey)
}

func encrypt(key, plain, aad []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return nil, err
	}
	return append(nonce, gcm.Seal(nil, nonce, plain, aad)...), nil
}
func validConfigValue(f ConfigField, v any) bool {
	switch f.Type {
	case "string":
		s, ok := v.(string)
		return ok && len(s) <= 4096
	case "integer":
		n, ok := v.(float64)
		return ok && n == float64(int64(n))
	case "boolean":
		_, ok := v.(bool)
		return ok
	case "json":
		return len(fmt.Sprint(v)) <= 16384
	}
	return false
}
