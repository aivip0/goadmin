package settings

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/joho/godotenv"
	_ "github.com/lib/pq"
	"goadminkit/backend/internal/core/config"
	"goadminkit/backend/internal/core/database"
	"goadminkit/backend/internal/modules/auth"
	"goadminkit/backend/migrations"
)

func TestPostgreSQLSecretStorage(t *testing.T) {
	if os.Getenv("RUN_DB_TESTS") != "1" {
		t.Skip("requires isolated PostgreSQL integration")
	}
	if err := godotenv.Load("../../../../.env"); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	base, err := database.Open(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer base.SQL.Close()
	var entropy [8]byte
	if _, err = rand.Read(entropy[:]); err != nil {
		t.Fatal(err)
	}
	schema := "phase32_secret_test_" + hex.EncodeToString(entropy[:])
	ctx := context.Background()
	if _, err = base.SQL.ExecContext(ctx, `CREATE SCHEMA "`+schema+`"`); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, e := base.SQL.ExecContext(ctx, `DROP SCHEMA "`+schema+`" CASCADE`); e != nil {
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
	if err = migrations.Up(dsn.String()); err != nil {
		t.Fatal(err)
	}
	pool, err := sql.Open("postgres", dsn.String())
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	identity, err := auth.NewService(auth.NewRepository(pool), time.Hour, 30*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	actor, err := identity.Initialize(ctx, auth.InitialAdmin{Username: "secret_test_admin", Email: "secret_test@example.test", Password: "dedicated isolated secret test password"})
	if err != nil {
		t.Fatal(err)
	}
	// A test-only secret key exercises the reserved storage branch without exposing
	// a production secret setting or adding an API key to the public catalog.
	registry["test.secret"] = definition{defaultValue: "", validate: text(128, true), sensitive: true}
	defer delete(registry, "test.secret")
	value := `"isolated encrypted fixture"`
	change := map[string]json.RawMessage{"test.secret": json.RawMessage(value)}
	if err = NewService(NewRepository(pool), nil).Update(ctx, actor.ID, change); err != ErrKeyUnavailable {
		t.Fatal("missing deployment key allowed", err)
	}
	key := bytes.Repeat([]byte{42}, 32)
	service := NewService(NewRepository(pool), key)
	if err = service.Update(ctx, actor.ID, change); err != nil {
		t.Fatal(err)
	}
	var public []byte
	var ciphertext []byte
	var version int
	var sensitive bool
	if err = pool.QueryRowContext(ctx, `SELECT value_json,value_ciphertext,encryption_key_version,is_sensitive FROM system_settings WHERE key='test.secret'`).Scan(&public, &ciphertext, &version, &sensitive); err != nil {
		t.Fatal(err)
	}
	if public != nil || !sensitive || version != 1 || len(ciphertext) < 32 || bytes.Contains(ciphertext, []byte("isolated encrypted fixture")) {
		t.Fatal("secret plaintext or invalid ciphertext persisted")
	}
	read, err := service.Get(ctx)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(read)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(encoded, []byte("isolated encrypted fixture")) || !read.Settings["test.secret"].Configured || read.Settings["test.secret"].Value != nil {
		t.Fatal("secret exposed in GET")
	}
	if _, err = NewService(NewRepository(pool), nil).Get(ctx); err != ErrKeyUnavailable {
		t.Fatal("secret read without key accepted", err)
	}
}
