package tests

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"goadminkit/backend/internal/core/security"
	"goadminkit/backend/internal/modules/auth"
	"goadminkit/backend/internal/modules/iam"
	"goadminkit/backend/internal/modules/settings"
	"goadminkit/backend/internal/router"
	"goadminkit/backend/migrations"
)

func TestPostgreSQLSettings(t *testing.T) {
	db, dsn := authDatabase(t)
	ctx := context.Background()
	identity, _ := auth.NewService(auth.NewRepository(db.SQL), time.Hour, 30*time.Minute)
	root, err := identity.Initialize(ctx, auth.InitialAdmin{Username: "settings_root", Email: "settings_root@example.test", Password: "dedicated test settings password"})
	if err != nil {
		t.Fatal(err)
	}
	service := settings.NewService(settings.NewRepository(db.SQL), nil)
	defaultValues, err := service.Get(ctx)
	if err != nil || defaultValues.Settings["localization.timezone"].Value != "UTC" {
		t.Fatal(defaultValues, err)
	}
	updates := map[string]json.RawMessage{"site.name": json.RawMessage(`"New site"`), "general.default_page_size": json.RawMessage(`50`)}
	if err = service.Update(ctx, root.ID, updates); err != nil {
		t.Fatal(err)
	}
	var count int
	if err = db.SQL.QueryRow(`SELECT count(*) FROM system_settings`).Scan(&count); err != nil || count != 2 {
		t.Fatal("settings rows", count, err)
	}
	if err = migrations.Up(dsn); err != nil {
		t.Fatal(err)
	}
	if err = db.SQL.QueryRow(`SELECT count(*) FROM system_settings`).Scan(&count); err != nil || count != 2 {
		t.Fatal("repeat migration lost settings", err)
	}
	if err = db.SQL.QueryRow(`SELECT count(*) FROM security_events WHERE target_type='setting'`).Scan(&count); err != nil || count != 2 {
		t.Fatal("missing setting audit", count, err)
	}
	viewer, err := iam.NewService(iam.NewRepository(db.SQL)).CreateUser(ctx, root.ID, iam.UserInput{Username: "settings_viewer", Email: "settings_viewer@example.test", Password: "dedicated test settings viewer password"})
	if err != nil {
		t.Fatal(err)
	}
	if err = service.Update(ctx, viewer.ID, updates); err != settings.ErrForbidden {
		t.Fatal("unauthorized write", err)
	}
	// A failure in the audit insert must roll back the complete update.
	if _, err = db.SQL.Exec(`CREATE FUNCTION reject_setting_audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.target_type='setting' THEN RAISE EXCEPTION 'audit unavailable'; END IF; RETURN NEW; END $$;
CREATE TRIGGER reject_setting_audit BEFORE INSERT ON security_events FOR EACH ROW EXECUTE FUNCTION reject_setting_audit()`); err != nil {
		t.Fatal(err)
	}
	if err = service.Update(ctx, root.ID, map[string]json.RawMessage{"site.name": json.RawMessage(`"Should rollback"`)}); err == nil {
		t.Fatal("audit failure did not roll back")
	}
	if _, err = db.SQL.Exec(`DROP TRIGGER reject_setting_audit ON security_events; DROP FUNCTION reject_setting_audit()`); err != nil {
		t.Fatal(err)
	}
	got, err := service.Get(ctx)
	if err != nil || got.Settings["site.name"].Value != "New site" {
		t.Fatal("partial setting committed", got, err)
	}
	var wg sync.WaitGroup
	errors := make(chan error, 12)
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			errors <- service.Update(ctx, root.ID, map[string]json.RawMessage{"site.description": json.RawMessage(fmt.Sprintf(`"concurrent %d"`, n))})
		}(i)
	}
	wg.Wait()
	close(errors)
	for e := range errors {
		if e != nil {
			t.Fatal("concurrent update", e)
		}
	}
	if err = db.SQL.QueryRow(`SELECT count(*) FROM system_settings WHERE key='site.description'`).Scan(&count); err != nil || count != 1 {
		t.Fatal("duplicate key", count, err)
	}
	_, token, err := identity.Login(ctx, root.Username, "dedicated test settings password", "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	_, viewerToken, err := identity.Login(ctx, viewer.Username, "dedicated test settings viewer password", "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	r := router.New(db, slog.New(slog.NewTextHandler(httptest.NewRecorder(), nil)), time.Second)
	settings.RegisterRoutes(r, service, identity, iam.NewService(iam.NewRepository(db.SQL)))
	request := func(method, rawToken, stringBody string) int {
		t.Helper()
		w := httptest.NewRecorder()
		req := httptest.NewRequest(method, "/api/v1/admin/settings", bytes.NewBufferString(stringBody))
		if rawToken != "" {
			req.AddCookie(&http.Cookie{Name: security.SessionCookie, Value: rawToken})
		}
		r.ServeHTTP(w, req)
		return w.Code
	}
	if request("GET", "", "") != 401 || request("GET", viewerToken, "") != 403 || request("PUT", viewerToken, `{"settings":{"site.name":"bad"}}`) != 403 || request("GET", token, "") != 200 {
		t.Fatal("settings API authorization failed")
	}
	if request("PUT", token, `{"settings":{"unknown":"x"}}`) != 422 || request("PUT", token, `{"settings":{"localization.language":"xx"}}`) != 422 {
		t.Fatal("invalid values accepted")
	}
	if request("PUT", token, `{"settings":{"site.name":"Saved via API"}}`) != 200 {
		t.Fatal("API update failed")
	}
	// Historical login events protect the v8 rollback; earlier settings remain intact.
	m, err := migrations.New(dsn)
	if err != nil {
		t.Fatal(err)
	}
	if err = m.Migrate(6); err == nil {
		t.Fatal("log history was discarded by rollback")
	}
	m.Close()
	if err = db.SQL.QueryRow(`SELECT count(*) FROM permissions WHERE code='settings:view'`).Scan(&count); err != nil || count != 1 {
		t.Fatal("protected settings permission changed", err)
	}
}
