package tests

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"goadminkit/backend/internal/core/security"
	"goadminkit/backend/internal/modules/auth"
	"goadminkit/backend/internal/modules/dashboard"
	"goadminkit/backend/internal/modules/iam"
	"goadminkit/backend/internal/router"
)

func TestPostgreSQLDashboard(t *testing.T) {
	db, _ := authDatabase(t)
	ctx := context.Background()
	identity, err := auth.NewService(auth.NewRepository(db.SQL), time.Hour, 30*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	root, err := identity.Initialize(ctx, auth.InitialAdmin{Username: "dashboard_root", Email: "dashboard@example.test", Password: "a dedicated dashboard integration password"})
	if err != nil {
		t.Fatal(err)
	}
	service := dashboard.NewService(dashboard.NewRepository(db.SQL))
	check := func(users, active, roles, events int64) {
		t.Helper()
		got, e := service.Summary(ctx)
		if e != nil || got.Administrators != users || got.ActiveAdministrators != active || got.Roles != roles || got.SecurityEvents != events {
			t.Fatalf("summary %+v, err %v", got, e)
		}
	}
	check(1, 1, 2, 0)
	empty, err := service.Activity(ctx, 1, 10)
	if err != nil || empty.Total != 0 || empty.Items == nil || len(empty.Items) != 0 {
		t.Fatalf("empty activity %+v, err %v", empty, err)
	}
	zeroTrend, err := service.Trends(ctx, "7d", time.Now())
	if err != nil || len(zeroTrend.Points) != 7 {
		t.Fatalf("empty trend %+v, err %v", zeroTrend, err)
	}
	for _, point := range zeroTrend.Points {
		if point.Count != 0 {
			t.Fatalf("nonzero empty trend %+v", zeroTrend)
		}
	}
	if len(zeroTrend.LoginSuccesses) != 7 || len(zeroTrend.LoginFailures) != 7 {
		t.Fatal("missing empty login buckets")
	}
	utcDay := time.Now().UTC().Truncate(24 * time.Hour)
	if _, err = db.SQL.ExecContext(ctx, `INSERT INTO login_events(user_id,account_kind,outcome,created_at) VALUES
	($1,'known','success',$2),($1,'known','failure',$3),(NULL,'unknown','rate_limited',$4)`, root.ID, utcDay.Add(23*time.Hour), utcDay.Add(-time.Hour), utcDay.Add(-8*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	loginSummary, err := service.Summary(ctx)
	if err != nil || loginSummary.LoginSuccesses != 1 || loginSummary.LoginFailures != 2 {
		t.Fatalf("login totals %+v %v", loginSummary, err)
	}
	loginTrend, err := service.Trends(ctx, "7d", time.Now())
	if err != nil || loginTrend.LoginSuccesses[6].Count != 1 || loginTrend.LoginFailures[5].Count != 1 {
		t.Fatalf("UTC login buckets %+v %v", loginTrend, err)
	}
	for _, name := range []string{"first", "second"} {
		if _, err = db.SQL.ExecContext(ctx, `INSERT INTO admin_users(username,email,password_hash,status) VALUES($1,$2,'test-hash','active')`, name, name+"@example.test"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = db.SQL.ExecContext(ctx, `UPDATE admin_users SET status='disabled' WHERE username='second'`); err != nil {
		t.Fatal(err)
	}
	if _, err = db.SQL.ExecContext(ctx, `INSERT INTO security_events(actor_user_id,action,target_type,target_id,created_at) VALUES
	($1,'created','user',$1,now() - interval '3 days'),
	($1,'updated','role',$1,now() - interval '1 day'),
	($1,'deleted','menu',$1,now() - interval '31 days')`, root.ID); err != nil {
		t.Fatal(err)
	}
	check(3, 2, 2, 3)
	activity, err := service.Activity(ctx, 1, 2)
	if err != nil || activity.Total != 3 || len(activity.Items) != 2 || activity.Items[0].Action != "updated" || activity.Items[0].Actor != "dashboard_root" {
		t.Fatalf("activity %+v, err %v", activity, err)
	}
	last, err := service.Activity(ctx, 2, 2)
	if err != nil || len(last.Items) != 1 || last.Items[0].Action != "deleted" {
		t.Fatalf("pagination %+v, err %v", last, err)
	}
	for _, rangeName := range []string{"7d", "30d"} {
		v, e := service.Trends(ctx, rangeName, time.Now())
		if e != nil {
			t.Fatal(e)
		}
		want := 7
		if rangeName == "30d" {
			want = 30
		}
		var total int64
		for _, p := range v.Points {
			total += p.Count
		}
		if len(v.Points) != want || total != 2 {
			t.Fatalf("trend %+v", v)
		}
	}
	// An event at 23:30 UTC is counted in its UTC day, not the database session timezone.
	day := time.Now().UTC().Truncate(24 * time.Hour).Add(-24 * time.Hour)
	if _, err = db.SQL.ExecContext(ctx, `INSERT INTO security_events(actor_user_id,action,target_type,target_id,created_at) VALUES($1,'updated','menu',$1,$2)`, root.ID, day.Add(23*time.Hour+30*time.Minute)); err != nil {
		t.Fatal(err)
	}
	trend, err := service.Trends(ctx, "7d", time.Now())
	if err != nil || trend.Points[5].Count != 2 {
		t.Fatalf("UTC buckets %+v, err %v", trend, err)
	}
	_, token, err := identity.Login(ctx, "dashboard_root", "a dedicated dashboard integration password", "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	r := router.New(db, slog.New(slog.NewTextHandler(httptest.NewRecorder(), nil)), time.Second)
	rights := iam.NewService(iam.NewRepository(db.SQL))
	dashboard.RegisterRoutes(r, service, identity, rights, db)
	request := func(path, cookie string) (int, map[string]any) {
		t.Helper()
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		if cookie != "" {
			req.AddCookie(&http.Cookie{Name: security.SessionCookie, Value: cookie})
		}
		r.ServeHTTP(w, req)
		var result map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		return w.Code, result
	}
	for _, path := range []string{"/api/v1/admin/dashboard/summary", "/api/v1/admin/dashboard/activity?page=1&page_size=2", "/api/v1/admin/dashboard/trends?range=7d"} {
		if code, _ := request(path, ""); code != 401 {
			t.Fatalf("%s unauthenticated %d", path, code)
		}
		if code, result := request(path, token); code != 200 || result["code"] != float64(0) {
			t.Fatalf("%s status %d: %+v", path, code, result)
		}
	}
	if code, _ := request("/api/v1/admin/dashboard/trends?range=all", token); code != 400 {
		t.Fatal("unbounded trend allowed")
	}
	if code, _ := request("/api/v1/admin/dashboard/activity?page_size=100000", token); code != 400 {
		t.Fatal("unbounded page allowed")
	}
	viewer, err := rights.CreateUser(ctx, root.ID, iam.UserInput{Username: "dashboard_viewer", Email: "dashboard_viewer@example.test", Password: "a dedicated dashboard viewer password"})
	if err != nil {
		t.Fatal(err)
	}
	_, viewerToken, err := identity.Login(ctx, viewer.Username, "a dedicated dashboard viewer password", "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/api/v1/admin/dashboard/summary", "/api/v1/admin/dashboard/activity", "/api/v1/admin/dashboard/trends?range=30d"} {
		if code, _ := request(path, viewerToken); code != 403 {
			t.Fatalf("%s without permission: %d", path, code)
		}
	}
}
