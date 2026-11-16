package auth

import (
	"context"
	"database/sql"
	"errors"
	"net"
	"time"
)

type Repository interface {
	Initialize(context.Context, InitialAdmin, string) (User, error)
	Find(context.Context, string) (account, error)
	Attempt(context.Context, []byte, []byte, LoginOutcome) error
	Failure(context.Context, []byte, LoginOutcome) error
	Create(context.Context, account, []byte, []byte, time.Duration, LoginOutcome) (User, error)
	Authenticate(context.Context, []byte, time.Duration) (session, error)
	Revoke(context.Context, []byte) error
	RevokeAll(context.Context, []byte, time.Duration) error
}

type SQLRepository struct{ db *sql.DB }

func NewRepository(db *sql.DB) *SQLRepository { return &SQLRepository{db: db} }

func (r *SQLRepository) Initialize(ctx context.Context, input InitialAdmin, hash string) (User, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return User{}, err
	}
	defer tx.Rollback()
	// The permanent core row is the shared serialization point for super-admin mutations.
	var guard string
	if err = tx.QueryRowContext(ctx, "SELECT id FROM app_metadata WHERE component='core' FOR UPDATE").Scan(&guard); err != nil {
		return User{}, err
	}
	var exists bool
	if err = tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM admin_users) OR EXISTS(SELECT 1 FROM app_metadata WHERE component='auth_initialized')").Scan(&exists); err != nil {
		return User{}, err
	}
	if exists {
		return User{}, ErrConflict
	}
	u := User{Username: input.Username, Email: input.Email, DisplayName: input.DisplayName}
	if err = tx.QueryRowContext(ctx, "INSERT INTO admin_users(username,email,password_hash,display_name) VALUES($1,$2,$3,$4) RETURNING id", u.Username, u.Email, hash, u.DisplayName).Scan(&u.ID); err != nil {
		return User{}, err
	}
	var role string
	if err = tx.QueryRowContext(ctx, "INSERT INTO roles(code,name,is_super_admin,is_system) VALUES('super_admin','Super administrator',true,true) RETURNING id").Scan(&role); err != nil {
		return User{}, err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO user_roles(user_id,role_id) VALUES($1,$2)", u.ID, role); err != nil {
		return User{}, err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO app_metadata(component,schema_version) VALUES('auth_initialized',1)"); err != nil {
		return User{}, err
	}
	return u, tx.Commit()
}

func (r *SQLRepository) Find(ctx context.Context, name string) (account, error) {
	var a account
	err := r.db.QueryRowContext(ctx, "SELECT id,username,email,display_name,password_hash,status,auth_version FROM admin_users WHERE lower(username)=$1", name).Scan(&a.ID, &a.Username, &a.Email, &a.DisplayName, &a.PasswordHash, &a.Status, &a.AuthVersion)
	if errors.Is(err, sql.ErrNoRows) {
		return a, ErrCredentials
	}
	return a, err
}

// Each request consumes both account and peer-IP buckets, transactionally and before hashing.
// Fixed windows cap account requests at 20/minute and shared-proxy IP requests at 120/minute.
func (r *SQLRepository) Attempt(ctx context.Context, accountKey, ipKey []byte, event LoginOutcome) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var limited time.Duration
	for i, key := range [][]byte{accountKey, ipKey} {
		var count int
		var retry float64
		err = tx.QueryRowContext(ctx, `INSERT INTO auth_login_limits(key_hash,request_count) VALUES($1,1)
ON CONFLICT(key_hash) DO UPDATE SET
request_count=CASE WHEN auth_login_limits.window_started_at <= clock_timestamp()-interval '1 minute' THEN 1 ELSE auth_login_limits.request_count+1 END,
window_started_at=CASE WHEN auth_login_limits.window_started_at <= clock_timestamp()-interval '1 minute' THEN clock_timestamp() ELSE auth_login_limits.window_started_at END,
failures=CASE WHEN auth_login_limits.updated_at <= clock_timestamp()-interval '15 minutes' THEN 0 ELSE auth_login_limits.failures END,
updated_at=clock_timestamp()
RETURNING request_count, greatest(0,extract(epoch FROM blocked_until-clock_timestamp()))`, key).Scan(&count, &retry)
		if err != nil {
			return err
		}
		limit := 20
		if i == 1 {
			limit = 120
		}
		if count > limit {
			retry = 60
		}
		if time.Duration(retry*float64(time.Second)) > limited {
			limited = time.Duration(retry * float64(time.Second))
		}
	}
	if limited > 0 {
		event.Outcome = "rate_limited"
		if err = insertLoginEvent(ctx, tx, event); err != nil {
			return err
		}
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	if limited > 0 {
		return &Limited{Retry: limited}
	}
	return nil
}

func (r *SQLRepository) Failure(ctx context.Context, key []byte, event LoginOutcome) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `UPDATE auth_login_limits SET failures=least(failures+1,30),
blocked_until=CASE WHEN failures+1>=5 THEN clock_timestamp()+make_interval(secs=>least(60,power(2,least(failures-4,6)))::double precision) ELSE NULL END,
updated_at=clock_timestamp() WHERE key_hash=$1`, key)
	if err != nil {
		return err
	}
	event.Outcome = "failure"
	if err = insertLoginEvent(ctx, tx, event); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *SQLRepository) Create(ctx context.Context, a account, hash, key []byte, ttl time.Duration, event LoginOutcome) (User, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return User{}, err
	}
	defer tx.Rollback()
	var current account
	err = tx.QueryRowContext(ctx, "SELECT id,username,email,display_name,password_hash,status,auth_version FROM admin_users WHERE id=$1 FOR UPDATE", a.ID).Scan(&current.ID, &current.Username, &current.Email, &current.DisplayName, &current.PasswordHash, &current.Status, &current.AuthVersion)
	if err != nil {
		return User{}, err
	}
	if current.Status != "active" || current.AuthVersion != a.AuthVersion || current.PasswordHash != a.PasswordHash {
		return User{}, ErrCredentials
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO admin_sessions(user_id,token_hash,user_auth_version,expires_at) VALUES($1,$2,$3,clock_timestamp()+make_interval(secs=>$4))", a.ID, hash, a.AuthVersion, ttl.Seconds()); err != nil {
		return User{}, err
	}
	if _, err = tx.ExecContext(ctx, "UPDATE admin_users SET last_login_at=clock_timestamp(),updated_at=clock_timestamp() WHERE id=$1", a.ID); err != nil {
		return User{}, err
	}
	if _, err = tx.ExecContext(ctx, "UPDATE auth_login_limits SET failures=0,blocked_until=NULL WHERE key_hash=$1", key); err != nil {
		return User{}, err
	}
	event.UserID = a.ID
	event.Outcome = "success"
	if err = insertLoginEvent(ctx, tx, event); err != nil {
		return User{}, err
	}
	return current.User, tx.Commit()
}

func insertLoginEvent(ctx context.Context, tx *sql.Tx, event LoginOutcome) error {
	var userID, ip any
	if event.UserID != "" {
		userID = event.UserID
	}
	if parsed := net.ParseIP(event.IP); parsed != nil {
		ip = parsed.String()
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO login_events(user_id,account_kind,outcome,client_ip,user_agent)
VALUES($1,$2,$3,$4,$5)`, userID, map[bool]string{true: "known", false: "unknown"}[userID != nil], event.Outcome, ip, event.UserAgent)
	return err
}

func authenticate(ctx context.Context, tx *sql.Tx, hash []byte, idle time.Duration) (session, error) {
	var s session
	var uid string
	if err := tx.QueryRowContext(ctx, "SELECT user_id FROM admin_sessions WHERE token_hash=$1", hash).Scan(&uid); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return s, ErrUnauthorized
		}
		return s, err
	}
	// Login, authenticate and logout-all use the same user -> session lock order.
	var status string
	var version int64
	err := tx.QueryRowContext(ctx, "SELECT id,username,email,display_name,status,auth_version FROM admin_users WHERE id=$1 FOR UPDATE", uid).Scan(&s.User.ID, &s.User.Username, &s.User.Email, &s.User.DisplayName, &status, &version)
	if errors.Is(err, sql.ErrNoRows) {
		return s, ErrUnauthorized
	}
	if err != nil {
		return s, err
	}
	if status != "active" {
		return s, ErrUnauthorized
	}
	err = tx.QueryRowContext(ctx, `UPDATE admin_sessions SET last_seen_at=clock_timestamp(),updated_at=clock_timestamp()
WHERE token_hash=$1 AND revoked_at IS NULL AND expires_at>clock_timestamp()
AND last_seen_at>clock_timestamp()-make_interval(secs=>$2) AND user_auth_version=$3 RETURNING id`, hash, idle.Seconds(), version).Scan(&s.ID)
	if errors.Is(err, sql.ErrNoRows) {
		return s, ErrUnauthorized
	}
	return s, err
}

// AuthenticateTx lets self-service writes share Auth's live validation and lock order.
// The caller owns the transaction; no raw token is returned or persisted.
func AuthenticateTx(ctx context.Context, tx *sql.Tx, token string, idle time.Duration) (User, string, error) {
	if !validToken(token) {
		return User{}, "", ErrUnauthorized
	}
	s, err := authenticate(ctx, tx, tokenHash(token), idle)
	return s.User, s.ID, err
}

func (r *SQLRepository) Authenticate(ctx context.Context, hash []byte, idle time.Duration) (session, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return session{}, err
	}
	defer tx.Rollback()
	s, err := authenticate(ctx, tx, hash, idle)
	if err != nil {
		return s, err
	}
	return s, tx.Commit()
}
func (r *SQLRepository) Revoke(ctx context.Context, hash []byte) error {
	_, err := r.db.ExecContext(ctx, "UPDATE admin_sessions SET revoked_at=clock_timestamp(),updated_at=clock_timestamp() WHERE token_hash=$1 AND revoked_at IS NULL", hash)
	return err
}
func (r *SQLRepository) RevokeAll(ctx context.Context, hash []byte, idle time.Duration) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	s, err := authenticate(ctx, tx, hash, idle)
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "UPDATE admin_sessions SET revoked_at=clock_timestamp(),updated_at=clock_timestamp() WHERE user_id=$1 AND revoked_at IS NULL", s.User.ID); err != nil {
		return err
	}
	return tx.Commit()
}
