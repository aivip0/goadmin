package jobs

import (
	"context"
	"database/sql"
	"errors"
	"github.com/lib/pq"
	"goadminkit/backend/internal/core/audit"
)

type Repository struct{ db *sql.DB }

func NewRepository(db *sql.DB) *Repository { return &Repository{db} }

const cols = `id::text,kind,status,requested_by::text,retry_of::text,attempts,run_after,lease_until,last_failure,affected_rows,created_at,updated_at`

func scan(row interface{ Scan(...any) error }) (Item, error) {
	var v Item
	err := row.Scan(&v.ID, &v.Kind, &v.Status, &v.RequestedBy, &v.RetryOf, &v.Attempts, &v.RunAfter, &v.LeaseUntil, &v.LastFailure, &v.AffectedRows, &v.CreatedAt, &v.UpdatedAt)
	return v, err
}
func (r *Repository) List(ctx context.Context, page, size int) (Page, error) {
	v := Page{Items: []Item{}, Page: page, PageSize: size}
	if err := r.db.QueryRowContext(ctx, `SELECT count(*) FROM admin_jobs`).Scan(&v.Total); err != nil {
		return v, err
	}
	rows, err := r.db.QueryContext(ctx, `SELECT `+cols+` FROM admin_jobs ORDER BY created_at DESC,id DESC LIMIT $1 OFFSET $2`, size, (page-1)*size)
	if err != nil {
		return v, err
	}
	defer rows.Close()
	for rows.Next() {
		item, e := scan(rows)
		if e != nil {
			return v, e
		}
		v.Items = append(v.Items, item)
	}
	return v, rows.Err()
}
func (r *Repository) Detail(ctx context.Context, id string) (Detail, error) {
	item, err := scan(r.db.QueryRowContext(ctx, `SELECT `+cols+` FROM admin_jobs WHERE id=$1`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Detail{}, ErrNotFound
	}
	if err != nil {
		return Detail{}, err
	}
	result := Detail{Item: item, Runs: []Run{}}
	rows, err := r.db.QueryContext(ctx, `SELECT id::text,job_id::text,attempt,status,failure_code,affected_rows,started_at,finished_at FROM admin_job_runs WHERE job_id=$1 ORDER BY attempt DESC`, id)
	if err != nil {
		return Detail{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var v Run
		if e := rows.Scan(&v.ID, &v.JobID, &v.Attempt, &v.Status, &v.FailureCode, &v.AffectedRows, &v.StartedAt, &v.FinishedAt); e != nil {
			return Detail{}, e
		}
		result.Runs = append(result.Runs, v)
	}
	return result, rows.Err()
}
func (r *Repository) Enqueue(ctx context.Context, actor, kind, retryOf string) (Item, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return Item{}, err
	}
	defer tx.Rollback()
	var guard string
	if err = tx.QueryRowContext(ctx, `SELECT id::text FROM app_metadata WHERE component='core' FOR UPDATE`).Scan(&guard); err != nil {
		return Item{}, err
	}
	permission := "jobs:create"
	if retryOf != "" {
		permission = "jobs:retry"
	}
	var allowed bool
	err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM admin_users u JOIN user_roles ur ON ur.user_id=u.id
 JOIN roles r ON r.id=ur.role_id AND r.status='active' WHERE u.id=$1 AND u.status='active' AND r.is_super_admin)
 OR EXISTS(SELECT 1 FROM admin_users u JOIN user_roles ur ON ur.user_id=u.id JOIN roles r ON r.id=ur.role_id AND r.status='active'
 JOIN role_permissions rp ON rp.role_id=r.id JOIN permissions p ON p.id=rp.permission_id
 WHERE u.id=$1 AND u.status='active' AND p.code=$2)`, actor, permission).Scan(&allowed)
	if err != nil {
		return Item{}, err
	}
	if !allowed {
		return Item{}, ErrForbidden
	}
	if retryOf != "" {
		var status string
		err = tx.QueryRowContext(ctx, `SELECT status FROM admin_jobs WHERE id=$1 FOR UPDATE`, retryOf).Scan(&status)
		if errors.Is(err, sql.ErrNoRows) {
			return Item{}, ErrNotFound
		}
		if err != nil {
			return Item{}, err
		}
		if status != "failed" {
			return Item{}, ErrConflict
		}
	}
	var retry any
	if retryOf != "" {
		retry = retryOf
	}
	v, err := scan(tx.QueryRowContext(ctx, `INSERT INTO admin_jobs(kind,requested_by,retry_of) VALUES($1,$2,$3) RETURNING `+cols, kind, actor, retry))
	var pg *pq.Error
	if errors.As(err, &pg) && string(pg.Code) == "23505" {
		return Item{}, ErrConflict
	}
	if err != nil {
		return Item{}, err
	}
	if err = audit.Insert(ctx, tx, actor, "job_queued", "job", v.ID); err != nil {
		return Item{}, err
	}
	return v, tx.Commit()
}

type claim struct {
	id      string
	attempt int
}

func (r *Repository) Claim(ctx context.Context) (claim, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return claim{}, err
	}
	defer tx.Rollback()
	var id string
	var attempt int
	var status string
	err = tx.QueryRowContext(ctx, `SELECT id::text,attempts,status FROM admin_jobs WHERE
 (status='queued' AND run_after<=clock_timestamp()) OR (status='running' AND lease_until<clock_timestamp())
 ORDER BY run_after,created_at,id FOR UPDATE SKIP LOCKED LIMIT 1`).Scan(&id, &attempt, &status)
	if err != nil {
		return claim{}, err
	}
	if status == "running" {
		if _, err = tx.ExecContext(ctx, `UPDATE admin_job_runs SET status='failed',failure_code='lease_expired',finished_at=clock_timestamp() WHERE job_id=$1 AND attempt=$2 AND status='running'`, id, attempt); err != nil {
			return claim{}, err
		}
	}
	if attempt >= 3 {
		if _, err = tx.ExecContext(ctx, `UPDATE admin_jobs SET status='failed',lease_until=NULL,last_failure='lease_expired',updated_at=clock_timestamp() WHERE id=$1`, id); err != nil {
			return claim{}, err
		}
		return claim{}, tx.Commit()
	}
	attempt++
	if _, err = tx.ExecContext(ctx, `UPDATE admin_jobs SET status='running',attempts=$2,lease_until=clock_timestamp()+interval '30 seconds',updated_at=clock_timestamp() WHERE id=$1`, id, attempt); err != nil {
		return claim{}, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO admin_job_runs(job_id,attempt,status) VALUES($1,$2,'running')`, id, attempt); err != nil {
		return claim{}, err
	}
	return claim{id, attempt}, tx.Commit()
}
func (r *Repository) Finish(ctx context.Context, c claim) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var count int
	// The cleanup and completion are one transaction. A stale claimant cannot commit a second run.
	err = tx.QueryRowContext(ctx, `SELECT attempts FROM admin_jobs WHERE id=$1 AND status='running' AND attempts=$2 AND lease_until>clock_timestamp() FOR UPDATE`, c.id, c.attempt).Scan(&count)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrConflict
	}
	if err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `DELETE FROM admin_sessions WHERE id IN
 (SELECT id FROM admin_sessions WHERE expires_at<clock_timestamp()-interval '30 days' ORDER BY expires_at,id LIMIT 100 FOR UPDATE SKIP LOCKED)`)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE admin_job_runs SET status='succeeded',affected_rows=$3,finished_at=clock_timestamp() WHERE job_id=$1 AND attempt=$2`, c.id, c.attempt, affected); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE admin_jobs SET status='succeeded',lease_until=NULL,affected_rows=$2,last_failure=NULL,updated_at=clock_timestamp() WHERE id=$1`, c.id, affected); err != nil {
		return err
	}
	return tx.Commit()
}
func (r *Repository) Fail(ctx context.Context, c claim) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var current int
	err = tx.QueryRowContext(ctx, `SELECT attempts FROM admin_jobs WHERE id=$1 AND status='running' AND attempts=$2 FOR UPDATE`, c.id, c.attempt).Scan(&current)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrConflict
	}
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE admin_job_runs SET status='failed',failure_code='execution_failed',finished_at=clock_timestamp() WHERE job_id=$1 AND attempt=$2`, c.id, c.attempt); err != nil {
		return err
	}
	status := "queued"
	if c.attempt >= 3 {
		status = "failed"
	}
	_, err = tx.ExecContext(ctx, `UPDATE admin_jobs SET status=$2,lease_until=NULL,last_failure='execution_failed',run_after=clock_timestamp()+($3 * interval '1 minute'),updated_at=clock_timestamp() WHERE id=$1`, c.id, status, c.attempt*c.attempt)
	if err != nil {
		return err
	}
	return tx.Commit()
}
