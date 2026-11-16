package tests

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"goadminkit/backend/internal/core/audit"
	"goadminkit/backend/internal/core/security"
	"goadminkit/backend/internal/modules/auth"
	"goadminkit/backend/internal/modules/iam"
	"goadminkit/backend/internal/modules/logs"
	"goadminkit/backend/migrations"
)

func TestPostgreSQLLogHistoryAndPermissions(t *testing.T) {
	db, dsn := authDatabase(t)
	ctx := context.Background()
	identity, err := auth.NewService(auth.NewRepository(db.SQL), time.Hour, 30*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	root, err := identity.Initialize(ctx, auth.InitialAdmin{Username: "logs_root", Email: "logs_root@example.test", Password: "a dedicated log test password"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = identity.LoginWithContext(ctx, "logs_root", "wrong password", "127.0.0.1", "Chrome/123 SecretHeader"); err != auth.ErrCredentials {
		t.Fatal("failure not rejected", err)
	}
	if _, _, err = identity.LoginWithContext(ctx, "unknown_user", "wrong password", "127.0.0.1", "secret browser header"); err != auth.ErrCredentials {
		t.Fatal("unknown not rejected", err)
	}
	_, token, err := identity.LoginWithContext(ctx, "logs_root", "a dedicated log test password", "127.0.0.1", "Firefox/123 sensitive")
	if err != nil {
		t.Fatal(err)
	}
	var successes, failures, unknown, rawAgents int
	if err = db.SQL.QueryRow(`SELECT count(*) FILTER (WHERE outcome='success'), count(*) FILTER (WHERE outcome='failure'), count(*) FILTER (WHERE account_kind='unknown'),count(*) FILTER (WHERE user_agent NOT IN ('Chrome','Firefox','Other','Safari','Edge')) FROM login_events`).Scan(&successes, &failures, &unknown, &rawAgents); err != nil || successes != 1 || failures != 2 || unknown != 1 || rawAgents != 0 {
		t.Fatal("login event classification", successes, failures, unknown, rawAgents, err)
	}
	// A failed event insert rolls the session, last-login change and counter reset back together.
	if _, err = db.SQL.Exec(`CREATE FUNCTION reject_login_event() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'integration injection'; END $$; CREATE TRIGGER reject_login_event BEFORE INSERT ON login_events FOR EACH ROW EXECUTE FUNCTION reject_login_event()`); err != nil {
		t.Fatal(err)
	}
	var before int
	db.SQL.QueryRow(`SELECT count(*) FROM admin_sessions`).Scan(&before)
	if _, raw, e := identity.Login(ctx, "logs_root", "a dedicated log test password", "127.0.0.1"); e == nil || raw != "" {
		t.Fatal("partial success after failed event insert", e)
	}
	var after int
	db.SQL.QueryRow(`SELECT count(*) FROM admin_sessions`).Scan(&after)
	if after != before {
		t.Fatal("session committed without event")
	}
	if _, err = db.SQL.Exec(`DROP TRIGGER reject_login_event ON login_events; DROP FUNCTION reject_login_event()`); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	results := make(chan error, 4)
	for range 4 {
		wg.Go(func() {
			_, _, e := identity.Login(ctx, "logs_root", "a dedicated log test password", "127.0.0.1")
			results <- e
		})
	}
	wg.Wait()
	close(results)
	for e := range results {
		if e != nil {
			t.Fatal("concurrent login", e)
		}
	}
	if err = db.SQL.QueryRow(`SELECT count(*) FROM login_events WHERE outcome='success'`).Scan(&successes); err != nil || successes != 5 {
		t.Fatal("concurrent event loss", successes, err)
	}

	rights := iam.NewService(iam.NewRepository(db.SQL))
	if _, err = rights.CreateRole(audit.WithRequest(ctx, "POST", "/api/v1/admin/roles", "203.0.113.8", "Chrome/123 secret"), root.ID, iam.RoleInput{Code: "logs_test", Name: "Logs test"}); err != nil {
		t.Fatal(err)
	}
	var method, path, agent string
	if err = db.SQL.QueryRow(`SELECT request_method,request_path,user_agent FROM security_events WHERE action='role_created'`).Scan(&method, &path, &agent); err != nil || method != "POST" || path != "/api/v1/admin/roles" || agent != "Chrome" {
		t.Fatal("audit request context", method, path, agent, err)
	}

	viewer, err := rights.CreateUser(ctx, root.ID, iam.UserInput{Username: "logs_viewer", Email: "logs_viewer@example.test", Password: "a dedicated viewer password"})
	if err != nil {
		t.Fatal(err)
	}
	_, viewerToken, err := identity.Login(ctx, "logs_viewer", "a dedicated viewer password", "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	r := gin.New()
	logs.RegisterRoutes(r, logs.NewService(logs.NewRepository(db.SQL)), identity, rights)
	request := func(path, session string) (int, map[string]any) {
		t.Helper()
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		if session != "" {
			req.AddCookie(&http.Cookie{Name: security.SessionCookie, Value: session})
		}
		r.ServeHTTP(w, req)
		var body map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		return w.Code, body
	}
	for _, path := range []string{"/api/v1/admin/audit-logs", "/api/v1/admin/login-logs"} {
		if status, _ := request(path, ""); status != 401 {
			t.Fatal("unauthenticated", path, status)
		}
		if status, _ := request(path, viewerToken); status != 403 {
			t.Fatal("unauthorized", viewer.ID, path, status)
		}
		status, body := request(path+"?page=1&page_size=1", token)
		if status != 200 {
			t.Fatal("super list", path, status, body)
		}
		data := body["data"].(map[string]any)
		encoded, _ := json.Marshal(body)
		for _, secret := range []string{token, "a dedicated log test password", "wrong password", "unknown_user", "SecretHeader"} {
			if strings.Contains(string(encoded), secret) {
				t.Fatal("sensitive log response", secret)
			}
		}
		if data["total"].(float64) < 1 || len(data["items"].([]any)) != 1 {
			t.Fatal("pagination", data)
		}
		id := data["items"].([]any)[0].(map[string]any)["id"].(string)
		if status, _ := request(path+"/"+id, token); status != 200 {
			t.Fatal("detail", path, status)
		}
		if _, second := request(path+"?page=1&page_size=1", token); second["data"].(map[string]any)["items"].([]any)[0].(map[string]any)["id"] != id {
			t.Fatal("unstable pagination", path)
		}
	}
	for _, path := range []string{"/api/v1/admin/audit-logs?user_id=bad", "/api/v1/admin/login-logs?outcome=invalid", "/api/v1/admin/audit-logs?from=2020-01-01&to=2022-01-01", "/api/v1/admin/login-logs?page_size=1000"} {
		if status, _ := request(path, token); status != 400 {
			t.Fatal("filter accepted", path, status)
		}
	}
	if status, _ := request("/api/v1/admin/login-logs?outcome=success", token); status != 200 {
		t.Fatal("valid filter", status)
	}
	if status, body := request("/api/v1/admin/login-logs?outcome=failure&user_id="+root.ID, token); status != 200 || body["data"].(map[string]any)["total"].(float64) != 1 {
		t.Fatal("known-user outcome filter", status, body)
	}
	if err = migrations.Up(dsn); err != nil {
		t.Fatal("repeat migration", err)
	}
	m, err := migrations.New(dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	if err = m.Migrate(7); err == nil {
		t.Fatal("history rollback must be refused")
	}
}
