package tests

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"goadminkit/backend/internal/core/security"
	"goadminkit/backend/internal/modules/articles"
	"goadminkit/backend/internal/modules/auth"
	"goadminkit/backend/internal/modules/iam"
	"goadminkit/backend/internal/router"
	"goadminkit/backend/migrations"
)

func TestPostgreSQLArticlesDemo(t *testing.T) {
	db, _ := authDatabase(t)
	ctx := context.Background()
	identity, err := auth.NewService(auth.NewRepository(db.SQL), time.Hour, 30*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	root, err := identity.Initialize(ctx, auth.InitialAdmin{Username: "article_root", Email: "article_root@example.test", Password: "isolated article test password"})
	if err != nil {
		t.Fatal(err)
	}
	rights := iam.NewService(iam.NewRepository(db.SQL))
	viewer, err := rights.CreateUser(ctx, root.ID, iam.UserInput{Username: "article_viewer", Email: "article_viewer@example.test", Password: "isolated viewer test password"})
	if err != nil {
		t.Fatal(err)
	}
	s := articles.NewService(articles.NewRepository(db.SQL))
	cat, err := s.CreateCategory(ctx, root.ID, articles.CategoryInput{Name: "News", Slug: " NEWS "})
	if err != nil || cat.Slug != "news" {
		t.Fatal(cat, err)
	}
	if _, err = s.CreateCategory(ctx, root.ID, articles.CategoryInput{Name: "Duplicate", Slug: "news"}); !errors.Is(err, articles.ErrConflict) {
		t.Fatal("duplicate category", err)
	}
	input := articles.ArticleInput{Title: "First", Slug: " First-Post ", Summary: "A", Body: "Body", CategoryID: &cat.ID}
	a, err := s.Create(ctx, root.ID, input)
	if err != nil || a.Status != "draft" || a.PublishedAt != nil || a.Slug != "first-post" {
		t.Fatal(a, err)
	}
	if _, err = s.Create(ctx, root.ID, articles.ArticleInput{Title: "Duplicate", Slug: "first-post", Body: "Body"}); !errors.Is(err, articles.ErrConflict) {
		t.Fatalf("duplicate article slug must be a conflict: %v", err)
	}
	if _, err = s.Create(ctx, viewer.ID, input); !errors.Is(err, articles.ErrForbidden) {
		t.Fatal("unauthorized creation", err)
	}
	if err = s.DeleteCategory(ctx, root.ID, cat.ID); !errors.Is(err, articles.ErrConflict) {
		t.Fatal("in-use category deleted", err)
	}
	f := articles.Filter{Page: 1, PageSize: 20, Query: "First", Status: "draft", CategoryID: cat.ID}
	list, err := s.List(ctx, f)
	if err != nil || list.Total != 1 || list.Items[0].ID != a.ID {
		t.Fatal(list, err)
	}
	pub, err := s.Transition(ctx, root.ID, a.ID, "publish")
	if err != nil || pub.Status != "published" || pub.PublishedAt == nil {
		t.Fatal(pub, err)
	}
	if _, err = s.Transition(ctx, root.ID, a.ID, "publish"); !errors.Is(err, articles.ErrTransition) {
		t.Fatal("repeated publish", err)
	}
	if err = s.Delete(ctx, root.ID, a.ID); !errors.Is(err, articles.ErrTransition) {
		t.Fatal("published delete", err)
	}
	draft, err := s.Transition(ctx, root.ID, a.ID, "unpublish")
	if err != nil || draft.PublishedAt != nil {
		t.Fatal(draft, err)
	}
	if _, err = s.Transition(ctx, root.ID, a.ID, "unpublish"); !errors.Is(err, articles.ErrTransition) {
		t.Fatal("repeated unpublish", err)
	}
	if err = s.Delete(ctx, root.ID, a.ID); err != nil {
		t.Fatal(err)
	}
	if err = s.DeleteCategory(ctx, root.ID, cat.ID); err != nil {
		t.Fatal(err)
	}
	var auditCount int
	if err = db.SQL.QueryRow(`SELECT count(*) FROM security_events WHERE target_type IN ('article','article_category')`).Scan(&auditCount); err != nil || auditCount != 6 {
		t.Fatal("audit mismatch", auditCount, err)
	}
	// The deleted rows are gone, but Down must still reject the retained audit history.
}

func TestPostgreSQLArticlesHTTPConflict(t *testing.T) {
	db, _ := authDatabase(t)
	ctx := context.Background()
	identity, err := auth.NewService(auth.NewRepository(db.SQL), time.Hour, 30*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	root, err := identity.Initialize(ctx, auth.InitialAdmin{Username: "article_http_root", Email: "article_http_root@example.test", Password: "isolated article HTTP password"})
	if err != nil {
		t.Fatal(err)
	}
	_, token, err := identity.Login(ctx, root.Username, "isolated article HTTP password", "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	r := router.New(db, slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)), time.Second)
	articles.RegisterRoutes(r, articles.NewService(articles.NewRepository(db.SQL)), identity, iam.NewService(iam.NewRepository(db.SQL)))
	request := func(slug string, category *string) int {
		t.Helper()
		categoryJSON := "null"
		if category != nil {
			categoryJSON = `"` + *category + `"`
		}
		body := []byte(`{"title":"HTTP test","slug":"` + slug + `","summary":"","body":"Text","category_id":` + categoryJSON + `}`)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/articles", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(&http.Cookie{Name: security.SessionCookie, Value: token})
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w.Code
	}
	s := articles.NewService(articles.NewRepository(db.SQL))
	category, err := s.CreateCategory(ctx, root.ID, articles.CategoryInput{Name: "HTTP category", Slug: "http-category"})
	if err != nil {
		t.Fatal(err)
	}
	if got := request("HTTP-DUPLICATE", &category.ID); got != 200 {
		t.Fatalf("create status = %d", got)
	}
	if got := request("http-duplicate", nil); got != 409 {
		t.Fatalf("duplicate status = %d, want 409", got)
	}
}

func TestPostgreSQLArticlesNinePermissions(t *testing.T) {
	db, _ := authDatabase(t)
	ctx := context.Background()
	identity, err := auth.NewService(auth.NewRepository(db.SQL), time.Hour, 30*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	root, err := identity.Initialize(ctx, auth.InitialAdmin{Username: "article_permissions_root", Email: "article_permissions_root@example.test", Password: "isolated permission test password"})
	if err != nil {
		t.Fatal(err)
	}
	rights := iam.NewService(iam.NewRepository(db.SQL))
	viewer, err := rights.CreateUser(ctx, root.ID, iam.UserInput{Username: "article_permissions_viewer", Email: "article_permissions_viewer@example.test", Password: "isolated viewer permission password"})
	if err != nil {
		t.Fatal(err)
	}
	role, err := rights.CreateRole(ctx, root.ID, iam.RoleInput{Code: "article_test_viewer", Name: "Article permission test"})
	if err != nil {
		t.Fatal(err)
	}
	if err = rights.SetUserRoles(ctx, root.ID, viewer.ID, []string{role.ID}); err != nil {
		t.Fatal(err)
	}
	_, token, err := identity.Login(ctx, viewer.Username, "isolated viewer permission password", "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := rights.Permissions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]string{}
	for _, permission := range catalog {
		ids[permission.Code] = permission.ID
	}
	r := router.New(db, slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)), time.Second)
	articles.RegisterRoutes(r, articles.NewService(articles.NewRepository(db.SQL)), identity, rights)
	const missing = "11111111-1111-4111-8111-111111111111"
	checks := []struct{ permission, method, path string }{
		{"articles:view", "GET", "/api/v1/admin/articles"},
		{"articles:create", "POST", "/api/v1/admin/articles"},
		{"articles:update", "PUT", "/api/v1/admin/articles/" + missing},
		{"articles:delete", "DELETE", "/api/v1/admin/articles/" + missing},
		{"articles:publish", "POST", "/api/v1/admin/articles/" + missing + "/publish"},
		{"article_categories:view", "GET", "/api/v1/admin/article-categories"},
		{"article_categories:create", "POST", "/api/v1/admin/article-categories"},
		{"article_categories:update", "PUT", "/api/v1/admin/article-categories/" + missing},
		{"article_categories:delete", "DELETE", "/api/v1/admin/article-categories/" + missing},
	}
	for _, granted := range checks {
		if ids[granted.permission] == "" {
			t.Fatalf("permission missing: %s", granted.permission)
		}
		if err = rights.SetRolePermissions(ctx, root.ID, role.ID, []string{ids[granted.permission]}); err != nil {
			t.Fatal(err)
		}
		for _, check := range checks {
			req := httptest.NewRequest(check.method, check.path, bytes.NewBufferString("{}"))
			req.Header.Set("Content-Type", "application/json")
			req.AddCookie(&http.Cookie{Name: security.SessionCookie, Value: token})
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			if check.permission == granted.permission && w.Code == 403 {
				t.Fatalf("granted %s denied %s", granted.permission, check.path)
			}
			if check.permission != granted.permission && w.Code != 403 {
				t.Fatalf("%s allowed %s (%d)", granted.permission, check.path, w.Code)
			}
		}
	}
}

func TestPostgreSQLArticleSearchPagination(t *testing.T) {
	db, _ := authDatabase(t)
	ctx := context.Background()
	identity, _ := auth.NewService(auth.NewRepository(db.SQL), time.Hour, 30*time.Minute)
	root, err := identity.Initialize(ctx, auth.InitialAdmin{Username: "article_pagination", Email: "article_pagination@example.test", Password: "isolated pagination password"})
	if err != nil {
		t.Fatal(err)
	}
	s := articles.NewService(articles.NewRepository(db.SQL))
	for i := 0; i < 23; i++ {
		slug := "search-" + strconv.Itoa(i)
		if _, err = s.Create(ctx, root.ID, articles.ArticleInput{Title: "Search " + strconv.Itoa(i), Slug: slug, Body: "Text"}); err != nil {
			t.Fatal(err)
		}
	}
	first, err := s.List(ctx, articles.Filter{Page: 1, PageSize: 20, Query: "search", Status: "draft"})
	if err != nil || first.Total != 23 || len(first.Items) != 20 {
		t.Fatal("first page", first.Total, len(first.Items), err)
	}
	second, err := s.List(ctx, articles.Filter{Page: 2, PageSize: 20, Query: "search", Status: "draft"})
	if err != nil || second.Total != 23 || len(second.Items) != 3 {
		t.Fatal("second page", second.Total, len(second.Items), err)
	}
	filtered, err := s.List(ctx, articles.Filter{Page: 1, PageSize: 20, Query: "search", Status: "published"})
	if err != nil || filtered.Total != 0 || len(filtered.Items) != 0 {
		t.Fatal("status filter", filtered, err)
	}
}

func TestPostgreSQLArticleConcurrentSlugAndAuditRollback(t *testing.T) {
	db, _ := authDatabase(t)
	ctx := context.Background()
	identity, _ := auth.NewService(auth.NewRepository(db.SQL), time.Hour, 30*time.Minute)
	root, err := identity.Initialize(ctx, auth.InitialAdmin{Username: "article_race", Email: "article_race@example.test", Password: "isolated concurrent password"})
	if err != nil {
		t.Fatal(err)
	}
	s := articles.NewService(articles.NewRepository(db.SQL))
	input := articles.ArticleInput{Title: "Race", Slug: "race", Body: "Some text"}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for range 2 {
		wg.Go(func() { _, e := s.Create(ctx, root.ID, input); results <- e })
	}
	wg.Wait()
	close(results)
	success, conflict := 0, 0
	for e := range results {
		if e == nil {
			success++
		} else if errors.Is(e, articles.ErrConflict) {
			conflict++
		} else {
			t.Fatal(e)
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatal("concurrent slug uniqueness", success, conflict)
	}
	var count int
	if err = db.SQL.QueryRow(`SELECT count(*) FROM security_events WHERE action='article_created'`).Scan(&count); err != nil || count != 1 {
		t.Fatal("failed create left audit", count, err)
	}
	// Inject a transactional audit failure in only the isolated schema.
	if _, err = db.SQL.Exec(`ALTER TABLE security_events ADD CONSTRAINT deny_article_audit CHECK (target_type <> 'article') NOT VALID`); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Create(ctx, root.ID, articles.ArticleInput{Title: "Blocked", Slug: "blocked", Body: "Text"}); err == nil {
		t.Fatal("audit failure accepted")
	}
	if err = db.SQL.QueryRow(`SELECT count(*) FROM articles WHERE slug='blocked'`).Scan(&count); err != nil || count != 0 {
		t.Fatal("business write survived audit rollback", count, err)
	}
}

func TestPostgreSQLArticleConcurrentUpdateAndCategoryDelete(t *testing.T) {
	db, _ := authDatabase(t)
	ctx := context.Background()
	identity, _ := auth.NewService(auth.NewRepository(db.SQL), time.Hour, 30*time.Minute)
	root, err := identity.Initialize(ctx, auth.InitialAdmin{Username: "article_update_race", Email: "article_update_race@example.test", Password: "isolated concurrent update password"})
	if err != nil {
		t.Fatal(err)
	}
	s := articles.NewService(articles.NewRepository(db.SQL))
	first, err := s.Create(ctx, root.ID, articles.ArticleInput{Title: "First", Slug: "first", Body: "Text"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.Create(ctx, root.ID, articles.ArticleInput{Title: "Second", Slug: "second", Body: "Text"})
	if err != nil {
		t.Fatal(err)
	}
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for _, id := range []string{first.ID, second.ID} {
		wg.Go(func() {
			_, e := s.Update(ctx, root.ID, id, articles.ArticleInput{Title: "Changed", Slug: "shared", Body: "Text"})
			results <- e
		})
	}
	wg.Wait()
	close(results)
	success, conflict := 0, 0
	for e := range results {
		switch {
		case e == nil:
			success++
		case errors.Is(e, articles.ErrConflict):
			conflict++
		default:
			t.Fatal(e)
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatal("concurrent update slug uniqueness", success, conflict)
	}
	category, err := s.CreateCategory(ctx, root.ID, articles.CategoryInput{Name: "Concurrent", Slug: "concurrent"})
	if err != nil {
		t.Fatal(err)
	}
	inserted := make(chan error, 1)
	deleted := make(chan error, 1)
	wg.Go(func() {
		_, e := s.Create(ctx, root.ID, articles.ArticleInput{Title: "Referenced", Slug: "referenced", Body: "Text", CategoryID: &category.ID})
		inserted <- e
	})
	wg.Go(func() { deleted <- s.DeleteCategory(ctx, root.ID, category.ID) })
	wg.Wait()
	createErr, deleteErr := <-inserted, <-deleted
	if !((createErr == nil && errors.Is(deleteErr, articles.ErrConflict)) ||
		(errors.Is(createErr, articles.ErrTransition) && deleteErr == nil)) {
		t.Fatal("unexpected category/create race", createErr, deleteErr)
	}
	var orphans int
	if err := db.SQL.QueryRow(`SELECT count(*) FROM articles a LEFT JOIN article_categories c ON c.id=a.category_id
WHERE a.category_id IS NOT NULL AND c.id IS NULL`).Scan(&orphans); err != nil || orphans != 0 {
		t.Fatal("category race created orphan", orphans, err)
	}
}

func TestPostgreSQLArticlesDownRetainsHistory(t *testing.T) {
	db, dsn := authDatabase(t)
	ctx := context.Background()
	identity, _ := auth.NewService(auth.NewRepository(db.SQL), time.Hour, 30*time.Minute)
	root, err := identity.Initialize(ctx, auth.InitialAdmin{Username: "article_down", Email: "article_down@example.test", Password: "isolated rollback password"})
	if err != nil {
		t.Fatal(err)
	}
	s := articles.NewService(articles.NewRepository(db.SQL))
	a, err := s.Create(ctx, root.ID, articles.ArticleInput{Title: "History", Slug: "history", Body: "Content"})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Delete(ctx, root.ID, a.ID); err != nil {
		t.Fatal(err)
	}
	m, err := migrations.New(dsn)
	if err != nil {
		t.Fatal(err)
	}
	if err = m.Down(); err == nil {
		t.Fatal("rollback removed article audit history")
	}
	_, _ = m.Close()
	var count int
	if err = db.SQL.QueryRow(`SELECT count(*) FROM security_events WHERE target_type='article'`).Scan(&count); err != nil || count != 2 {
		t.Fatal("audit lost", count, err)
	}
	if err = db.SQL.QueryRow(`SELECT schema_version FROM app_metadata WHERE component='core'`).Scan(&count); err != nil || count != 9 {
		t.Fatal("schema unexpectedly rolled back", count, err)
	}
}

func TestPostgreSQLArticlesDownGuards(t *testing.T) {
	// Each case owns a fresh schema in the transient integration database.
	cases := []struct {
		name, mutation, expected string
	}{
		{"article row", `INSERT INTO admin_users(username,email,password_hash) VALUES('down_author','down_author@example.test','test-hash'); INSERT INTO articles(author_user_id,title,slug,body) SELECT id,'Article','down-article','Body' FROM admin_users WHERE username='down_author'`, "articles data"},
		{"category row", `INSERT INTO article_categories(name,slug) VALUES('Category','down-category')`, "articles data"},
		{"article audit", `INSERT INTO security_events(actor_user_id,action,target_type,target_id) VALUES(gen_random_uuid(),'article_created','article',gen_random_uuid())`, "articles data"},
		{"category audit", `INSERT INTO security_events(actor_user_id,action,target_type,target_id) VALUES(gen_random_uuid(),'article_category_created','article_category',gen_random_uuid())`, "articles data"},
		{"role permission", `INSERT INTO roles(code,name) VALUES('down_role','Down role'); INSERT INTO role_permissions(role_id,permission_id) SELECT r.id,p.id FROM roles r,permissions p WHERE r.code='down_role' AND p.code='articles:view'`, "articles data"},
		{"role menu", `INSERT INTO roles(code,name) VALUES('down_role','Down role'); INSERT INTO role_menus(role_id,menu_id) SELECT r.id,m.id FROM roles r,menus m WHERE r.code='down_role' AND m.code='demo_articles_page'`, "articles data"},
		{"external permission reference", `INSERT INTO menus(code,name,permission_id) SELECT 'down_external','External',id FROM permissions WHERE code='articles:view'`, "articles data"},
		{"external child menu", `INSERT INTO menus(code,name,parent_id) SELECT 'down_child','External child',id FROM menus WHERE code='demo_articles_directory'`, "articles data"},
		{"permission name changed", `UPDATE permissions SET name='Changed' WHERE code='articles:view'`, "permission seed changed"},
		{"permission description changed", `UPDATE permissions SET description='Changed' WHERE code='article_categories:delete'`, "permission seed changed"},
		{"menu system flag changed", `UPDATE menus SET is_system=false WHERE code='demo_articles_page'`, "menu seed changed"},
		{"menu visibility changed", `UPDATE menus SET visible=false WHERE code='demo_articles_directory'`, "menu seed changed"},
		{"menu status changed", `UPDATE menus SET status='disabled' WHERE code='demo_article_categories_page'`, "menu seed changed"},
		{"menu name changed", `UPDATE menus SET name='Changed' WHERE code='demo_articles_directory'`, "menu seed changed"},
		{"menu path changed", `UPDATE menus SET path='/admin/changed' WHERE code='demo_articles_page'`, "menu seed changed"},
		{"menu icon changed", `UPDATE menus SET icon='Changed' WHERE code='demo_article_categories_page'`, "menu seed changed"},
		{"menu order changed", `UPDATE menus SET sort_order=11 WHERE code='demo_articles_page'`, "menu seed changed"},
		{"menu component changed", `UPDATE menus SET component_key='dashboard' WHERE code='demo_articles_page'`, "menu seed changed"},
		{"menu permission changed", `UPDATE menus SET permission_id=(SELECT id FROM permissions WHERE code='dashboard:view') WHERE code='demo_articles_page'`, "menu seed changed"},
		{"menu parent changed", `UPDATE menus SET parent_id=NULL WHERE code='demo_articles_page'`, "menu seed changed"},
		{"core metadata deleted", `DELETE FROM app_metadata WHERE component='core'`, "core metadata row"},
		{"core version changed", `UPDATE app_metadata SET schema_version=8 WHERE component='core'`, "schema version 9"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db, dsn := authDatabase(t)
			base, err := migrations.New(dsn)
			if err != nil {
				t.Fatal(err)
			}
			if err = base.Migrate(9); err != nil {
				t.Fatal(err)
			}
			_, _ = base.Close()
			if _, err := db.SQL.Exec(tc.mutation); err != nil {
				t.Fatal(err)
			}
			m, err := migrations.New(dsn)
			if err != nil {
				t.Fatal(err)
			}
			err = m.Steps(-1)
			_, _ = m.Close()
			if err == nil || !strings.Contains(err.Error(), tc.expected) {
				t.Fatalf("Down should reject %s with %q: %v", tc.name, tc.expected, err)
			}
			var version int
			if err := db.SQL.QueryRow(`SELECT schema_version FROM app_metadata WHERE component='core'`).Scan(&version); tc.name != "core metadata deleted" && err != nil {
				t.Fatal(err)
			} else if err == nil && tc.name != "core version changed" && version != 9 {
				t.Fatalf("Down changed core version to %d", version)
			}
		})
	}
}
