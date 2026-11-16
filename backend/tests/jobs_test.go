package tests

import (
	"context"
	"errors"
	"goadminkit/backend/internal/modules/auth"
	"goadminkit/backend/internal/modules/jobs"
	"sync"
	"testing"
	"time"
)

func TestPostgreSQLJobs(t *testing.T) {
	db, _ := authDatabase(t)
	ctx := context.Background()
	identity, err := auth.NewService(auth.NewRepository(db.SQL), time.Hour, 30*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	actor, err := identity.Initialize(ctx, auth.InitialAdmin{Username: "job_root", Email: "job_root@example.test", Password: "isolated jobs test password"})
	if err != nil {
		t.Fatal(err)
	}
	svc := jobs.NewService(jobs.NewRepository(db.SQL))
	if _, err = svc.Enqueue(ctx, actor.ID, "arbitrary_execution"); !errors.Is(err, jobs.ErrInvalid) {
		t.Fatal("arbitrary task accepted", err)
	}
	job, err := svc.Enqueue(ctx, actor.ID, jobs.SessionCleanup)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = svc.Retry(ctx, actor.ID, job.ID); !errors.Is(err, jobs.ErrConflict) {
		t.Fatal("running task retried", err)
	}
	_, err = db.SQL.ExecContext(ctx, `INSERT INTO admin_sessions(user_id,token_hash,user_auth_version,created_at,expires_at,last_seen_at)
 VALUES($1,decode(repeat('ab',32),'hex'),1,clock_timestamp()-interval '45 days',clock_timestamp()-interval '40 days',clock_timestamp()-interval '45 days')`, actor.ID)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errorsCh := make(chan error, 4)
	for i := 0; i < 4; i++ {
		wg.Go(func() { _, e := svc.RunOnce(ctx); errorsCh <- e })
	}
	wg.Wait()
	close(errorsCh)
	for e := range errorsCh {
		if e != nil {
			t.Fatal(e)
		}
	}
	detail, err := svc.Detail(ctx, job.ID)
	if err != nil || detail.Status != "succeeded" || detail.Attempts != 1 || detail.AffectedRows != 1 || len(detail.Runs) != 1 || detail.Runs[0].Status != "succeeded" {
		t.Fatal(detail, err)
	}
	var count int
	if err = db.SQL.QueryRowContext(ctx, `SELECT count(*) FROM admin_sessions WHERE user_id=$1`, actor.ID).Scan(&count); err != nil || count != 0 {
		t.Fatal("expired sessions retained", count, err)
	}
	if err = db.SQL.QueryRowContext(ctx, `SELECT count(*) FROM security_events WHERE target_type='job' AND target_id=$1`, job.ID).Scan(&count); err != nil || count != 1 {
		t.Fatal("queue audit missing", count, err)
	}
	page, err := svc.List(ctx, 1, 20)
	if err != nil || page.Total != 1 {
		t.Fatal(page, err)
	}
	// A crash leaves a lease. On expiry the next claim closes the old run and retries.
	stale, err := svc.Enqueue(ctx, actor.ID, jobs.SessionCleanup)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.SQL.ExecContext(ctx, `UPDATE admin_jobs SET status='running',attempts=1,lease_until=clock_timestamp()-interval '1 second' WHERE id=$1`, stale.ID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.SQL.ExecContext(ctx, `INSERT INTO admin_job_runs(job_id,attempt,status) VALUES($1,1,'running')`, stale.ID)
	if err != nil {
		t.Fatal(err)
	}
	ran, err := svc.RunOnce(ctx)
	if err != nil || !ran {
		t.Fatal(ran, err)
	}
	detail, err = svc.Detail(ctx, stale.ID)
	if err != nil || detail.Status != "succeeded" || detail.Attempts != 2 || len(detail.Runs) != 2 || detail.Runs[1].FailureCode == nil || *detail.Runs[1].FailureCode != "lease_expired" {
		t.Fatal(detail, err)
	}
	if _, err = svc.Detail(ctx, "00000000-0000-4000-8000-000000000002"); !errors.Is(err, jobs.ErrNotFound) {
		t.Fatal(err)
	}
}
