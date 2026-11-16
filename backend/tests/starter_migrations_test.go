package tests

import (
	"context"
	"database/sql"
	"goadminkit/backend/migrations"
	"testing"
)

func TestPostgreSQLStarterMigrationRollback(t *testing.T) {
	db, dsn := authDatabase(t)
	ctx := context.Background()
	var user string
	if err := db.SQL.QueryRowContext(ctx, `INSERT INTO admin_users(username,email,password_hash) VALUES('migration_actor','migration@example.test','test-only-hash') RETURNING id::text`).Scan(&user); err != nil {
		t.Fatal(err)
	}
	var job, token, notice string
	if err := db.SQL.QueryRowContext(ctx, `INSERT INTO admin_jobs(kind,requested_by) VALUES('session_cleanup',$1) RETURNING id::text`, user).Scan(&job); err != nil {
		t.Fatal(err)
	}
	m, err := migrations.New(dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if m != nil {
			_, _ = m.Close()
		}
	}()
	rejectDown := func(version int) {
		t.Helper()
		if e := m.Steps(-1); e == nil {
			t.Fatalf("version %d discarded protected data", version)
		}
		var current int
		var dirty bool
		if e := db.SQL.QueryRowContext(ctx, `SELECT schema_version FROM app_metadata WHERE component='core'`).Scan(&current); e != nil || current != version {
			t.Fatalf("version changed: %d %v", current, e)
		}
		if e := db.SQL.QueryRowContext(ctx, `SELECT dirty FROM schema_migrations`).Scan(&dirty); e != nil || !dirty {
			t.Fatalf("expected dirty after rejected down: %v %v", dirty, e)
		}
		_, _ = m.Close()
		// Only this disposable test schema may clear the dirty marker after a
		// deliberately rejected, transactionally rolled-back Down.
		if _, e := db.SQL.ExecContext(ctx, `UPDATE schema_migrations SET version=$1,dirty=false`, version); e != nil {
			t.Fatal(e)
		}
		var e error
		m, e = migrations.New(dsn)
		if e != nil {
			t.Fatal(e)
		}
	}
	if err = m.Steps(-1); err != nil { // Migration 13 has no plugin data and is safe to roll back.
		t.Fatal("clean plugin migration rollback", err)
	}
	rejectDown(12)
	if _, err = db.SQL.ExecContext(ctx, `DELETE FROM admin_jobs WHERE id=$1`, job); err != nil {
		t.Fatal(err)
	}
	if err = m.Steps(-1); err != nil {
		t.Fatal(err)
	}
	if err := db.SQL.QueryRowContext(ctx, `INSERT INTO admin_api_keys(owner_user_id,name,token_prefix,token_hash,scopes,expires_at)
 VALUES($1,'test','gak_test',decode(repeat('fa',32),'hex'),ARRAY['dashboard:view']::text[],clock_timestamp()+interval '1 day') RETURNING id::text`, user).Scan(&token); err != nil {
		t.Fatal(err)
	}
	rejectDown(11)
	if _, err = db.SQL.ExecContext(ctx, `DELETE FROM admin_api_keys WHERE id=$1`, token); err != nil {
		t.Fatal(err)
	}
	if err = m.Steps(-1); err != nil {
		t.Fatal(err)
	}
	if err := db.SQL.QueryRowContext(ctx, `INSERT INTO admin_notifications(recipient_user_id,sender_user_id,title,body) VALUES($1,$1,'test','body') RETURNING id::text`, user).Scan(&notice); err != nil {
		t.Fatal(err)
	}
	rejectDown(10)
	if _, err = db.SQL.ExecContext(ctx, `DELETE FROM admin_notifications WHERE id=$1`, notice); err != nil {
		t.Fatal(err)
	}
	if err = m.Steps(-1); err != nil {
		t.Fatal(err)
	}
	var current int
	if err = db.SQL.QueryRowContext(ctx, `SELECT schema_version FROM app_metadata WHERE component='core'`).Scan(&current); err != nil || current != 9 {
		t.Fatalf("normal down: %d %v", current, err)
	}
	for _, table := range []string{"admin_jobs", "admin_job_runs", "admin_api_keys", "admin_notifications"} {
		var exists sql.NullString
		if err = db.SQL.QueryRowContext(ctx, `SELECT to_regclass($1)::text`, table).Scan(&exists); err != nil || exists.Valid {
			t.Fatalf("table %s retained: %v %v", table, exists, err)
		}
	}
	if err = m.Migrate(13); err != nil {
		t.Fatal(err)
	}
	if err = db.Ready(ctx); err != nil {
		t.Fatal(err)
	}
}
