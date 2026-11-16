package settings

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"strings"
	"time"
	_ "time/tzdata"
	"unicode/utf8"
)

var (
	ErrInvalid        = errors.New("invalid settings")
	ErrForbidden      = errors.New("permission denied")
	ErrKeyUnavailable = errors.New("settings encryption key unavailable")
)

type definition struct {
	defaultValue any
	validate     func(any) bool
	sensitive    bool
}

func text(limit int, required bool) func(any) bool {
	return func(v any) bool {
		s, ok := v.(string)
		return ok && utf8.ValidString(s) && len(s) <= limit && (!required || strings.TrimSpace(s) != "")
	}
}
func logo(v any) bool {
	s, ok := v.(string)
	if !ok || !text(2048, false)(s) {
		return false
	}
	if s == "" {
		return true
	}
	u, err := url.Parse(s)
	return err == nil && u.Scheme == "" && u.Host == "" && u.User == nil && u.RawQuery == "" && u.Fragment == "" && strings.HasPrefix(s, "/") && !strings.HasPrefix(s, "//") && !strings.ContainsAny(s, "\\%\r\n") && u.EscapedPath() == s && !strings.Contains(s, "..")
}
func zone(v any) bool {
	s, ok := v.(string)
	if !ok || len(s) > 64 || strings.Contains(s, "..") || (!strings.Contains(s, "/") && s != "UTC") {
		return false
	}
	_, err := time.LoadLocation(s)
	return err == nil
}
func language(v any) bool {
	s, ok := v.(string)
	return ok && (s == "en" || s == "zh-CN" || s == "zh-TW")
}
func pageSize(v any) bool {
	n, ok := v.(float64)
	return ok && (n == 10 || n == 25 || n == 50 || n == 100)
}

var registry = map[string]definition{
	"site.name":                 {defaultValue: "GoAdmin Kit", validate: text(100, true)},
	"site.description":          {defaultValue: "", validate: text(500, false)},
	"branding.logo_path":        {defaultValue: "", validate: logo},
	"localization.timezone":     {defaultValue: "UTC", validate: zone},
	"localization.language":     {defaultValue: "en", validate: language},
	"general.default_page_size": {defaultValue: float64(25), validate: pageSize},
}

type Value struct {
	Value      any  `json:"value,omitempty"`
	Sensitive  bool `json:"sensitive"`
	Configured bool `json:"configured"`
}
type Result struct {
	Settings map[string]Value `json:"settings"`
}
type record struct {
	id, key   string
	public    []byte
	sensitive bool
	encrypted []byte
	version   int
}
type Repository interface {
	List(context.Context) ([]record, error)
	Save(context.Context, string, map[string]record) error
}
type Service struct {
	repo Repository
	key  []byte
}

func NewService(repo Repository, key []byte) *Service { return &Service{repo: repo, key: key} }

// Ciphertext is nonce || AES-256-GCM(payload); key version 1 is explicit for rotation.
func (s *Service) encrypt(value []byte) ([]byte, error) {
	if len(s.key) != 32 {
		return nil, ErrKeyUnavailable
	}
	b, err := aes.NewCipher(s.key)
	if err != nil {
		return nil, ErrKeyUnavailable
	}
	g, err := cipher.NewGCM(b)
	if err != nil {
		return nil, ErrKeyUnavailable
	}
	nonce := make([]byte, g.NonceSize())
	if _, err = io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	return g.Seal(nonce, nonce, value, nil), nil
}
func (s *Service) decrypt(value []byte, version int) ([]byte, error) {
	if len(s.key) != 32 || version != 1 {
		return nil, ErrKeyUnavailable
	}
	b, err := aes.NewCipher(s.key)
	if err != nil {
		return nil, ErrKeyUnavailable
	}
	g, err := cipher.NewGCM(b)
	if err != nil {
		return nil, ErrKeyUnavailable
	}
	if len(value) < g.NonceSize() {
		return nil, ErrKeyUnavailable
	}
	data, err := g.Open(nil, value[:g.NonceSize()], value[g.NonceSize():], nil)
	if err != nil {
		return nil, ErrKeyUnavailable
	}
	return data, nil
}
func (s *Service) Get(ctx context.Context) (Result, error) {
	rows, err := s.repo.List(ctx)
	if err != nil {
		return Result{}, err
	}
	result := Result{Settings: make(map[string]Value, len(registry))}
	for key, def := range registry {
		result.Settings[key] = Value{Value: def.defaultValue, Sensitive: def.sensitive, Configured: false}
	}
	for _, row := range rows {
		def, ok := registry[row.key]
		if !ok {
			continue
		}
		if row.sensitive != def.sensitive {
			return Result{}, ErrInvalid
		}
		if def.sensitive {
			if _, err = s.decrypt(row.encrypted, row.version); err != nil {
				return Result{}, err
			}
			result.Settings[row.key] = Value{Sensitive: true, Configured: true}
			continue
		}
		var value any
		if err = json.Unmarshal(row.public, &value); err != nil || !def.validate(value) {
			return Result{}, ErrInvalid
		}
		result.Settings[row.key] = Value{Value: value, Configured: true}
	}
	return result, nil
}
func (s *Service) Update(ctx context.Context, actor string, changes map[string]json.RawMessage) error {
	if len(changes) == 0 || len(changes) > len(registry) {
		return ErrInvalid
	}
	updates := make(map[string]record, len(changes))
	for key, raw := range changes {
		def, ok := registry[key]
		if !ok {
			return ErrInvalid
		}
		var value any
		if err := json.Unmarshal(raw, &value); err != nil || !def.validate(value) {
			return ErrInvalid
		}
		rec := record{key: key, sensitive: def.sensitive}
		if def.sensitive {
			plain := []byte(value.(string))
			ciphertext, err := s.encrypt(plain)
			if err != nil {
				return err
			}
			rec.encrypted = ciphertext
			rec.version = 1
		} else {
			data, err := json.Marshal(value)
			if err != nil {
				return ErrInvalid
			}
			rec.public = data
		}
		updates[key] = rec
	}
	return s.repo.Save(ctx, actor, updates)
}
