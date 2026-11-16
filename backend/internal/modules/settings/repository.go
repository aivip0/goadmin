package settings

import (
	"context"
	"database/sql"
	"encoding/json"
	"regexp"
	"sort"
	"strings"

	auditlog "goadminkit/backend/internal/core/audit"
)

var mediaLogo = regexp.MustCompile(`^/api/v1/admin/media/([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})/content$`)

type SQLRepository struct{ db *sql.DB }

func NewRepository(db *sql.DB) *SQLRepository { return &SQLRepository{db: db} }
func (r *SQLRepository) List(ctx context.Context) ([]record, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id::text,key,value_json,is_sensitive,value_ciphertext,encryption_key_version FROM system_settings ORDER BY key LIMIT 100`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []record{}
	for rows.Next() {
		var rec record
		var value []byte
		var ver sql.NullInt64
		if err = rows.Scan(&rec.id, &rec.key, &value, &rec.sensitive, &rec.encrypted, &ver); err != nil {
			return nil, err
		}
		rec.public = value
		if ver.Valid {
			rec.version = int(ver.Int64)
		}
		result = append(result, rec)
	}
	return result, rows.Err()
}
func (r *SQLRepository) Save(ctx context.Context, actor string, updates map[string]record) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var ignored string
	if err = tx.QueryRowContext(ctx, `SELECT id::text FROM app_metadata WHERE component='core' FOR UPDATE`).Scan(&ignored); err != nil {
		return err
	}
	var allowed bool
	err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM user_roles ur JOIN roles r ON r.id=ur.role_id AND r.status='active' AND r.is_super_admin JOIN admin_users u ON u.id=ur.user_id AND u.status='active' WHERE u.id=$1)
OR EXISTS(SELECT 1 FROM user_roles ur JOIN roles r ON r.id=ur.role_id AND r.status='active' JOIN role_permissions rp ON rp.role_id=r.id JOIN permissions p ON p.id=rp.permission_id JOIN admin_users u ON u.id=ur.user_id AND u.status='active' WHERE u.id=$1 AND p.code='settings:update')`, actor).Scan(&allowed)
	if err != nil {
		return err
	}
	if !allowed {
		return ErrForbidden
	}
	keys := make([]string, 0, len(updates))
	for key := range updates {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		v := updates[key]
		var mediaID any
		if key == "branding.logo_path" {
			var path string
			if err = json.Unmarshal(v.public, &path); err != nil {
				return ErrInvalid
			}
			if strings.HasPrefix(path, "/api/v1/admin/media/") {
				match := mediaLogo.FindStringSubmatch(path)
				if match == nil {
					return ErrInvalid
				}
				var mime string
				if err = tx.QueryRowContext(ctx, `SELECT mime_type FROM media_files WHERE id=$1 AND status='ready' AND verified_generation=(SELECT active_verify_generation FROM media_storage_binding WHERE singleton=true AND verification_state='complete') FOR UPDATE`, match[1]).Scan(&mime); err != nil {
					if err == sql.ErrNoRows {
						return ErrInvalid
					}
					return err
				}
				if !strings.HasPrefix(mime, "image/") {
					return ErrInvalid
				}
				mediaID = match[1]
			}
		}
		var public any
		var secret any
		var version any
		if v.sensitive {
			secret = v.encrypted
			version = v.version
		} else {
			public = string(v.public)
		}
		var id string
		err = tx.QueryRowContext(ctx, `INSERT INTO system_settings(key,value_json,is_sensitive,value_ciphertext,encryption_key_version,media_file_id) VALUES($1,$2,$3,$4,$5,$6)
ON CONFLICT(key) DO UPDATE SET value_json=excluded.value_json,is_sensitive=excluded.is_sensitive,value_ciphertext=excluded.value_ciphertext,encryption_key_version=excluded.encryption_key_version,media_file_id=excluded.media_file_id,updated_at=clock_timestamp() RETURNING id::text`, key, public, v.sensitive, secret, version, mediaID).Scan(&id)
		if err != nil {
			return err
		}
		if err = auditlog.Insert(ctx, tx, actor, "setting_updated", "setting", id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

var _ Repository = (*SQLRepository)(nil)
