package iam

import (
	"errors"
	"regexp"
	"time"
)

var (
	ErrInvalid   = errors.New("invalid input")
	ErrNotFound  = errors.New("not found")
	ErrConflict  = errors.New("conflict")
	ErrForbidden = errors.New("forbidden")
	uuidPattern  = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
	codePattern  = regexp.MustCompile(`^[a-z][a-z0-9_]{2,63}$`)
)

func validID(id string) bool { return uuidPattern.MatchString(id) }

type User struct {
	ID          string    `json:"id"`
	Username    string    `json:"username"`
	Email       string    `json:"email"`
	DisplayName string    `json:"display_name"`
	Status      string    `json:"status"`
	Roles       []Role    `json:"roles"`
	Permissions []string  `json:"permissions"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}
type Role struct {
	ID           string        `json:"id"`
	Code         string        `json:"code"`
	Name         string        `json:"name"`
	Description  string        `json:"description"`
	Status       string        `json:"status"`
	IsSystem     bool          `json:"is_system"`
	IsSuperAdmin bool          `json:"is_super_admin"`
	Permissions  []Permission  `json:"permissions"`
	Users        []UserSummary `json:"users"`
	CreatedAt    time.Time     `json:"created_at"`
	UpdatedAt    time.Time     `json:"updated_at"`
}
type UserSummary struct {
	ID       string `json:"id"`
	Username string `json:"username"`
	Status   string `json:"status"`
}
type Permission struct {
	ID          string `json:"id"`
	Code        string `json:"code"`
	Name        string `json:"name"`
	Description string `json:"description"`
}
type UserInput struct {
	Username    string `json:"username"`
	Email       string `json:"email"`
	DisplayName string `json:"display_name"`
	Password    string `json:"password"`
}
type RoleInput struct {
	Code        string `json:"code"`
	Name        string `json:"name"`
	Description string `json:"description"`
}
type Page[T any] struct {
	Items    []T `json:"items"`
	Total    int `json:"total"`
	Page     int `json:"page"`
	PageSize int `json:"page_size"`
}

var catalog = []string{
	"users:view", "users:create", "users:update", "users:delete", "users:assign_roles",
	"roles:view", "roles:create", "roles:update", "roles:delete", "roles:assign_permissions",
	"permissions:view", "permissions:assign", "dashboard:view",
	"menus:view", "menus:create", "menus:update", "menus:delete", "menus:assign_roles",
	"settings:view", "settings:update",
	"articles:view", "articles:create", "articles:update", "articles:delete", "articles:publish",
	"article_categories:view", "article_categories:create", "article_categories:update", "article_categories:delete",
	"notifications:view", "notifications:create",
	"api_keys:view", "api_keys:create", "api_keys:revoke",
	"jobs:view", "jobs:create", "jobs:retry",
	"plugins:view", "plugins:install", "plugins:enable", "plugins:disable", "plugins:uninstall", "plugins:configure", "plugins:migrate",
}
