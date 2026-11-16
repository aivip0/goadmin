package iam

import "testing"

func TestPermissionCodeCatalogStable(t *testing.T) {
	want := []string{"users:view", "users:create", "users:update", "users:delete", "users:assign_roles", "roles:view", "roles:create", "roles:update", "roles:delete", "roles:assign_permissions", "permissions:view", "permissions:assign", "dashboard:view", "menus:view", "menus:create", "menus:update", "menus:delete", "menus:assign_roles", "settings:view", "settings:update", "articles:view", "articles:create", "articles:update", "articles:delete", "articles:publish", "article_categories:view", "article_categories:create", "article_categories:update", "article_categories:delete", "notifications:view", "notifications:create", "api_keys:view", "api_keys:create", "api_keys:revoke", "jobs:view", "jobs:create", "jobs:retry", "plugins:view", "plugins:install", "plugins:enable", "plugins:disable", "plugins:uninstall", "plugins:configure", "plugins:migrate"}
	if len(catalog) != len(want) {
		t.Fatalf("catalog size: got %d want %d", len(catalog), len(want))
	}
	for i, code := range want {
		if catalog[i] != code {
			t.Fatalf("catalog[%d]: got %q want %q", i, catalog[i], code)
		}
	}
}

func TestInputValidation(t *testing.T) {
	for _, tc := range []struct {
		name    string
		input   UserInput
		created bool
		ok      bool
	}{
		{"valid create", UserInput{Username: "admin.user", Email: "admin@example.test", Password: "long enough password"}, true, true},
		{"short create password", UserInput{Username: "admin", Email: "admin@example.test", Password: "short"}, true, false},
		{"invalid username", UserInput{Username: "admin name", Email: "admin@example.test", Password: "long enough password"}, true, false},
		{"invalid email", UserInput{Username: "admin", Email: "not-an-email"}, false, false},
		{"valid profile", UserInput{Username: "admin", Email: "admin@example.test"}, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := normalizeUser(&tc.input, tc.created)
			if (err == nil) != tc.ok {
				t.Fatalf("normalizeUser error=%v, want ok=%v", err, tc.ok)
			}
		})
	}
	for _, tc := range []struct {
		name  string
		input RoleInput
		ok    bool
	}{
		{"valid", RoleInput{Code: "content_editor", Name: "Content editor"}, true},
		{"invalid code", RoleInput{Code: "Editor", Name: "Editor"}, false},
		{"empty name", RoleInput{Code: "editor", Name: " "}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := normalizeRole(&tc.input)
			if (err == nil) != tc.ok {
				t.Fatalf("normalizeRole error=%v, want ok=%v", err, tc.ok)
			}
		})
	}
}
