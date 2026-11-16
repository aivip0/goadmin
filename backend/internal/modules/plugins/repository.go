package plugins

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	auditlog "goadminkit/backend/internal/core/audit"
)

type Repository struct{ db *sql.DB }

func NewRepository(db *sql.DB) *Repository { return &Repository{db: db} }

func (r *Repository) List(ctx context.Context) ([]Plugin, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT plugin_id,name,version,description,author,status,COALESCE(failure_code,''),installed_at,updated_at,manifest FROM admin_plugins ORDER BY plugin_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]Plugin, 0)
	for rows.Next() {
		var p Plugin
		var raw []byte
		var installed, updated time.Time
		if err = rows.Scan(&p.ID, &p.Name, &p.Version, &p.Description, &p.Author, &p.Status, &p.FailureCode, &installed, &updated, &raw); err != nil {
			return nil, err
		}
		p.InstalledAt = installed.UTC().Format(time.RFC3339)
		p.UpdatedAt = updated.UTC().Format(time.RFC3339)
		if err = json.Unmarshal(raw, &p.Manifest); err != nil {
			return nil, err
		}
		items = append(items, p)
	}
	return items, rows.Err()
}
func (r *Repository) Get(ctx context.Context, id string) (Plugin, error) {
	var p Plugin
	var raw []byte
	var installed, updated time.Time
	err := r.db.QueryRowContext(ctx, `SELECT plugin_id,name,version,description,author,status,COALESCE(failure_code,''),installed_at,updated_at,manifest FROM admin_plugins WHERE plugin_id=$1`, id).Scan(&p.ID, &p.Name, &p.Version, &p.Description, &p.Author, &p.Status, &p.FailureCode, &installed, &updated, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		return p, ErrNotFound
	}
	if err != nil {
		return p, err
	}
	p.InstalledAt = installed.UTC().Format(time.RFC3339)
	p.UpdatedAt = updated.UTC().Format(time.RFC3339)
	err = json.Unmarshal(raw, &p.Manifest)
	return p, err
}
func (r *Repository) Events(ctx context.Context, id string, limit int) ([]Event, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id::text,plugin_id,plugin_version,event_type,outcome,COALESCE(failure_code,''),created_at FROM admin_plugin_events WHERE ($1='' OR plugin_id=$1) ORDER BY created_at DESC,id DESC LIMIT $2`, id, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]Event, 0)
	for rows.Next() {
		var e Event
		var at time.Time
		if err = rows.Scan(&e.ID, &e.PluginID, &e.PluginVersion, &e.EventType, &e.Outcome, &e.FailureCode, &at); err != nil {
			return nil, err
		}
		e.CreatedAt = at.UTC().Format(time.RFC3339)
		items = append(items, e)
	}
	return items, rows.Err()
}

func (r *Repository) install(ctx context.Context, actor string, pkg verifiedPackage) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = lockCore(ctx, tx); err != nil {
		return err
	}
	for _, d := range pkg.Manifest.Dependencies {
		var version string
		var status string
		if err = tx.QueryRowContext(ctx, `SELECT version,status FROM admin_plugins WHERE plugin_id=$1`, d.ID).Scan(&version, &status); err != nil || status != "enabled" || version != d.Version {
			return ErrConflict
		}
	}
	manifest, _ := json.Marshal(pkg.Manifest)
	_, err = tx.ExecContext(ctx, `INSERT INTO admin_plugins(plugin_id,name,version,description,author,source_key,manifest,manifest_sha256,status) VALUES($1,$2,$3,$4,$5,$6,$7,$8,'installed')`, pkg.Manifest.ID, pkg.Manifest.Name, pkg.Manifest.Version, pkg.Manifest.Description, pkg.Manifest.Author, pkg.KeyID, manifest, pkg.ManifestHash[:])
	if err != nil {
		return classify(err)
	}
	for i, name := range pkg.MigrationNames {
		content := pkg.Migrations[name]
		if _, err = tx.ExecContext(ctx, string(content)); err != nil {
			return fmt.Errorf("plugin migration failed: %w", err)
		}
		sum := pkg.MigrationHashes[name]
		if _, err = tx.ExecContext(ctx, `INSERT INTO admin_plugin_migrations(plugin_id,version,checksum) VALUES($1,$2,$3)`, pkg.Manifest.ID, i+1, sum[:]); err != nil {
			return err
		}
	}
	if err = syncPermissions(ctx, tx, pkg.Manifest, false); err != nil {
		return err
	}
	if err = syncMenus(ctx, tx, pkg.Manifest, "disabled"); err != nil {
		return err
	}
	if err = pluginEvent(ctx, tx, actor, pkg.Manifest.ID, pkg.Manifest.Version, "install", "succeeded", ""); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *Repository) lifecycle(ctx context.Context, actor, id, action string, pkg *verifiedPackage) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = lockCore(ctx, tx); err != nil {
		return err
	}
	var version, status string
	var raw []byte
	err = tx.QueryRowContext(ctx, `SELECT version,status,manifest FROM admin_plugins WHERE plugin_id=$1 FOR UPDATE`, id).Scan(&version, &status, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	var m Manifest
	if err = json.Unmarshal(raw, &m); err != nil {
		return err
	}
	switch action {
	case "enable":
		if status == "enabled" {
			return ErrConflict
		}
		if status != "installed" && status != "disabled" {
			return ErrConflict
		}
		if !dependenciesSatisfied(ctx, tx, m) {
			return ErrConflict
		}
		if _, err = tx.ExecContext(ctx, `UPDATE admin_plugins SET status='enabled',failure_code=NULL,updated_at=clock_timestamp() WHERE plugin_id=$1`, id); err != nil {
			return err
		}
		if err = syncMenus(ctx, tx, m, "active"); err != nil {
			return err
		}
	case "disable":
		if status != "enabled" {
			return ErrConflict
		}
		var dependents int
		if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM admin_plugins p, LATERAL jsonb_array_elements(p.manifest->'dependencies') d WHERE p.status='enabled' AND d->>'id'=$1`, id).Scan(&dependents); err != nil {
			return err
		}
		if dependents > 0 {
			return ErrConflict
		}
		if _, err = tx.ExecContext(ctx, `UPDATE admin_plugins SET status='disabled',updated_at=clock_timestamp() WHERE plugin_id=$1`, id); err != nil {
			return err
		}
		if err = syncMenus(ctx, tx, m, "disabled"); err != nil {
			return err
		}
	case "uninstall":
		if status == "enabled" {
			return ErrConflict
		}
		if err = deleteMenus(ctx, tx, m); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `DELETE FROM permissions WHERE left(code,length($1))=$1`, permissionPrefix(id)); err != nil {
			return err
		}
		if err = pluginEvent(ctx, tx, actor, id, version, "uninstall", "succeeded", ""); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `DELETE FROM admin_plugins WHERE plugin_id=$1`, id); err != nil {
			return err
		}
		return tx.Commit()
	case "upgrade":
		if pkg == nil || pkg.Manifest.ID != id || status == "enabled" {
			return ErrInvalid
		}
		if compareVersion(pkg.Manifest.Version, version) <= 0 {
			return ErrConflict
		}
		if err = syncPermissions(ctx, tx, pkg.Manifest, true); err != nil {
			return err
		}
		var applied int
		if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM admin_plugin_migrations WHERE plugin_id=$1`, id).Scan(&applied); err != nil {
			return err
		}
		if len(pkg.MigrationNames) < applied {
			return ErrConflict
		}
		for i, name := range pkg.MigrationNames {
			sum := pkg.MigrationHashes[name]
			if i < applied {
				var previous []byte
				if err = tx.QueryRowContext(ctx, `SELECT checksum FROM admin_plugin_migrations WHERE plugin_id=$1 AND version=$2`, id, i+1).Scan(&previous); err != nil || !equalBytes(previous, sum[:]) {
					return ErrConflict
				}
				continue
			}
			content := pkg.Migrations[name]
			if _, err = tx.ExecContext(ctx, string(content)); err != nil {
				return fmt.Errorf("plugin migration failed: %w", err)
			}
			if _, err = tx.ExecContext(ctx, `INSERT INTO admin_plugin_migrations(plugin_id,version,checksum) VALUES($1,$2,$3)`, id, i+1, sum[:]); err != nil {
				return err
			}
		}
		manifest, _ := json.Marshal(pkg.Manifest)
		if _, err = tx.ExecContext(ctx, `UPDATE admin_plugins SET name=$2,version=$3,description=$4,author=$5,source_key=$6,manifest=$7,manifest_sha256=$8,failure_code=NULL,updated_at=clock_timestamp() WHERE plugin_id=$1`, id, pkg.Manifest.Name, pkg.Manifest.Version, pkg.Manifest.Description, pkg.Manifest.Author, pkg.KeyID, manifest, pkg.ManifestHash[:]); err != nil {
			return err
		}
		m = pkg.Manifest
		version = pkg.Manifest.Version
		if err = syncMenus(ctx, tx, m, "disabled"); err != nil {
			return err
		}
	default:
		return ErrInvalid
	}
	if err = pluginEvent(ctx, tx, actor, id, version, action, "succeeded", ""); err != nil {
		return err
	}
	return tx.Commit()
}

func lockCore(ctx context.Context, tx *sql.Tx) error {
	var id string
	return tx.QueryRowContext(ctx, `SELECT id::text FROM app_metadata WHERE component='core' FOR UPDATE`).Scan(&id)
}
func dependenciesSatisfied(ctx context.Context, tx *sql.Tx, m Manifest) bool {
	for _, d := range m.Dependencies {
		var ok bool
		if tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM admin_plugins WHERE plugin_id=$1 AND version=$2 AND status='enabled')`, d.ID, d.Version).Scan(&ok) != nil || !ok {
			return false
		}
	}
	return true
}
func permissionPrefix(id string) string { return "plugin_" + strings.ReplaceAll(id, "-", "_") + ":" }
func syncPermissions(ctx context.Context, tx *sql.Tx, m Manifest, allowUpdate bool) error {
	for _, p := range m.Permissions {
		var name, description string
		err := tx.QueryRowContext(ctx, `SELECT name,description FROM permissions WHERE code=$1`, p.Code).Scan(&name, &description)
		if err == nil && (name != p.Name || description != p.Description) {
			if !allowUpdate {
				return ErrConflict
			}
			if _, err = tx.ExecContext(ctx, `UPDATE permissions SET name=$2,description=$3,updated_at=clock_timestamp() WHERE code=$1`, p.Code, p.Name, p.Description); err != nil {
				return classify(err)
			}
			continue
		}
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO permissions(code,name,description) VALUES($1,$2,$3) ON CONFLICT(code) DO NOTHING`, p.Code, p.Name, p.Description); err != nil {
			return classify(err)
		}
	}
	return nil
}
func syncMenus(ctx context.Context, tx *sql.Tx, m Manifest, status string) error {
	for _, v := range m.Menus {
		code := "plugin_" + strings.ReplaceAll(m.ID, "-", "_") + "_" + v.Key
		path := "/admin/plugins/" + m.ID + "/" + v.RouteKey
		var id string
		err := tx.QueryRowContext(ctx, `SELECT id::text FROM menus WHERE code=$1 AND path=$2 AND component_key='plugins' AND is_system=true`, code, path).Scan(&id)
		if errors.Is(err, sql.ErrNoRows) {
			_, err = tx.ExecContext(ctx, `INSERT INTO menus(code,name,path,icon,sort_order,visible,status,menu_type,component_key,is_system,permission_id) VALUES($1,$2,$3,$4,$5,true,$6,'page','plugins',true,(SELECT id FROM permissions WHERE code=$7))`, code, v.Name, path, v.Icon, v.SortOrder, status, v.Permission)
			if err != nil {
				return classify(err)
			}
			continue
		}
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `UPDATE menus SET name=$2,icon=$3,sort_order=$4,visible=true,status=$5,permission_id=(SELECT id FROM permissions WHERE code=$6),updated_at=clock_timestamp() WHERE id=$1`, id, v.Name, v.Icon, v.SortOrder, status, v.Permission)
		if err != nil {
			return classify(err)
		}
	}
	return nil
}
func deleteMenus(ctx context.Context, tx *sql.Tx, m Manifest) error {
	for _, v := range m.Menus {
		code := "plugin_" + strings.ReplaceAll(m.ID, "-", "_") + "_" + v.Key
		if _, err := tx.ExecContext(ctx, `DELETE FROM menus WHERE code=$1 AND is_system=true AND component_key='plugins'`, code); err != nil {
			return err
		}
	}
	return nil
}
func pluginEvent(ctx context.Context, tx *sql.Tx, actor, id, version, kind, outcome, failure string) error {
	var actorArg any
	if actor != "" {
		actorArg = actor
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO admin_plugin_events(plugin_id,plugin_version,actor_user_id,event_type,outcome,failure_code) VALUES($1,$2,$3,$4,$5,NULLIF($6,''))`, id, version, actorArg, kind, outcome, failure)
	if err == nil {
		if actor != "" {
			err = auditlog.Insert(ctx, tx, actor, "plugin_"+kind, "plugin", stableUUID(id))
		}
	}
	return err
}

func (r *Repository) SaveConfig(ctx context.Context, actor, id string, values map[string]json.RawMessage, secrets map[string][]byte) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = lockCore(ctx, tx); err != nil {
		return err
	}
	var status string
	if err = tx.QueryRowContext(ctx, `SELECT status FROM admin_plugins WHERE plugin_id=$1 FOR UPDATE`, id).Scan(&status); errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	} else if err != nil {
		return err
	}
	for k, v := range values {
		if _, err = tx.ExecContext(ctx, `INSERT INTO admin_plugin_config(plugin_id,config_key,public_value) VALUES($1,$2,$3) ON CONFLICT(plugin_id,config_key) DO UPDATE SET public_value=EXCLUDED.public_value,secret_ciphertext=NULL,key_version=NULL,updated_at=clock_timestamp()`, id, k, []byte(v)); err != nil {
			return classify(err)
		}
	}
	for k, v := range secrets {
		if _, err = tx.ExecContext(ctx, `INSERT INTO admin_plugin_config(plugin_id,config_key,secret_ciphertext,key_version) VALUES($1,$2,$3,1) ON CONFLICT(plugin_id,config_key) DO UPDATE SET public_value=NULL,secret_ciphertext=EXCLUDED.secret_ciphertext,key_version=1,updated_at=clock_timestamp()`, id, k, v); err != nil {
			return classify(err)
		}
	}
	if err = pluginEvent(ctx, tx, actor, id, "", "configure", "succeeded", ""); err != nil {
		return err
	}
	return tx.Commit()
}
func (r *Repository) Config(ctx context.Context, id string) (map[string]storedConfig, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT config_key,public_value,secret_ciphertext,key_version FROM admin_plugin_config WHERE plugin_id=$1 ORDER BY config_key`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	m := map[string]storedConfig{}
	for rows.Next() {
		var k string
		var pub, secret []byte
		var version sql.NullInt64
		if err = rows.Scan(&k, &pub, &secret, &version); err != nil {
			return nil, err
		}
		m[k] = storedConfig{Public: pub, Secret: secret, KeyVersion: int(version.Int64)}
	}
	return m, rows.Err()
}
func classify(err error) error {
	if err == nil {
		return nil
	}
	var state interface{ SQLState() string }
	if errors.As(err, &state) {
		switch state.SQLState() {
		case "23505", "23503", "23514":
			return ErrConflict
		}
	}
	return err
}
func stableUUID(s string) string { // Deterministic valid UUID target for append-only security_events.
	h := sha256.Sum256([]byte(s))
	return fmt.Sprintf("%x-%x-%x-%x-%x", h[0:4], h[4:6], h[6:8], h[8:10], h[10:16])
}
func compareVersion(a, b string) int {
	coreA, preA, hasPreA := splitVersion(a)
	coreB, preB, hasPreB := splitVersion(b)
	if len(coreA) != 3 || len(coreB) != 3 {
		return strings.Compare(a, b)
	}
	for i := range coreA {
		if cmp := compareNumericIdentifier(coreA[i], coreB[i]); cmp != 0 {
			return cmp
		}
	}
	if hasPreA != hasPreB {
		if hasPreA {
			return -1
		}
		return 1
	}
	if !hasPreA {
		return 0
	}
	for i := 0; i < len(preA) && i < len(preB); i++ {
		if preA[i] == preB[i] {
			continue
		}
		numericA, numericB := isNumericIdentifier(preA[i]), isNumericIdentifier(preB[i])
		if numericA && numericB {
			return compareNumericIdentifier(preA[i], preB[i])
		}
		if numericA != numericB {
			if numericA {
				return -1
			}
			return 1
		}
		return strings.Compare(preA[i], preB[i])
	}
	if len(preA) < len(preB) {
		return -1
	}
	if len(preA) > len(preB) {
		return 1
	}
	return 0
}

func splitVersion(version string) (core, prerelease []string, hasPrerelease bool) {
	withoutBuild := strings.SplitN(version, "+", 2)[0]
	parts := strings.SplitN(withoutBuild, "-", 2)
	core = strings.Split(parts[0], ".")
	if len(parts) == 2 {
		prerelease, hasPrerelease = strings.Split(parts[1], "."), true
	}
	return core, prerelease, hasPrerelease
}

func isNumericIdentifier(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func compareNumericIdentifier(a, b string) int {
	if len(a) < len(b) {
		return -1
	}
	if len(a) > len(b) {
		return 1
	}
	return strings.Compare(a, b)
}
func equalBytes(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	var diff byte
	for i := range a {
		diff |= a[i] ^ b[i]
	}
	return diff == 0
}
