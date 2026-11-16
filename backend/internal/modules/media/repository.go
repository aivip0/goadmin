package media

import (
	"context"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"time"
)

type Repository struct{ db *sql.DB }

func NewRepository(db *sql.DB) *Repository { return &Repository{db: db} }

func (r *Repository) write(ctx context.Context, actor, permission string, fn func(*sql.Tx) error) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var core string
	if err = tx.QueryRowContext(ctx, `SELECT id::text FROM app_metadata WHERE component='core' FOR UPDATE`).Scan(&core); err != nil {
		return err
	}
	var allowed bool
	err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM user_roles ur JOIN roles r ON r.id=ur.role_id AND r.status='active' AND r.is_super_admin JOIN admin_users u ON u.id=ur.user_id AND u.status='active' WHERE u.id=$1)
OR EXISTS(SELECT 1 FROM user_roles ur JOIN roles r ON r.id=ur.role_id AND r.status='active' JOIN role_permissions rp ON rp.role_id=r.id JOIN permissions p ON p.id=rp.permission_id JOIN admin_users u ON u.id=ur.user_id AND u.status='active' WHERE u.id=$1 AND p.code=$2)`, actor, permission).Scan(&allowed)
	if err != nil {
		return err
	}
	if !allowed {
		return ErrForbidden
	}
	if err = fn(tx); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *Repository) generation(ctx context.Context) (string, error) {
	var generation, state string
	err := r.db.QueryRowContext(ctx, `SELECT active_verify_generation::text,verification_state FROM media_storage_binding WHERE singleton=true`).Scan(&generation, &state)
	if err != nil || state != "complete" {
		return "", ErrNotReady
	}
	return generation, nil
}

func (r *Repository) Pending(ctx context.Context, actor string, f *File) error {
	return r.write(ctx, actor, "media:upload", func(tx *sql.Tx) error {
		var state string
		if err := tx.QueryRowContext(ctx, `SELECT verification_state FROM media_storage_binding WHERE singleton=true`).Scan(&state); err != nil || state != "complete" {
			return ErrNotReady
		}
		return tx.QueryRowContext(ctx, `INSERT INTO media_files(upload_operation_id,storage_key,original_name,extension,mime_type,byte_size,sha256,width,height,uploaded_by,status,next_reconcile_at)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,'pending',clock_timestamp()+interval '2 hours') RETURNING id::text,created_at`, f.OperationID, f.Key, f.OriginalName, extension(f.OriginalName), f.MIME, f.Size, f.Digest[:], f.Width, f.Height, actor).Scan(&f.ID, &f.CreatedAt)
	})
}

func extension(name string) string { _, ext, _ := validateName(name); return ext }

func (r *Repository) Ready(ctx context.Context, actor string, f *File) error {
	return r.write(ctx, actor, "media:upload", func(tx *sql.Tx) error {
		var generation string
		if err := tx.QueryRowContext(ctx, `SELECT active_verify_generation::text FROM media_storage_binding WHERE singleton=true AND verification_state='complete'`).Scan(&generation); err != nil {
			return ErrNotReady
		}
		res, err := tx.ExecContext(ctx, `UPDATE media_files SET status='ready',verified_generation=$4,next_reconcile_at=NULL,updated_at=clock_timestamp() WHERE id=$1 AND upload_operation_id=$2 AND storage_key=$3 AND status='pending' AND reconcile_claim_id IS NULL`, f.ID, f.OperationID, f.Key, generation)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n != 1 {
			return ErrConflict
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO security_events(actor_user_id,action,target_type,target_id) VALUES($1,'media_uploaded','media',$2)`, actor, f.ID)
		return err
	})
}

func (r *Repository) Fail(ctx context.Context, f *File, code string) error {
	_, err := r.db.ExecContext(ctx, `UPDATE media_files SET status='failed',next_reconcile_at=NULL,last_error_code=$2,updated_at=clock_timestamp() WHERE id=$1 AND status='pending'`, f.ID, code)
	return err
}

const fileFields = `id::text,upload_operation_id::text,storage_key,original_name,mime_type,byte_size,sha256,width,height,created_at`

func scanFile(row interface{ Scan(...any) error }) (File, error) {
	var f File
	var d []byte
	if err := row.Scan(&f.ID, &f.OperationID, &f.Key, &f.OriginalName, &f.MIME, &f.Size, &d, &f.Width, &f.Height, &f.CreatedAt); err != nil {
		return f, err
	}
	if len(d) != 32 {
		return f, ErrCorrupt
	}
	copy(f.Digest[:], d)
	f.SHA256 = hex.EncodeToString(d)
	return f, nil
}

func (r *Repository) Get(ctx context.Context, id string) (File, error) {
	f, err := scanFile(r.db.QueryRowContext(ctx, `SELECT `+fileFields+` FROM media_files m WHERE id=$1 AND status='ready' AND verified_generation=(SELECT active_verify_generation FROM media_storage_binding WHERE singleton=true AND verification_state='complete')`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return f, ErrNotFound
	}
	return f, err
}

func (r *Repository) List(ctx context.Context, page, size int, kind, query string) ([]File, int64, error) {
	if _, err := r.generation(ctx); err != nil {
		return nil, 0, err
	}
	filter := ` WHERE status='ready' AND verified_generation=(SELECT active_verify_generation FROM media_storage_binding WHERE singleton=true AND verification_state='complete') AND ($1='' OR ($1='image' AND mime_type LIKE 'image/%') OR ($1='document' AND mime_type NOT LIKE 'image/%')) AND ($2='' OR original_name ILIKE '%'||$2||'%')`
	var total int64
	if err := r.db.QueryRowContext(ctx, `SELECT count(*) FROM media_files`+filter, kind, query).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := r.db.QueryContext(ctx, `SELECT `+fileFields+` FROM media_files`+filter+` ORDER BY created_at DESC,id DESC LIMIT $3 OFFSET $4`, kind, query, size, (page-1)*size)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := []File{}
	for rows.Next() {
		f, e := scanFile(rows)
		if e != nil {
			return nil, 0, e
		}
		items = append(items, f)
	}
	return items, total, rows.Err()
}

func (r *Repository) BeginDelete(ctx context.Context, actor, id string) (File, error) {
	var f File
	err := r.write(ctx, actor, "media:delete", func(tx *sql.Tx) error {
		var e error
		f, e = scanFile(tx.QueryRowContext(ctx, `SELECT `+fileFields+` FROM media_files WHERE id=$1 AND status='ready' FOR UPDATE`, id))
		if errors.Is(e, sql.ErrNoRows) {
			return ErrNotFound
		}
		if e != nil {
			return e
		}
		var referenced bool
		if e := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM system_settings WHERE media_file_id=$1)`, id).Scan(&referenced); e != nil {
			return e
		}
		if referenced {
			return ErrConflict
		}
		_, e = tx.ExecContext(ctx, `UPDATE media_files SET status='deleting',verified_generation=NULL,next_reconcile_at=clock_timestamp(),updated_at=clock_timestamp() WHERE id=$1`, id)
		return e
	})
	return f, err
}

func (r *Repository) Deleted(ctx context.Context, actor string, f File) error {
	return r.write(ctx, actor, "media:delete", func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `UPDATE media_files SET status='deleted',deleted_at=clock_timestamp(),next_reconcile_at=NULL,updated_at=clock_timestamp() WHERE id=$1 AND status IN ('deleting','delete_failed')`, f.ID)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n != 1 {
			return ErrConflict
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO security_events(actor_user_id,action,target_type,target_id) VALUES($1,'media_deleted','media',$2)`, actor, f.ID)
		return err
	})
}

func (r *Repository) DeleteFailed(ctx context.Context, f File) error {
	_, err := r.db.ExecContext(ctx, `UPDATE media_files SET status='delete_failed',next_reconcile_at=clock_timestamp()+interval '1 minute',reconcile_attempts=1,last_error_code='storage_delete',updated_at=clock_timestamp() WHERE id=$1 AND status='deleting'`, f.ID)
	return err
}

func (r *Repository) RecoveredReady(ctx context.Context, actor, id, claim string) error {
	return r.write(ctx, actor, "media:upload", func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `UPDATE media_files SET status='ready',verified_generation=(SELECT active_verify_generation FROM media_storage_binding WHERE singleton=true AND verification_state='complete'),next_reconcile_at=NULL,reconcile_claim_id=NULL,reconcile_claim_until=NULL,updated_at=clock_timestamp() WHERE id=$1 AND status='pending' AND reconcile_claim_id=$2`, id, claim)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n != 1 {
			return ErrConflict
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO security_events(actor_user_id,action,target_type,target_id) VALUES($1,'media_recovered','media',$2)`, actor, id)
		return err
	})
}

func (r *Repository) RecoveredDeleted(ctx context.Context, actor, id, claim string) error {
	return r.write(ctx, actor, "media:delete", func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `UPDATE media_files SET status='deleted',deleted_at=clock_timestamp(),next_reconcile_at=NULL,reconcile_claim_id=NULL,reconcile_claim_until=NULL,updated_at=clock_timestamp() WHERE id=$1 AND status IN ('deleting','delete_failed') AND reconcile_claim_id=$2`, id, claim)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n != 1 {
			return ErrConflict
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO security_events(actor_user_id,action,target_type,target_id) VALUES($1,'media_recovered_delete','media',$2)`, actor, id)
		return err
	})
}

func (r *Repository) Unavailable(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx, `UPDATE media_files SET status='unavailable',verified_generation=NULL,last_error_code='integrity',updated_at=clock_timestamp() WHERE id=$1 AND status='ready'`, id)
	return err
}

func (r *Repository) VerifyAll(ctx context.Context, s Storage) error {
	gen := uuid()
	if _, err := r.db.ExecContext(ctx, `UPDATE media_storage_binding SET active_verify_generation=$1,verification_state='checking',verified_at=NULL WHERE singleton=true`, gen); err != nil {
		return err
	}
	// Stable keyset traversal, no unbounded result buffering.
	var cursor string
	for {
		rows, err := r.db.QueryContext(ctx, `SELECT `+fileFields+` FROM media_files WHERE status='ready' AND ($1='' OR id::text>$1) ORDER BY id LIMIT 50`, cursor)
		if err != nil {
			return err
		}
		batch := []File{}
		for rows.Next() {
			f, e := scanFile(rows)
			if e != nil {
				rows.Close()
				return e
			}
			batch = append(batch, f)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		if len(batch) == 0 {
			break
		}
		for _, f := range batch {
			cursor = f.ID
			stream, e := s.OpenVerified(ctx, f.Key, f.Size, f.Digest)
			if e == nil {
				e = stream.Close()
			}
			if errors.Is(e, ErrCorrupt) || errors.Is(e, os.ErrNotExist) {
				if e = r.Unavailable(ctx, f.ID); e != nil {
					return e
				}
				continue
			}
			if e != nil {
				return e
			}
			if _, e = r.db.ExecContext(ctx, `UPDATE media_files SET verified_generation=$2 WHERE id=$1 AND status='ready'`, f.ID, gen); e != nil {
				return e
			}
		}
	}
	_, err := r.db.ExecContext(ctx, `UPDATE media_storage_binding SET verification_state='complete',verified_at=clock_timestamp() WHERE singleton=true AND active_verify_generation=$1 AND verification_state='checking'`, gen)
	return err
}

func (r *Repository) Bind(ctx context.Context, rootID, actor string) error {
	if !uuidName.MatchString(rootID) || !uuidName.MatchString(actor) {
		return ErrInvalid
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var core string
	if err = tx.QueryRowContext(ctx, `SELECT id::text FROM app_metadata WHERE component='core' FOR UPDATE`).Scan(&core); err != nil {
		return err
	}
	var exists bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM media_storage_binding)`).Scan(&exists); err != nil {
		return err
	}
	if exists {
		return ErrConflict
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO media_storage_binding(root_id) VALUES($1)`, rootID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO security_events(actor_user_id,action,target_type,target_id) VALUES($1,'media_root_bound','media',$2)`, actor, rootID); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *Repository) RootID(ctx context.Context) (string, error) {
	var id string
	err := r.db.QueryRowContext(ctx, `SELECT root_id::text FROM media_storage_binding WHERE singleton=true`).Scan(&id)
	return id, err
}

// QuarantineOrphan serializes with Pending's core lock, so a new row cannot
// claim the key between the absence check and the no-replace quarantine move.
func (r *Repository) QuarantineOrphan(ctx context.Context, s *LocalStorage, key string) (bool, error) {
	if !keyName.MatchString(key) {
		return false, ErrInvalid
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var core string
	if err = tx.QueryRowContext(ctx, `SELECT id::text FROM app_metadata WHERE component='core' FOR UPDATE`).Scan(&core); err != nil {
		return false, err
	}
	var exists bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM media_files WHERE storage_key=$1)`, key).Scan(&exists); err != nil || exists {
		return false, err
	}
	if err = s.QuarantineObject(key); err != nil {
		return false, err
	}
	return true, tx.Commit()
}

// RetryOne is an audited, exact-state recovery. It cannot revive a ready or
// deleted row and never clears an active maintenance claim.
func (r *Repository) RetryOne(ctx context.Context, actor, id, expected string) error {
	if !uuidName.MatchString(id) || (expected != "failed" && expected != "delete_failed") {
		return ErrInvalid
	}
	permission := "media:upload"
	next := "pending"
	if expected == "delete_failed" {
		permission = "media:delete"
		next = "delete_failed"
	}
	return r.write(ctx, actor, permission, func(tx *sql.Tx) error {
		var actual string
		var activeClaim bool
		if err := tx.QueryRowContext(ctx, `SELECT status,COALESCE(reconcile_claim_until>clock_timestamp(),false) FROM media_files WHERE id=$1 FOR UPDATE`, id).Scan(&actual, &activeClaim); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrNotFound
			}
			return err
		}
		if actual != expected || activeClaim {
			return ErrConflict
		}
		if _, err := tx.ExecContext(ctx, `UPDATE media_files SET status=$2,reconcile_attempts=0,next_reconcile_at=clock_timestamp()+interval '1 minute',last_error_code=NULL,reconcile_claim_id=NULL,reconcile_claim_until=NULL,updated_at=clock_timestamp() WHERE id=$1`, id, next); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO security_events(actor_user_id,action,target_type,target_id) VALUES($1,'media_retry','media',$2)`, actor, id)
		return err
	})
}

const apiLockKey int64 = 0x676f61646d656469

func (r *Repository) LockInstance(ctx context.Context) (*sql.Conn, error) {
	c, err := r.db.Conn(ctx)
	if err != nil {
		return nil, err
	}
	var ok bool
	if err = c.QueryRowContext(ctx, `SELECT pg_try_advisory_lock($1)`, apiLockKey).Scan(&ok); err != nil || !ok {
		c.Close()
		return nil, fmt.Errorf("media instance already active or unavailable")
	}
	return c, nil
}

func (r *Repository) CheckLock(ctx context.Context, c *sql.Conn) error {
	var v int
	return c.QueryRowContext(ctx, `SELECT 1`).Scan(&v)
}
func retryAt(n int) time.Duration { return time.Duration(1<<min(n, 5)) * time.Minute }
