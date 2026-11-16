package profile

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"time"

	"github.com/lib/pq"
	"goadminkit/backend/internal/core/audit"
	"goadminkit/backend/internal/modules/auth"
)

type SQLRepository struct {
	db   *sql.DB
	idle time.Duration
}

func NewRepository(db *sql.DB, idle time.Duration) *SQLRepository { return &SQLRepository{db, idle} }

func (r *SQLRepository) transaction(ctx context.Context, token string, write bool, fn func(*sql.Tx, auth.User, string) error) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// Matches IAM's core -> user -> session order; never take core after a user lock.
	if write {
		var id string
		if err = tx.QueryRowContext(ctx, "SELECT id FROM app_metadata WHERE component='core' FOR UPDATE").Scan(&id); err != nil {
			return err
		}
	}
	u, sid, err := auth.AuthenticateTx(ctx, tx, token, r.idle)
	if err != nil {
		return err
	}
	if err = fn(tx, u, sid); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *SQLRepository) Load(ctx context.Context, token string) (credential, error) {
	var out credential
	err := r.transaction(ctx, token, false, func(tx *sql.Tx, u auth.User, _ string) error {
		out.User = u
		return tx.QueryRowContext(ctx, "SELECT password_hash FROM admin_users WHERE id=$1", u.ID).Scan(&out.Hash)
	})
	return out, err
}

func (r *SQLRepository) Update(ctx context.Context, token string, in Input) (auth.User, error) {
	var out auth.User
	err := r.transaction(ctx, token, true, func(tx *sql.Tx, u auth.User, _ string) error {
		err := tx.QueryRowContext(ctx, `UPDATE admin_users SET email=$2,display_name=$3,updated_at=clock_timestamp() WHERE id=$1 RETURNING id,username,email,display_name`, u.ID, in.Email, in.DisplayName).Scan(&out.ID, &out.Username, &out.Email, &out.DisplayName)
		var pg *pq.Error
		if errors.As(err, &pg) && pg.Code == "23505" {
			return ErrConflict
		}
		if err != nil {
			return err
		}
		return audit.Insert(ctx, tx, u.ID, "profile_updated", "user", u.ID)
	})
	return out, err
}

// A separate durable bucket limits reauthentication to five attempts/minute,
// including successful attempts. It cannot be bypassed by a new process/session.
func (r *SQLRepository) PasswordAttempt(ctx context.Context, uid string) error {
	key := sha256.Sum256([]byte("profile-password:" + uid))
	var count int
	err := r.db.QueryRowContext(ctx, `INSERT INTO auth_login_limits(key_hash,request_count) VALUES($1,1)
ON CONFLICT(key_hash) DO UPDATE SET request_count=CASE WHEN auth_login_limits.window_started_at<=clock_timestamp()-interval '1 minute' THEN 1 ELSE auth_login_limits.request_count+1 END,
window_started_at=CASE WHEN auth_login_limits.window_started_at<=clock_timestamp()-interval '1 minute' THEN clock_timestamp() ELSE auth_login_limits.window_started_at END,updated_at=clock_timestamp() RETURNING request_count`, key[:]).Scan(&count)
	if err != nil {
		return err
	}
	if count > 5 {
		return &auth.Limited{Retry: time.Minute}
	}
	return nil
}

func (r *SQLRepository) ChangePassword(ctx context.Context, token, expected, hash string) error {
	return r.transaction(ctx, token, true, func(tx *sql.Tx, u auth.User, _ string) error {
		var current string
		if err := tx.QueryRowContext(ctx, "SELECT password_hash FROM admin_users WHERE id=$1", u.ID).Scan(&current); err != nil {
			return err
		}
		if current != expected {
			return auth.ErrUnauthorized
		}
		if _, err := tx.ExecContext(ctx, `UPDATE admin_users SET password_hash=$2,auth_version=auth_version+1,password_changed_at=clock_timestamp(),updated_at=clock_timestamp() WHERE id=$1`, u.ID, hash); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "UPDATE admin_sessions SET revoked_at=clock_timestamp(),updated_at=clock_timestamp() WHERE user_id=$1 AND revoked_at IS NULL", u.ID); err != nil {
			return err
		}
		return audit.Insert(ctx, tx, u.ID, "password_changed", "user", u.ID)
	})
}

func (r *SQLRepository) Sessions(ctx context.Context, token string, page, size int) (SessionPage, error) {
	out := SessionPage{Items: []Session{}, Page: page, PageSize: size}
	err := r.transaction(ctx, token, false, func(tx *sql.Tx, u auth.User, sid string) error {
		const filter = ` FROM admin_sessions s JOIN admin_users u ON u.id=s.user_id WHERE s.user_id=$1 AND s.revoked_at IS NULL AND s.expires_at>clock_timestamp() AND s.last_seen_at>clock_timestamp()-make_interval(secs=>$2) AND s.user_auth_version=u.auth_version`
		if err := tx.QueryRowContext(ctx, "SELECT count(*)"+filter, u.ID, r.idle.Seconds()).Scan(&out.Total); err != nil {
			return err
		}
		rows, err := tx.QueryContext(ctx, "SELECT s.id,s.created_at,s.last_seen_at,s.expires_at"+filter+" ORDER BY s.created_at DESC,s.id DESC LIMIT $3 OFFSET $4", u.ID, r.idle.Seconds(), size, (page-1)*size)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var s Session
			if err = rows.Scan(&s.ID, &s.CreatedAt, &s.LastSeenAt, &s.ExpiresAt); err != nil {
				return err
			}
			s.Current = s.ID == sid
			out.Items = append(out.Items, s)
		}
		return rows.Err()
	})
	return out, err
}

func (r *SQLRepository) Revoke(ctx context.Context, token, id string, others bool) (bool, error) {
	current := false
	err := r.transaction(ctx, token, true, func(tx *sql.Tx, u auth.User, sid string) error {
		if !others {
			var owned string
			if err := tx.QueryRowContext(ctx, "SELECT id FROM admin_sessions WHERE id=$1 AND user_id=$2", id, u.ID).Scan(&owned); errors.Is(err, sql.ErrNoRows) {
				return ErrNotFound
			} else if err != nil {
				return err
			}
			current = id == sid
		}
		query := "UPDATE admin_sessions SET revoked_at=clock_timestamp(),updated_at=clock_timestamp() WHERE user_id=$1 AND revoked_at IS NULL AND id=$2"
		action := "session_revoked"
		if others {
			query = "UPDATE admin_sessions SET revoked_at=clock_timestamp(),updated_at=clock_timestamp() WHERE user_id=$1 AND revoked_at IS NULL AND id<>$2"
			id = sid
			action = "sessions_revoked"
		}
		if _, err := tx.ExecContext(ctx, query, u.ID, id); err != nil {
			return err
		}
		return audit.Insert(ctx, tx, u.ID, action, "user", u.ID)
	})
	return current, err
}
