package menus

import (
	"errors"
	"net/url"
	"regexp"
	"sort"
	"strings"
)

var (
	ErrInvalid   = errors.New("invalid menu input")
	ErrNotFound  = errors.New("menu not found")
	ErrConflict  = errors.New("menu conflict")
	ErrForbidden = errors.New("forbidden")
	codePattern  = regexp.MustCompile(`^[a-z][a-z0-9_]{2,63}$`)
)

type Input struct {
	ParentID       *string `json:"parent_id"`
	Code           string  `json:"code"`
	Name           string  `json:"name"`
	Type           string  `json:"menu_type"`
	Path           string  `json:"path"`
	ComponentKey   string  `json:"component_key"`
	Icon           string  `json:"icon"`
	SortOrder      int     `json:"sort_order"`
	Visible        bool    `json:"visible"`
	Status         string  `json:"status"`
	PermissionCode string  `json:"permission_code"`
}

type Menu struct {
	ID             string   `json:"id"`
	ParentID       *string  `json:"parent_id"`
	Code           string   `json:"code"`
	Name           string   `json:"name"`
	Type           string   `json:"menu_type"`
	Path           string   `json:"path"`
	ComponentKey   string   `json:"component_key"`
	Icon           string   `json:"icon"`
	SortOrder      int      `json:"sort_order"`
	Visible        bool     `json:"visible"`
	Status         string   `json:"status"`
	PermissionCode string   `json:"permission_code"`
	IsSystem       bool     `json:"is_system"`
	RoleIDs        []string `json:"role_ids"`
}

type Node struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Type         string `json:"menu_type"`
	Path         string `json:"path,omitempty"`
	ComponentKey string `json:"component_key,omitempty"`
	Icon         string `json:"icon,omitempty"`
	External     bool   `json:"is_external"`
	Children     []Node `json:"children"`
	SortOrder    int    `json:"-"`
}

type ReorderItem struct {
	ID        string `json:"id"`
	SortOrder int    `json:"sort_order"`
}

type Role struct {
	ID   string `json:"id"`
	Code string `json:"code"`
	Name string `json:"name"`
}

type Permission struct {
	ID          string `json:"id"`
	Code        string `json:"code"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

type iconRegistry = map[string]struct{}

var icons = iconRegistry{"LayoutDashboard": {}, "Users": {}, "Shield": {}, "KeyRound": {}, "ListTree": {}, "ExternalLink": {}, "Folder": {}, "FileText": {}, "Settings": {}, "Images": {}, "ScrollText": {}, "LogIn": {}, "Bell": {}, "Puzzle": {}}
var pageRegistry = map[string]struct {
	path       string
	permission string
}{
	"dashboard":          {path: "/admin", permission: "dashboard:view"},
	"users":              {path: "/admin/users", permission: "users:view"},
	"roles":              {path: "/admin/roles", permission: "roles:view"},
	"permissions":        {path: "/admin/permissions", permission: "permissions:view"},
	"menus":              {path: "/admin/menus", permission: "menus:view"},
	"settings":           {path: "/admin/settings", permission: "settings:view"},
	"media":              {path: "/admin/media", permission: "media:view"},
	"audit_logs":         {path: "/admin/audit-logs", permission: "audit:view"},
	"login_logs":         {path: "/admin/login-logs", permission: "login_logs:view"},
	"articles":           {path: "/admin/articles", permission: "articles:view"},
	"article_categories": {path: "/admin/articles/categories", permission: "article_categories:view"},
	"notifications":      {path: "/admin/notifications", permission: "notifications:view"},
	"api_keys":           {path: "/admin/api-keys", permission: "api_keys:view"},
	"jobs":               {path: "/admin/jobs", permission: "jobs:view"},
}

func normalize(in *Input) error {
	in.Code = strings.TrimSpace(strings.ToLower(in.Code))
	in.Name = strings.TrimSpace(in.Name)
	in.Path = strings.TrimSpace(in.Path)
	in.ComponentKey = strings.TrimSpace(in.ComponentKey)
	in.Icon = strings.TrimSpace(in.Icon)
	in.PermissionCode = strings.TrimSpace(in.PermissionCode)
	if !codePattern.MatchString(in.Code) || len(in.Name) == 0 || len(in.Name) > 128 || len(in.Icon) > 64 || in.SortOrder < -1000000 || in.SortOrder > 1000000 {
		return ErrInvalid
	}
	switch in.Type {
	case "directory":
		if in.Path != "" || in.ComponentKey != "" || in.PermissionCode != "" {
			return ErrInvalid
		}
	case "page":
		page, ok := pageRegistry[in.ComponentKey]
		if !ok || page.path != in.Path || in.PermissionCode != page.permission {
			return ErrInvalid
		}
	case "external":
		u, err := url.ParseRequestURI(in.Path)
		if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || in.ComponentKey != "" || in.PermissionCode == "" {
			return ErrInvalid
		}
	default:
		return ErrInvalid
	}
	if in.Icon != "" {
		if _, ok := icons[in.Icon]; !ok {
			return ErrInvalid
		}
	}
	if in.Status == "" {
		in.Status = "active"
	}
	if in.Status != "active" && in.Status != "disabled" {
		return ErrInvalid
	}
	if in.Type != "directory" && (in.ParentID != nil && *in.ParentID == "") {
		in.ParentID = nil
	}
	return nil
}

func BuildTree(items []Menu) []Node {
	children := make(map[string][]Menu, len(items))
	known := make(map[string]bool, len(items))
	for _, item := range items {
		known[item.ID] = true
	}
	for _, item := range items {
		parent := ""
		if item.ParentID != nil && known[*item.ParentID] {
			parent = *item.ParentID
		}
		children[parent] = append(children[parent], item)
	}
	for parent := range children {
		sort.Slice(children[parent], func(i, j int) bool {
			if children[parent][i].SortOrder == children[parent][j].SortOrder {
				return children[parent][i].ID < children[parent][j].ID
			}
			return children[parent][i].SortOrder < children[parent][j].SortOrder
		})
	}
	visited := make(map[string]bool, len(items))
	var build func(string) []Node
	build = func(parent string) []Node {
		nodes := make([]Node, 0, len(children[parent]))
		for _, item := range children[parent] {
			if visited[item.ID] {
				continue
			}
			visited[item.ID] = true
			nodes = append(nodes, Node{ID: item.ID, Name: item.Name, Type: item.Type, Path: item.Path, ComponentKey: item.ComponentKey, Icon: item.Icon, External: item.Type == "external", Children: build(item.ID), SortOrder: item.SortOrder})
		}
		return nodes
	}
	return build("")
}
