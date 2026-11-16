package tests

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/joho/godotenv"
	_ "github.com/lib/pq"
	"goadminkit/backend/internal/core/config"
	"goadminkit/backend/internal/core/database"
	"goadminkit/backend/migrations"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestPostgreSQLMigrationLifecycle(t *testing.T) {
	if os.Getenv("RUN_DB_TESTS") != "1" {
		t.Skip("set RUN_DB_TESTS=1 for isolated PostgreSQL integration tests")
	}
	if err := godotenv.Load("../../.env"); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	db, err := database.Open(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer db.SQL.Close()
	token := make([]byte, 8)
	if _, err = rand.Read(token); err != nil {
		t.Fatal(err)
	}
	schema := "phase21_test_" + hex.EncodeToString(token)
	// Only the new, random test schema is removed. No existing schema or data is touched.
	if _, err = db.SQL.ExecContext(ctx, `CREATE SCHEMA "`+schema+`"`); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, e := db.SQL.ExecContext(context.Background(), `DROP SCHEMA "`+schema+`" CASCADE`); e != nil {
			t.Error(e)
		}
	}()
	dsn, err := url.Parse(cfg.DatabaseURL())
	if err != nil {
		t.Fatal(err)
	}
	q := dsn.Query()
	q.Set("search_path", schema)
	dsn.RawQuery = q.Encode()
	isolated, err := sql.Open("postgres", dsn.String())
	if err != nil {
		t.Fatal(err)
	}
	defer isolated.Close()
	orm, err := gorm.Open(postgres.New(postgres.Config{Conn: isolated}), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	target := database.Database{SQL: isolated, ORM: orm}
	if target.Ready(ctx) == nil {
		t.Fatal("unmigrated DB reported ready")
	}
	initial, err := migrations.New(dsn.String())
	if err != nil {
		t.Fatal(err)
	}
	if err = initial.Migrate(1); err != nil {
		t.Fatal(err)
	}
	initial.Close()
	if target.Ready(ctx) == nil {
		t.Fatal("version 1 incorrectly current for version 13 binary")
	}
	if _, err = isolated.ExecContext(ctx, "INSERT INTO app_metadata(component,schema_version) VALUES('upgrade_sentinel',1)"); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err = migrations.Up(dsn.String()); err != nil {
			t.Fatal(err)
		}
	}
	if err = target.Ready(ctx); err != nil {
		t.Fatal(err)
	}
	var id string
	var version int
	var count int
	var created time.Time
	if err = isolated.QueryRowContext(ctx, "SELECT id::text,schema_version,created_at FROM app_metadata WHERE component='core'").Scan(&id, &version, &created); err != nil {
		t.Fatal(err)
	}
	if len(id) != 36 || version != 13 || created.IsZero() {
		t.Fatal("invalid foundation metadata")
	}
	testAuthorizationSchema(t, isolated)
	for _, table := range []string{"admin_plugins", "admin_plugin_config", "admin_plugin_migrations", "admin_plugin_events"} {
		if err = isolated.QueryRow(`SELECT count(*) FROM information_schema.tables WHERE table_schema=current_schema() AND table_name=$1`, table).Scan(&count); err != nil || count != 1 {
			t.Fatalf("Migration 13 table %s missing: %v", table, err)
		}
	}
	if err = isolated.QueryRow(`SELECT count(*) FROM permissions WHERE code LIKE 'plugins:%'`).Scan(&count); err != nil || count != 7 {
		t.Fatal("Migration 13 permission catalog mismatch", count, err)
	}
	if err = isolated.QueryRow(`SELECT count(*) FROM menus WHERE code='system_plugins' AND component_key='plugins' AND permission_id=(SELECT id FROM permissions WHERE code='plugins:view')`).Scan(&count); err != nil || count != 1 {
		t.Fatal("Migration 13 plugin menu seed mismatch", count, err)
	}
	if err = migrations.Up(dsn.String()); err != nil {
		t.Fatal(err)
	}
	if err = isolated.QueryRow("SELECT count(*) FROM admin_users").Scan(&count); err != nil || count != 2 {
		t.Fatal("repeat migration damaged users", err)
	}
	if err = isolated.QueryRow("SELECT count(*) FROM app_metadata WHERE component='upgrade_sentinel'").Scan(&count); err != nil || count != 1 {
		t.Fatal("upgrade lost existing data", err)
	}
	for _, query := range []string{"UPDATE schema_migrations SET version=1", "UPDATE schema_migrations SET version=14", "UPDATE app_metadata SET schema_version=1 WHERE component='core'"} {
		if _, err = isolated.ExecContext(ctx, query); err != nil {
			t.Fatal(err)
		}
		if target.Ready(ctx) == nil {
			t.Fatal("version mismatch accepted")
		}
		if _, err = isolated.ExecContext(ctx, "UPDATE schema_migrations SET version=13; UPDATE app_metadata SET schema_version=13 WHERE component='core'"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = isolated.ExecContext(ctx, "INSERT INTO app_metadata(component,schema_version) VALUES ('core',1)"); err == nil {
		t.Fatal("component uniqueness not enforced")
	}
	for _, query := range []string{"UPDATE schema_migrations SET dirty=true", "DELETE FROM schema_migrations"} {
		if _, err = isolated.ExecContext(ctx, query); err != nil {
			t.Fatal(err)
		}
		if target.Ready(ctx) == nil {
			t.Fatalf("invalid migration state reported ready after %s", query)
		}
		if query == "UPDATE schema_migrations SET dirty=true" {
			if err = migrations.Up(dsn.String()); err == nil {
				t.Fatal("dirty migration unexpectedly succeeded")
			}
		}
	}
	if _, err = isolated.ExecContext(ctx, "INSERT INTO schema_migrations(version,dirty) VALUES(13,false)"); err != nil {
		t.Fatal(err)
	}
	m, err := migrations.New(dsn.String())
	if err != nil {
		t.Fatal(err)
	}
	if err = m.Migrate(9); err != nil {
		t.Fatal(err)
	}
	if err = m.Steps(-1); err != nil {
		t.Fatal(err)
	}
	a, b := m.Close()
	if a != nil || b != nil {
		t.Fatal(a, b)
	}
	if err = isolated.QueryRow(`SELECT schema_version FROM app_metadata WHERE component='core'`).Scan(&version); err != nil || version != 8 {
		t.Fatal("Down did not restore core version 8", version, err)
	}
	if err = isolated.QueryRow(`SELECT count(*) FROM information_schema.tables WHERE table_schema=current_schema() AND table_name IN ('articles','article_categories')`).Scan(&count); err != nil || count != 0 {
		t.Fatal("Down retained Articles tables", count, err)
	}
	if err = isolated.QueryRow(`SELECT count(*) FROM permissions WHERE code IN ('articles:view','articles:create','articles:update','articles:delete','articles:publish','article_categories:view','article_categories:create','article_categories:update','article_categories:delete')`).Scan(&count); err != nil || count != 0 {
		t.Fatal("Down retained Articles permissions", count, err)
	}
	if err = isolated.QueryRow(`SELECT count(*) FROM menus WHERE code IN ('demo_articles_directory','demo_articles_page','demo_article_categories_page')`).Scan(&count); err != nil || count != 0 {
		t.Fatal("Down retained Articles menus", count, err)
	}
	for _, check := range []struct{ table, name, excluded string }{
		{"menus", "menus_component_key_check", "articles"},
		{"security_events", "security_events_target_type_check", "article_category"},
	} {
		var definition string
		if err = isolated.QueryRow(`SELECT pg_get_constraintdef(c.oid) FROM pg_constraint c JOIN pg_class t ON t.oid=c.conrelid WHERE t.relnamespace=current_schema()::regnamespace AND t.relname=$1 AND c.conname=$2`, check.table, check.name).Scan(&definition); err != nil || strings.Contains(definition, check.excluded) {
			t.Fatal("Down did not restore original CHECK", check, definition, err)
		}
	}
	if target.Ready(ctx) == nil {
		t.Fatal("rolled-back DB reported ready")
	}
	if err = migrations.Up(dsn.String()); err != nil {
		t.Fatal(err)
	}
	if err = target.Ready(ctx); err != nil {
		t.Fatal(err)
	}
	t.Log("verified: real PostgreSQL 1->13 upgrade, plugin schema and seeds, preserved sentinel/users, repeat up, authorization schema, lock serialization, dirty/missing/behind/ahead rejection, isolated v9 rollback and up")
}

func testAuthorizationSchema(t *testing.T, db *sql.DB) {
	t.Helper()
	var first, second, role, permission, menu string
	for i, dest := range []*string{&first, &second} {
		if err := db.QueryRow("INSERT INTO admin_users(username,email,password_hash) VALUES($1,$2,'test-hash-not-a-password') RETURNING id", []string{"alice", "bob"}[i], []string{"a@example.test", "b@example.test"}[i]).Scan(dest); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.QueryRow("INSERT INTO roles(code,name,is_super_admin) VALUES('super_admin','Super admin',true) RETURNING id").Scan(&role); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow("INSERT INTO permissions(code,name) VALUES('users:read','Read users') RETURNING id").Scan(&permission); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow("INSERT INTO menus(code,name,permission_id) VALUES('users','Users',$1) RETURNING id", permission).Scan(&menu); err != nil {
		t.Fatal(err)
	}
	for _, item := range []struct {
		sql  string
		args []any
	}{
		{"INSERT INTO user_roles(user_id,role_id) VALUES($1,$2),($3,$2)", []any{first, role, second}},
		{"INSERT INTO role_permissions(role_id,permission_id) VALUES($1,$2)", []any{role, permission}},
		{"INSERT INTO role_menus(role_id,menu_id) VALUES($1,$2)", []any{role, menu}},
		{"INSERT INTO admin_sessions(user_id,token_hash,user_auth_version,expires_at) VALUES($1,decode(repeat('ab',32),'hex'),1,CURRENT_TIMESTAMP+interval '1 hour')", []any{first}},
	} {
		if _, err := db.Exec(item.sql, item.args...); err != nil {
			t.Fatal(err)
		}
	}
	for _, query := range []string{
		"INSERT INTO admin_users(username,email,password_hash) VALUES('ALICE','other@example.test','hash')",
		"INSERT INTO admin_users(username,email,password_hash) VALUES('other','A@example.test','hash')",
		"INSERT INTO roles(code,name,is_super_admin) VALUES('another','Another',true)",
		"INSERT INTO user_roles(user_id,role_id) SELECT user_id,role_id FROM user_roles LIMIT 1",
		"INSERT INTO role_permissions(role_id,permission_id) SELECT role_id,permission_id FROM role_permissions",
		"INSERT INTO role_menus(role_id,menu_id) SELECT role_id,menu_id FROM role_menus",
		"INSERT INTO user_roles(user_id,role_id) VALUES(gen_random_uuid(),gen_random_uuid())",
		"UPDATE admin_sessions SET token_hash=decode('ab','hex')",
		"UPDATE admin_sessions SET expires_at=created_at",
		"UPDATE admin_users SET status='unknown'",
		"UPDATE menus SET parent_id=id",
	} {
		if _, err := db.Exec(query); err == nil {
			t.Fatalf("constraint not enforced: %s", query)
		}
	}
	// The shared core row serializes all future super-admin membership/status mutations.
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err = tx.Exec("SELECT id FROM app_metadata WHERE component='core' FOR UPDATE"); err != nil {
		t.Fatal(err)
	}
	other, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer other.Rollback()
	if _, err = other.Exec("SET LOCAL lock_timeout='100ms'"); err != nil {
		t.Fatal(err)
	}
	if _, err = other.Exec("SELECT id FROM app_metadata WHERE component='core' FOR UPDATE"); err == nil {
		t.Fatal("guard did not serialize concurrent mutation")
	}
	other.Rollback()
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("SELECT id FROM app_metadata WHERE component='core' FOR UPDATE"); err != nil {
		t.Fatal("guard not released")
	}
}
