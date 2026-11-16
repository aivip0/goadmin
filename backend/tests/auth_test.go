package tests

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/joho/godotenv"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"sync"
	"testing"
	"time"

	"goadminkit/backend/internal/core/config"
	"goadminkit/backend/internal/core/database"
	"goadminkit/backend/internal/core/security"
	"goadminkit/backend/internal/modules/auth"
	"goadminkit/backend/internal/router"
	"goadminkit/backend/migrations"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func authDatabase(t *testing.T) (*database.Database, string) {
	t.Helper()
	if os.Getenv("RUN_DB_TESTS") != "1" {
		t.Skip("requires isolated PostgreSQL integration")
	}
	if err := godotenv.Load("../../.env"); err != nil && !os.IsNotExist(err) {
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
	var entropy [8]byte
	rand.Read(entropy[:])
	name := "phase24_test_" + hex.EncodeToString(entropy[:])
	if _, err = base.SQL.Exec(`CREATE SCHEMA "` + name + `"`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, err := base.SQL.Exec(`DROP SCHEMA "` + name + `" CASCADE`)
		if err != nil {
			t.Error(err)
		}
		base.SQL.Close()
	})
	u, _ := url.Parse(cfg.DatabaseURL())
	q := u.Query()
	q.Set("search_path", name)
	u.RawQuery = q.Encode()
	pool, err := sql.Open("postgres", u.String())
	if err != nil {
		t.Fatal(err)
	}
	pool.SetMaxOpenConns(12)
	t.Cleanup(func() { pool.Close() })
	orm, err := gorm.Open(postgres.New(postgres.Config{Conn: pool}), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	m, err := migrations.New(u.String())
	if err != nil {
		t.Fatal(err)
	}
	if err = m.Migrate(1); err != nil {
		t.Fatal(err)
	}
	m.Close()
	if _, err = pool.Exec("INSERT INTO app_metadata(component,schema_version) VALUES('phase22_sentinel',1)"); err != nil {
		t.Fatal(err)
	}
	m, err = migrations.New(u.String())
	if err != nil {
		t.Fatal(err)
	}
	if err = m.Migrate(2); err != nil {
		t.Fatal(err)
	}
	m.Close()
	if err = migrations.Up(u.String()); err != nil {
		t.Fatal(err)
	}
	return &database.Database{SQL: pool, ORM: orm}, u.String()
}

func TestPostgreSQLAuthentication(t *testing.T) {
	db, dsn := authDatabase(t)
	ctx := context.Background()
	repo := auth.NewRepository(db.SQL)
	s, err := auth.NewService(repo, time.Hour, 30*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	input := auth.InitialAdmin{Username: "test_admin", Email: "dedicated@example.test", Password: "a dedicated integration password", DisplayName: "Test administrator"}
	// Concurrent initialization can commit exactly one initial administrator.
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for range 2 {
		wg.Go(func() { _, e := s.Initialize(ctx, input); results <- e })
	}
	wg.Wait()
	close(results)
	success := 0
	for e := range results {
		if e == nil {
			success++
		} else if !errors.Is(e, auth.ErrConflict) {
			t.Fatal(e)
		}
	}
	if success != 1 {
		t.Fatal("initialization not serialized")
	}
	var count int
	if err = db.SQL.QueryRow("SELECT count(*) FROM user_roles").Scan(&count); err != nil || count != 1 {
		t.Fatal("initial role missing", err)
	}
	login := func() string {
		t.Helper()
		u, raw, e := s.Login(ctx, input.Username, input.Password, "127.0.0.1")
		if e != nil || u.Username != input.Username {
			t.Fatal("login", e)
		}
		return raw
	}
	first := login()
	var stored []byte
	if err = db.SQL.QueryRow("SELECT token_hash FROM admin_sessions LIMIT 1").Scan(&stored); err != nil || len(stored) != 32 || bytes.Equal(stored, []byte(first)) {
		t.Fatal("raw token persisted", err)
	}
	if _, err = s.Me(ctx, first); err != nil {
		t.Fatal(err)
	}
	for _, query := range []string{
		"UPDATE admin_sessions SET expires_at=clock_timestamp()-interval '1 second',created_at=clock_timestamp()-interval '2 hours'",
		"UPDATE admin_sessions SET expires_at=clock_timestamp()+interval '1 hour',last_seen_at=clock_timestamp()-interval '31 minutes'",
		"UPDATE admin_sessions SET last_seen_at=clock_timestamp(),revoked_at=clock_timestamp()",
		"UPDATE admin_sessions SET revoked_at=NULL; UPDATE admin_users SET status='disabled'",
		"UPDATE admin_users SET status='active',auth_version=auth_version+1",
	} {
		if _, err = db.SQL.Exec(query); err != nil {
			t.Fatal(err)
		}
		if _, err = s.Me(ctx, first); !errors.Is(err, auth.ErrUnauthorized) {
			t.Fatal("invalid session accepted", err)
		}
	}
	second := login()
	if err = s.Logout(ctx, second); err != nil {
		t.Fatal(err)
	}
	if err = s.Logout(ctx, second); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Me(ctx, second); !errors.Is(err, auth.ErrUnauthorized) {
		t.Fatal("old token reusable")
	}
	a, b := login(), login()
	allResults := make(chan error, 2)
	for range 2 {
		wg.Go(func() { allResults <- s.LogoutAll(ctx, a) })
	}
	wg.Wait()
	close(allResults)
	completed := 0
	for e := range allResults {
		if e == nil {
			completed++
		} else if !errors.Is(e, auth.ErrUnauthorized) {
			t.Fatal("concurrent logout-all", e)
		}
	}
	if completed != 1 {
		t.Fatal("concurrent logout-all committed more than once")
	}
	for _, raw := range []string{a, b} {
		if _, err = s.Me(ctx, raw); !errors.Is(err, auth.ErrUnauthorized) {
			t.Fatal("logout-all failed")
		}
	}
	// Real PostgreSQL error after INSERT must roll back the Session and last_login update.
	if _, err = db.SQL.Exec(`CREATE FUNCTION fail_login() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'test transaction failure'; END $$;
CREATE TRIGGER fail_login BEFORE UPDATE ON admin_users FOR EACH ROW EXECUTE FUNCTION fail_login()`); err != nil {
		t.Fatal(err)
	}
	var before int
	db.SQL.QueryRow("SELECT count(*) FROM admin_sessions").Scan(&before)
	if _, raw, e := s.Login(ctx, input.Username, input.Password, "127.0.0.1"); e == nil || raw != "" {
		t.Fatal("transaction failure returned session")
	}
	db.SQL.QueryRow("SELECT count(*) FROM admin_sessions").Scan(&count)
	if before != count {
		t.Fatal("partial session committed")
	}
	if _, err = db.SQL.Exec("DROP TRIGGER fail_login ON admin_users; DROP FUNCTION fail_login()"); err != nil {
		t.Fatal(err)
	}
	// Concurrent logins and single-session logouts do not deadlock.
	results = make(chan error, 8)
	tokens := make(chan string, 4)
	for range 4 {
		wg.Go(func() {
			_, raw, e := s.Login(ctx, input.Username, input.Password, "127.0.0.1")
			if e == nil {
				tokens <- raw
			}
			results <- e
		})
	}
	wg.Wait()
	close(tokens)
	for raw := range tokens {
		wg.Go(func() { results <- s.Logout(ctx, raw) })
	}
	wg.Wait()
	// Drain only login/exit results; a sufficiently large buffer avoids blocking producers.
	close(results)
	for e := range results {
		if e != nil {
			t.Fatal("concurrent auth", e)
		}
	}
	for range 2 {
		if err = migrations.Up(dsn); err != nil {
			t.Fatal(err)
		}
	}
	if err = db.Ready(ctx); err != nil {
		t.Fatal(err)
	}
	if err = db.SQL.QueryRow("SELECT count(*) FROM app_metadata WHERE component='phase22_sentinel'").Scan(&count); err != nil || count != 1 {
		t.Fatal("migration lost sentinel")
	}
	// Failed password protection survives a new service instance (all state is in PostgreSQL).
	for range 5 {
		if _, _, e := s.Login(ctx, input.Username, "incorrect password", "127.0.0.1"); !errors.Is(e, auth.ErrCredentials) {
			t.Fatal(e)
		}
	}
	s2, _ := auth.NewService(repo, time.Hour, 30*time.Minute)
	var limit *auth.Limited
	if _, _, e := s2.Login(ctx, input.Username, input.Password, "127.0.0.1"); !errors.As(e, &limit) {
		t.Fatal("backoff lost across service restart", e)
	}
	if _, err = db.SQL.Exec("UPDATE auth_login_limits SET blocked_until=clock_timestamp()-interval '1 second',request_count=0"); err != nil {
		t.Fatal(err)
	}
	login()
	if err = db.SQL.QueryRow("SELECT max(failures) FROM auth_login_limits").Scan(&count); err != nil || count != 0 {
		t.Fatal("success did not clear failures", err)
	}
	if _, err = db.SQL.Exec("UPDATE auth_login_limits SET request_count=120"); err != nil {
		t.Fatal(err)
	}
	if _, _, e := s.Login(ctx, input.Username, input.Password, "127.0.0.1"); !errors.As(e, &limit) {
		t.Fatal("request limit bypass", e)
	}
	t.Log("real PostgreSQL: 1->2->3->4, concurrent one-time init, hash-only sessions, expiry/idle/revoke/status/version, logout/all, concurrent login/logout, rollback, persistent backoff and reset, repeated migrations")
}

func TestPostgreSQLAuthenticationHTTP(t *testing.T) {
	db, _ := authDatabase(t)
	s, err := auth.NewService(auth.NewRepository(db.SQL), time.Hour, 30*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.Initialize(context.Background(), auth.InitialAdmin{Username: "http_admin", Email: "http@example.test", Password: "a dedicated HTTP test password"})
	if err != nil {
		t.Fatal(err)
	}
	opts := security.Defaults()
	opts.Origins = []string{"http://localhost:3000"}
	guard, _ := security.New(opts)
	r := router.New(db, slog.New(slog.NewTextHandler(io.Discard, nil)), time.Second, guard)
	auth.RegisterRoutes(r, s, guard)
	call := func(method, path string, body []byte, cookies []*http.Cookie, csrf string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", opts.Origins[0])
		req.Header.Set("X-CSRF-Token", csrf)
		for _, c := range cookies {
			req.AddCookie(c)
		}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}
	w := call("GET", "/api/v1/auth/csrf", nil, nil, "")
	var result struct {
		Data struct {
			Token string `json:"csrf_token"`
		}
	}
	json.Unmarshal(w.Body.Bytes(), &result)
	body := []byte(`{"username":"http_admin","password":"a dedicated HTTP test password"}`)
	if call("POST", "/api/v1/auth/login", body, nil, "").Code != 403 {
		t.Fatal("CSRF bypass")
	}
	w = call("POST", "/api/v1/auth/login", body, w.Result().Cookies(), result.Data.Token)
	if w.Code != 200 {
		t.Fatal("HTTP login", w.Code, w.Body.String())
	}
	var session *http.Cookie
	for _, c := range w.Result().Cookies() {
		if c.Name == security.SessionCookie {
			session = c
		}
	}
	if session == nil || !session.HttpOnly || bytes.Contains(w.Body.Bytes(), []byte(session.Value)) {
		t.Fatal("unsafe session response")
	}
	if call("GET", "/api/v1/auth/me", nil, []*http.Cookie{session}, "").Code != 200 {
		t.Fatal("HTTP me failed")
	}
	w = call("GET", "/api/v1/auth/csrf", nil, []*http.Cookie{session}, "")
	json.Unmarshal(w.Body.Bytes(), &result)
	cookies := append([]*http.Cookie{session}, w.Result().Cookies()...)
	if call("POST", "/api/v1/auth/logout", nil, cookies, result.Data.Token).Code != 200 {
		t.Fatal("HTTP logout failed")
	}
	if call("GET", "/api/v1/auth/me", nil, []*http.Cookie{session}, "").Code != 401 {
		t.Fatal("HTTP old token accepted")
	}
	for _, path := range []string{"/api/v1/health", "/api/v1/openapi.json"} {
		if call("GET", path, nil, nil, "").Code != 200 {
			t.Fatal("foundation regression", path)
		}
	}
}
