package menus

import "testing"

func TestNormalizeMenuKindsAndSafety(t *testing.T) {
	tests := []struct {
		name  string
		input Input
		ok    bool
	}{
		{"directory", Input{Code: "operations", Name: "Operations", Type: "directory", Visible: true}, true},
		{"registered page", Input{Code: "users_page", Name: "Users", Type: "page", Path: "/admin/users", ComponentKey: "users", PermissionCode: "users:view"}, true},
		{"wrong page route", Input{Code: "users_page", Name: "Users", Type: "page", Path: "/admin/evil", ComponentKey: "users", PermissionCode: "users:view"}, false},
		{"wrong page permission", Input{Code: "users_page", Name: "Users", Type: "page", Path: "/admin/users", ComponentKey: "users", PermissionCode: "roles:view"}, false},
		{"javascript URL", Input{Code: "bad_link", Name: "Bad", Type: "external", Path: "javascript:alert(1)", PermissionCode: "users:view"}, false},
		{"http external URL", Input{Code: "http_link", Name: "Bad", Type: "external", Path: "http://example.test", PermissionCode: "users:view"}, false},
		{"safe external URL", Input{Code: "safe_link", Name: "Docs", Type: "external", Path: "https://docs.example.test/admin", PermissionCode: "users:view"}, true},
		{"unknown icon", Input{Code: "users_page", Name: "Users", Type: "page", Path: "/admin/users", ComponentKey: "users", Icon: "Arbitrary", PermissionCode: "users:view"}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := normalize(&tc.input)
			if (err == nil) != tc.ok {
				t.Fatalf("normalize error=%v want success=%v", err, tc.ok)
			}
		})
	}
}

func TestBuildTreeSortsAndKeepsHierarchy(t *testing.T) {
	parent := "d"
	grandchildParent := "a"
	tree := BuildTree([]Menu{{ID: "b", Name: "B", Type: "page", SortOrder: 20}, {ID: "d", Name: "Directory", Type: "directory", SortOrder: 10}, {ID: "a", ParentID: &parent, Name: "A", Type: "directory", SortOrder: 10}, {ID: "c", ParentID: &parent, Name: "C", Type: "page", SortOrder: 1}, {ID: "e", ParentID: &grandchildParent, Name: "E", Type: "page", SortOrder: 1}})
	if len(tree) != 2 || tree[0].ID != "d" || tree[1].ID != "b" || len(tree[0].Children) != 2 || tree[0].Children[0].ID != "c" || len(tree[0].Children[1].Children) != 1 || tree[0].Children[1].Children[0].ID != "e" {
		t.Fatalf("unexpected ordered tree: %+v", tree)
	}
}
