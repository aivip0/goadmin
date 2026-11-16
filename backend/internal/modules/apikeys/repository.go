package apikeys

import (
	"context"
	"database/sql"
	"errors"
	"github.com/lib/pq"
	"goadminkit/backend/internal/core/audit"
	"time"
)

type Repository struct{ db *sql.DB }

func NewRepository(db *sql.DB) *Repository { return &Repository{db} }

const cols = `id::text,name,token_prefix,scopes,expires_at,revoked_at,last_used_at,created_at`

func scan(row interface{ Scan(...any) error }) (Item, error) {
	var v Item
	err := row.Scan(&v.ID, &v.Name, &v.Prefix, pq.Array(&v.Scopes), &v.ExpiresAt, &v.RevokedAt, &v.LastUsedAt, &v.CreatedAt)
	return v, err
}
func (r *Repository) List(ctx context.Context, owner string, page, size int) (Page, error) {
	p := Page{Items: []Item{}, Page: page, PageSize: size}
	if err := r.db.QueryRowContext(ctx, `SELECT count(*) FROM admin_api_keys WHERE owner_user_id=$1`, owner).Scan(&p.Total); err != nil {
		return p, err
	}
	rows, err := r.db.QueryContext(ctx, `SELECT `+cols+` FROM admin_api_keys WHERE owner_user_id=$1 ORDER BY created_at DESC,id DESC LIMIT $2 OFFSET $3`, owner, size, (page-1)*size)
	if err != nil {
		return p, err
	}
	defer rows.Close()
	for rows.Next() {
		v, e := scan(rows)
		if e != nil {
			return p, e
		}
		p.Items = append(p.Items, v)
	}
	return p, rows.Err()
}
func (r *Repository) Create(ctx context.Context, owner string, in Input, prefix string, hash []byte) (Item, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return Item{}, err
	}
	defer tx.Rollback()
	if err = r.authorize(ctx, tx, owner, "api_keys:create"); err != nil {
		return Item{}, err
	}
	v, err := scan(tx.QueryRowContext(ctx, `INSERT INTO admin_api_keys(owner_user_id,name,token_prefix,token_hash,scopes,expires_at)
 VALUES($1,$2,$3,$4,ARRAY['dashboard:view']::text[],clock_timestamp()+($5 * interval '1 day')) RETURNING `+cols, owner, in.Name, prefix, hash, in.ExpiresInDays))
	if err != nil {
		return Item{}, err
	}
	if err = audit.Insert(ctx, tx, owner, "api_key_created", "api_key", v.ID); err != nil {
		return Item{}, err
	}
	return v, tx.Commit()
}
func (r *Repository) Revoke(ctx context.Context, owner, id string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = r.authorize(ctx, tx, owner, "api_keys:revoke"); err != nil {
		return err
	}
	var changed bool
	err = tx.QueryRowContext(ctx, `SELECT revoked_at IS NULL FROM admin_api_keys WHERE id=$1 AND owner_user_id=$2 FOR UPDATE`, id, owner).Scan(&changed)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if changed {
		if _, err = tx.ExecContext(ctx, `UPDATE admin_api_keys SET revoked_at=clock_timestamp() WHERE id=$1 AND owner_user_id=$2`, id, owner); err != nil {
			return err
		}
		if err = audit.Insert(ctx, tx, owner, "api_key_revoked", "api_key", id); err != nil {
			return err
		}
	}
	return tx.Commit()
}
func (r *Repository) authorize(ctx context.Context, tx *sql.Tx, owner, permission string) error {
	var guard string
	if err := tx.QueryRowContext(ctx, `SELECT id::text FROM app_metadata WHERE component='core' FOR UPDATE`).Scan(&guard); err != nil {
		return err
	}
	var allowed bool
	err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM admin_users u JOIN user_roles ur ON ur.user_id=u.id
 JOIN roles r ON r.id=ur.role_id AND r.status='active' WHERE u.id=$1 AND u.status='active' AND r.is_super_admin)
 OR EXISTS(SELECT 1 FROM admin_users u JOIN user_roles ur ON ur.user_id=u.id JOIN roles r ON r.id=ur.role_id AND r.status='active'
 JOIN role_permissions rp ON rp.role_id=r.id JOIN permissions p ON p.id=rp.permission_id
 WHERE u.id=$1 AND u.status='active' AND p.code=$2)`, owner, permission).Scan(&allowed)
	if err != nil {
		return err
	}
	if !allowed {
		return ErrForbidden
	}
	return nil
}
func (r *Repository) Resolve(ctx context.Context, hash []byte) (string, error) {
	var owner string
	var expires time.Time
	// The owner status is checked in the same statement as the key state.
	err := r.db.QueryRowContext(ctx, `SELECT k.owner_user_id::text,k.expires_at FROM admin_api_keys k
 JOIN admin_users u ON u.id=k.owner_user_id AND u.status='active'
 WHERE k.token_hash=$1 AND k.revoked_at IS NULL AND k.expires_at>clock_timestamp() AND k.scopes @> ARRAY['dashboard:view']::text[]`, hash).Scan(&owner, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrUnauthorized
	}
	return owner, err
}
func (r *Repository) Used(ctx context.Context, hash []byte) {
	_, _ = r.db.ExecContext(ctx, `UPDATE admin_api_keys SET last_used_at=clock_timestamp() WHERE token_hash=$1 AND revoked_at IS NULL`, hash)
}
