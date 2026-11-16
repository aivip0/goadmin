package tests

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"goadminkit/backend/internal/core/security"
	"goadminkit/backend/internal/modules/auth"
	"goadminkit/backend/internal/modules/iam"
	"goadminkit/backend/internal/modules/menus"
	"goadminkit/backend/internal/router"
)

func TestPostgreSQLRBAC(t *testing.T) {
	db, dsn := authDatabase(t)
	ctx := context.Background()
	repo := iam.NewRepository(db.SQL)
	service := iam.NewService(repo)
	authService, err := auth.NewService(auth.NewRepository(db.SQL), time.Hour, 30*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	input := auth.InitialAdmin{Username: "rbac_root", Email: "rbac_root@example.test", Password: "a special RBAC test password"}
	root, err := authService.Initialize(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = service.Effective(ctx, root.ID); err != nil {
		t.Fatalf("effective super permissions: %v", err)
	}
	initial, err := service.Permissions(ctx)
	if err != nil || len(initial) != 49 {
		t.Fatal("catalog", len(initial), err)
	}
	if _, err = db.SQL.Exec("INSERT INTO permissions(code,name) VALUES('users:view','duplicate') ON CONFLICT(code) DO NOTHING"); err != nil {
		t.Fatal(err)
	}
	if err = service.SetUserStatus(ctx, root.ID, root.ID, "disabled"); !errors.Is(err, iam.ErrConflict) {
		t.Fatal("disabled last super", err)
	}
	if err = service.DeleteUser(ctx, root.ID, root.ID); !errors.Is(err, iam.ErrForbidden) {
		t.Fatal("deleted self", err)
	}
	var superRole string
	if err = db.SQL.QueryRow("SELECT id FROM roles WHERE is_super_admin").Scan(&superRole); err != nil {
		t.Fatal(err)
	}
	if err = service.SetUserRoles(ctx, root.ID, root.ID, []string{}); !errors.Is(err, iam.ErrConflict) {
		t.Fatal("demoted last super", err)
	}
	if err = service.SetRolePermissions(ctx, root.ID, superRole, []string{}); !errors.Is(err, iam.ErrForbidden) {
		t.Fatal("mutable super role", err)
	}
	viewer, err := service.CreateUser(ctx, root.ID, iam.UserInput{Username: "viewer", Email: "viewer@example.test", Password: "an isolated viewer password"})
	if err != nil {
		t.Fatal(err)
	}
	role, err := service.CreateRole(ctx, root.ID, iam.RoleInput{Code: "limited_viewer", Name: "Limited viewer"})
	if err != nil {
		t.Fatal(err)
	}
	var viewID string
	for _, p := range initial {
		if p.Code == "users:view" {
			viewID = p.ID
		}
	}
	if err = service.SetRolePermissions(ctx, root.ID, role.ID, []string{viewID}); err != nil {
		t.Fatal(err)
	}
	if err = service.SetUserRoles(ctx, root.ID, viewer.ID, []string{role.ID}); err != nil {
		t.Fatal(err)
	}
	userDetail, err := service.User(ctx, viewer.ID)
	if err != nil || len(userDetail.Roles) != 1 || len(userDetail.Permissions) != 1 {
		t.Fatal("user associations", err, userDetail)
	}
	roleDetail, err := service.Role(ctx, role.ID)
	if err != nil || len(roleDetail.Users) != 1 || len(roleDetail.Permissions) != 1 {
		t.Fatal("role associations", err)
	}
	allowed, err := service.HasAll(ctx, viewer.ID, "users:view")
	if err != nil || !allowed {
		t.Fatal("missing grant", err)
	}
	allowed, err = service.HasAny(ctx, viewer.ID, "roles:delete", "users:view")
	if err != nil || !allowed {
		t.Fatal("any grant", err)
	}
	if err = service.SetUserRoles(ctx, viewer.ID, viewer.ID, []string{role.ID, superRole}); !errors.Is(err, iam.ErrForbidden) {
		t.Fatal("self elevation", err)
	}
	if err = service.SetRolePermissions(ctx, viewer.ID, role.ID, []string{viewID}); !errors.Is(err, iam.ErrForbidden) {
		t.Fatal("role elevation", err)
	}
	if err = service.SetUserStatus(ctx, viewer.ID, root.ID, "disabled"); !errors.Is(err, iam.ErrForbidden) {
		t.Fatal("ordinary edited super", err)
	}
	if err = service.SetRolePermissions(ctx, root.ID, role.ID, []string{}); err != nil {
		t.Fatal(err)
	}
	allowed, err = service.Has(ctx, viewer.ID, "users:view")
	if err != nil || allowed {
		t.Fatal("revocation not immediate", err)
	}
	// A rejected grant must not partially replace a role's current permissions.
	if err = service.SetRolePermissions(ctx, root.ID, role.ID, []string{viewID, "ffffffff-ffff-ffff-ffff-ffffffffffff"}); !errors.Is(err, iam.ErrInvalid) {
		t.Fatal("invalid grant", err)
	}
	roleDetail, err = service.Role(ctx, role.ID)
	if err != nil || len(roleDetail.Permissions) != 0 {
		t.Fatal("partial grant", err)
	}
	if err = service.SetRolePermissions(ctx, root.ID, role.ID, []string{viewID}); err != nil {
		t.Fatal(err)
	}
	if err = service.DeleteRole(ctx, root.ID, role.ID); !errors.Is(err, iam.ErrConflict) {
		t.Fatal("deleted used role", err)
	}
	if err = service.SetUserRoles(ctx, root.ID, viewer.ID, []string{}); err != nil {
		t.Fatal(err)
	}
	if err = service.DeleteRole(ctx, root.ID, role.ID); err != nil {
		t.Fatal(err)
	}
	if err = service.SetUserRoles(ctx, root.ID, viewer.ID, []string{superRole}); err != nil {
		t.Fatal(err)
	}
	// Serialize three different mutation entry points against the same core row.
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	wg.Go(func() { errs <- service.SetUserStatus(ctx, root.ID, viewer.ID, "disabled") })
	wg.Go(func() { errs <- service.SetUserRoles(ctx, root.ID, root.ID, []string{}) })
	wg.Wait()
	close(errs)
	failed := 0
	for e := range errs {
		if errors.Is(e, iam.ErrConflict) || errors.Is(e, iam.ErrForbidden) {
			failed++
		} else if e != nil {
			t.Fatal(e)
		}
	}
	if failed != 1 {
		t.Fatal("two simultaneous super demotions were accepted", failed)
	}
	var active int
	err = db.SQL.QueryRow(`SELECT count(DISTINCT u.id) FROM admin_users u JOIN user_roles ur ON ur.user_id=u.id JOIN roles r ON r.id=ur.role_id WHERE u.status='active' AND r.status='active' AND r.is_super_admin`).Scan(&active)
	if err != nil || active != 1 {
		t.Fatal("last super invariant", active, err)
	}
	var events int
	err = db.SQL.QueryRow("SELECT count(*) FROM security_events WHERE actor_user_id=$1", root.ID).Scan(&events)
	if err != nil || events < 7 {
		t.Fatal("missing security events", events, err)
	}
	_ = dsn
}

func TestPostgreSQLConcurrentSuperDeleteDisable(t *testing.T) {
	db, _ := authDatabase(t)
	ctx := context.Background()
	identity, _ := auth.NewService(auth.NewRepository(db.SQL), time.Hour, 30*time.Minute)
	root, err := identity.Initialize(ctx, auth.InitialAdmin{Username: "root_guard", Email: "root_guard@example.test", Password: "an isolated guard test password"})
	if err != nil {
		t.Fatal(err)
	}
	service := iam.NewService(iam.NewRepository(db.SQL))
	other, err := service.CreateUser(ctx, root.ID, iam.UserInput{Username: "other_guard", Email: "other_guard@example.test", Password: "an isolated second password"})
	if err != nil {
		t.Fatal(err)
	}
	var role string
	if err = db.SQL.QueryRow("SELECT id FROM roles WHERE is_super_admin").Scan(&role); err != nil {
		t.Fatal(err)
	}
	if err = service.SetUserRoles(ctx, root.ID, other.ID, []string{role}); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	wg.Go(func() { errs <- service.DeleteUser(ctx, root.ID, other.ID) })
	wg.Go(func() { errs <- service.SetUserStatus(ctx, root.ID, root.ID, "disabled") })
	wg.Wait()
	close(errs)
	failed := 0
	for e := range errs {
		if errors.Is(e, iam.ErrConflict) || errors.Is(e, iam.ErrForbidden) {
			failed++
		} else if e != nil {
			t.Fatal(e)
		}
	}
	if failed != 1 {
		t.Fatal("concurrent delete/disable accepted", failed)
	}
	var active int
	if err = db.SQL.QueryRow(`SELECT count(DISTINCT u.id) FROM admin_users u JOIN user_roles ur ON ur.user_id=u.id JOIN roles r ON r.id=ur.role_id WHERE u.status='active' AND r.is_super_admin`).Scan(&active); err != nil || active != 1 {
		t.Fatal("last super lost", active, err)
	}
}

func TestPostgreSQLMenus(t *testing.T) {
	db, _ := authDatabase(t)
	ctx := context.Background()
	identity, err := auth.NewService(auth.NewRepository(db.SQL), time.Hour, 30*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	root, err := identity.Initialize(ctx, auth.InitialAdmin{Username: "menu_root", Email: "menu_root@example.test", Password: "dedicated isolated menu password"})
	if err != nil {
		t.Fatal(err)
	}
	service := menus.NewService(db.SQL)
	if err = service.Initialize(ctx, root.ID); err != nil {
		t.Fatal(err)
	}
	if err = service.Initialize(ctx, root.ID); err != nil {
		t.Fatal("idempotent initialize", err)
	}
	var systemCount int
	if err = db.SQL.QueryRow(`SELECT count(*) FROM menus WHERE is_system`).Scan(&systemCount); err != nil || systemCount != 16 {
		t.Fatal("system menus", systemCount, err)
	}
	permissions, err := iam.NewRepository(db.SQL).Permissions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	permissionIDs := map[string]string{}
	for _, p := range permissions {
		permissionIDs[p.Code] = p.ID
	}
	viewer, err := iam.NewService(iam.NewRepository(db.SQL)).CreateUser(ctx, root.ID, iam.UserInput{Username: "menu_viewer", Email: "menu_viewer@example.test", Password: "dedicated viewer password"})
	if err != nil {
		t.Fatal(err)
	}
	role, err := iam.NewService(iam.NewRepository(db.SQL)).CreateRole(ctx, root.ID, iam.RoleInput{Code: "menu_viewer", Name: "Menu Viewer"})
	if err != nil {
		t.Fatal(err)
	}
	if err = iam.NewService(iam.NewRepository(db.SQL)).SetRolePermissions(ctx, root.ID, role.ID, []string{permissionIDs["users:view"]}); err != nil {
		t.Fatal(err)
	}
	if err = iam.NewService(iam.NewRepository(db.SQL)).SetUserRoles(ctx, root.ID, viewer.ID, []string{role.ID}); err != nil {
		t.Fatal(err)
	}
	directory, err := service.Create(ctx, root.ID, menus.Input{Code: "reports", Name: "Reports", Type: "directory", Visible: true, Status: "active", SortOrder: 100})
	if err != nil {
		t.Fatal(err)
	}
	parent := directory.ID
	page, err := service.Create(ctx, root.ID, menus.Input{ParentID: &parent, Code: "user_report", Name: "User report", Type: "page", Path: "/admin/users", ComponentKey: "users", PermissionCode: "users:view", Visible: true, Status: "active", SortOrder: 5})
	if err != nil {
		t.Fatal(err)
	}
	var reorder sync.WaitGroup
	results := make(chan error, 2)
	for _, order := range []int{15, 25} {
		reorder.Add(1)
		go func(sortOrder int) {
			defer reorder.Done()
			results <- service.Reorder(ctx, root.ID, []menus.ReorderItem{{ID: page.ID, SortOrder: sortOrder}})
		}(order)
	}
	reorder.Wait()
	close(results)
	for result := range results {
		if result != nil {
			t.Fatal("concurrent menu reorder", result)
		}
	}
	if err = service.SetRoles(ctx, root.ID, page.ID, []string{role.ID}); err != nil {
		t.Fatal(err)
	}
	tree, err := service.My(ctx, viewer.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(tree) != 1 || tree[0].ID != directory.ID || len(tree[0].Children) != 1 {
		t.Fatal("authorized user tree", tree)
	}
	if _, err = service.Create(ctx, root.ID, menus.Input{ParentID: func() *string { v := page.ID; return &v }(), Code: "cycle_node", Name: "Cycle", Type: "directory", Visible: true}); err == nil {
		t.Fatal("page accepted as directory parent")
	}
	if _, err = service.Update(ctx, root.ID, directory.ID, menus.Input{ParentID: func() *string { v := page.ID; return &v }(), Code: "reports", Name: "Reports", Type: "directory", Visible: true, Status: "active"}); !errors.Is(err, menus.ErrInvalid) {
		t.Fatal("cycle rejected", err)
	}
	if err = service.SetRoles(ctx, root.ID, page.ID, []string{}); err != nil {
		t.Fatal(err)
	}
	tree, err = service.My(ctx, viewer.ID)
	if err != nil || len(tree) != 0 {
		t.Fatal("revoked role menu still visible", tree, err)
	}
	if err = service.SetRoles(ctx, root.ID, page.ID, []string{role.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err = service.Update(ctx, root.ID, page.ID, menus.Input{ParentID: &parent, Code: "user_report", Name: "User report", Type: "page", Path: "/admin/users", ComponentKey: "users", PermissionCode: "roles:view", Visible: true, Status: "active"}); !errors.Is(err, menus.ErrInvalid) {
		t.Fatal("page permission mismatch accepted", err)
	}
	if err = iam.NewService(iam.NewRepository(db.SQL)).SetRolePermissions(ctx, root.ID, role.ID, []string{}); err != nil {
		t.Fatal(err)
	}
	tree, err = service.My(ctx, viewer.ID)
	if err != nil || len(tree) != 0 {
		t.Fatal("permission filter", tree, err)
	}
	if err = iam.NewService(iam.NewRepository(db.SQL)).SetRolePermissions(ctx, root.ID, role.ID, []string{permissionIDs["users:view"]}); err != nil {
		t.Fatal(err)
	}
	if _, err = service.Update(ctx, root.ID, page.ID, menus.Input{ParentID: &parent, Code: "user_report", Name: "User report", Type: "page", Path: "/admin/users", ComponentKey: "users", PermissionCode: "users:view", Visible: true, Status: "disabled"}); err != nil {
		t.Fatal(err)
	}
	tree, err = service.My(ctx, viewer.ID)
	if err != nil || len(tree) != 0 {
		t.Fatal("disabled menu visible", tree, err)
	}
	if err = service.Delete(ctx, root.ID, directory.ID); !errors.Is(err, menus.ErrConflict) {
		t.Fatal("directory with child deleted", err)
	}
	if err = service.Delete(ctx, root.ID, page.ID); err != nil {
		t.Fatal(err)
	}
	var links int
	if err = db.SQL.QueryRow(`SELECT count(*) FROM role_menus WHERE menu_id=$1`, page.ID).Scan(&links); err != nil || links != 0 {
		t.Fatal("role association cascade", links, err)
	}
	if err = service.Delete(ctx, root.ID, directory.ID); err != nil {
		t.Fatal(err)
	}
	var events int
	if err = db.SQL.QueryRow(`SELECT count(*) FROM security_events WHERE target_type='menu'`).Scan(&events); err != nil || events < 6 {
		t.Fatal("menu audit", events, err)
	}
	var seededRoleMenus int
	if err = db.SQL.QueryRow(`SELECT count(*) FROM role_menus WHERE menu_id IN (SELECT id FROM menus WHERE is_system)`).Scan(&seededRoleMenus); err != nil || seededRoleMenus != 0 {
		t.Fatal("initializer assigned unexpected role menus", seededRoleMenus, err)
	}
}

func TestPostgreSQLRBACHTTP(t *testing.T) {
	db, _ := authDatabase(t)
	ctx := context.Background()
	identity, _ := auth.NewService(auth.NewRepository(db.SQL), time.Hour, 30*time.Minute)
	initial := auth.InitialAdmin{Username: "http_root", Email: "http_root@example.test", Password: "a dedicated HTTP RBAC password"}
	root, err := identity.Initialize(ctx, initial)
	if err != nil {
		t.Fatal(err)
	}
	service := iam.NewService(iam.NewRepository(db.SQL))
	viewer, err := service.CreateUser(ctx, root.ID, iam.UserInput{Username: "http_viewer", Email: "http_viewer@example.test", Password: "a dedicated viewer password"})
	if err != nil {
		t.Fatal(err)
	}
	_, raw, err := identity.Login(ctx, initial.Username, initial.Password, "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	var viewerRaw string
	_, viewerRaw, err = identity.Login(ctx, "http_viewer", "a dedicated viewer password", "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	opts := security.Defaults()
	opts.Origins = []string{"http://localhost:3000"}
	guard, _ := security.New(opts)
	r := router.New(db, slog.New(slog.NewTextHandler(io.Discard, nil)), time.Second, guard)
	iam.RegisterRoutes(r, service, identity)
	call := func(method, path, token, csrf string, body []byte) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, bytes.NewReader(body))
		req.Header.Set("Origin", opts.Origins[0])
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-CSRF-Token", csrf)
		if token != "" {
			req.AddCookie(&http.Cookie{Name: security.SessionCookie, Value: token})
		}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}
	if got := call("GET", "/api/v1/admin/users", "", "", nil).Code; got != 401 {
		t.Fatal("no session", got)
	}
	if got := call("GET", "/api/v1/admin/users", viewerRaw, "", nil).Code; got != 403 {
		t.Fatal("unauthorized read", got)
	}
	if got := call("GET", "/api/v1/admin/users", raw, "", nil).Code; got != 200 {
		t.Fatal("super read", got)
	}
	csrfResult := call("GET", "/api/v1/auth/csrf", raw, "", nil)
	var envelope struct {
		Data struct {
			Token string `json:"csrf_token"`
		}
	}
	if err = json.Unmarshal(csrfResult.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if got := call("POST", "/api/v1/admin/roles", raw, "", []byte(`{}`)).Code; got != 403 {
		t.Fatal("missing CSRF", got)
	}
	if got := call("GET", "/api/v1/admin/permissions/effective", viewerRaw, "", nil).Code; got != 200 {
		t.Fatal("effective self", got)
	}
	if err = service.SetUserStatus(ctx, root.ID, viewer.ID, "disabled"); err != nil {
		t.Fatal(err)
	}
	if got := call("GET", "/api/v1/admin/permissions/effective", viewerRaw, "", nil).Code; got != 401 {
		t.Fatal("disabled session", got)
	}
}
