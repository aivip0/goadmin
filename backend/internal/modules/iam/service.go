package iam

import (
	"context"
	"net/mail"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

	"goadminkit/backend/internal/modules/auth"
)

type Service struct{ repo *Repository }

var usernamePattern = regexp.MustCompile(`^[a-zA-Z0-9_.-]{3,64}$`)

func NewService(repo *Repository) *Service { return &Service{repo: repo} }

func (s *Service) Effective(ctx context.Context, id string) ([]string, bool, error) {
	return s.repo.Rights(ctx, id)
}
func (s *Service) Has(ctx context.Context, id, permission string) (bool, error) {
	list, super, err := s.Effective(ctx, id)
	return super || slices.Contains(list, permission), err
}
func (s *Service) HasAll(ctx context.Context, id string, permissions ...string) (bool, error) {
	list, super, err := s.Effective(ctx, id)
	if err != nil {
		return false, err
	}
	if super {
		return true, nil
	}
	for _, p := range permissions {
		if !slices.Contains(list, p) {
			return false, nil
		}
	}
	return true, nil
}
func (s *Service) HasAny(ctx context.Context, id string, permissions ...string) (bool, error) {
	list, super, err := s.Effective(ctx, id)
	if err != nil {
		return false, err
	}
	if super {
		return true, nil
	}
	for _, p := range permissions {
		if slices.Contains(list, p) {
			return true, nil
		}
	}
	return false, nil
}
func (s *Service) Users(ctx context.Context, search, status string, page, size int) (Page[User], error) {
	return s.repo.ListUsers(ctx, search, status, page, size)
}
func (s *Service) User(ctx context.Context, id string) (User, error) {
	if !validID(id) {
		return User{}, ErrInvalid
	}
	return s.repo.User(ctx, id)
}
func (s *Service) Roles(ctx context.Context) ([]Role, error) { return s.repo.ListRoles(ctx) }
func (s *Service) Role(ctx context.Context, id string) (Role, error) {
	if !validID(id) {
		return Role{}, ErrInvalid
	}
	return s.repo.Role(ctx, id)
}
func (s *Service) Permissions(ctx context.Context) ([]Permission, error) {
	return s.repo.Permissions(ctx)
}
func normalizeUser(in *UserInput, created bool) error {
	in.Username = strings.ToLower(strings.TrimSpace(in.Username))
	in.Email = strings.ToLower(strings.TrimSpace(in.Email))
	in.DisplayName = strings.TrimSpace(in.DisplayName)
	address, err := mail.ParseAddress(in.Email)
	if !usernamePattern.MatchString(in.Username) || err != nil || address.Address != in.Email || len(in.Email) > 254 || len(in.DisplayName) > 128 {
		return ErrInvalid
	}
	if created && (utf8.RuneCountInString(in.Password) < 15 || len(in.Password) > 128) {
		return ErrInvalid
	}
	return nil
}
func normalizeRole(in *RoleInput) error {
	in.Code = strings.TrimSpace(in.Code)
	in.Name = strings.TrimSpace(in.Name)
	in.Description = strings.TrimSpace(in.Description)
	if !codePattern.MatchString(in.Code) || len(in.Name) == 0 || len(in.Name) > 128 || len(in.Description) > 1000 {
		return ErrInvalid
	}
	return nil
}
func (s *Service) CreateUser(ctx context.Context, actor string, in UserInput) (User, error) {
	if err := normalizeUser(&in, true); err != nil {
		return User{}, err
	}
	hash, err := auth.HashPassword(in.Password)
	if err != nil {
		return User{}, err
	}
	id, err := s.repo.UserCreate(ctx, actor, in, hash)
	if err != nil {
		return User{}, dbError(err)
	}
	return s.repo.User(ctx, id)
}
func (s *Service) UpdateUser(ctx context.Context, actor, id string, in UserInput) (User, error) {
	if !validID(id) {
		return User{}, ErrInvalid
	}
	if err := normalizeUser(&in, false); err != nil || in.Password != "" {
		return User{}, ErrInvalid
	}
	if err := s.repo.UserUpdate(ctx, actor, id, in); err != nil {
		return User{}, dbError(err)
	}
	return s.repo.User(ctx, id)
}
func (s *Service) SetUserStatus(ctx context.Context, actor, id, status string) error {
	if !validID(id) || (status != "active" && status != "disabled") {
		return ErrInvalid
	}
	return dbError(s.repo.UserStatus(ctx, actor, id, status))
}
func (s *Service) ChangePassword(ctx context.Context, actor, id, password string) error {
	if !validID(id) || utf8.RuneCountInString(password) < 15 || len(password) > 128 {
		return ErrInvalid
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		return err
	}
	return dbError(s.repo.SetPassword(ctx, actor, id, hash))
}
func (s *Service) DeleteUser(ctx context.Context, actor, id string) error {
	if !validID(id) {
		return ErrInvalid
	}
	return dbError(s.repo.UserDelete(ctx, actor, id))
}
func validateIDs(ids []string) error {
	if ids == nil || len(ids) > 200 {
		return ErrInvalid
	}
	seen := map[string]bool{}
	for _, id := range ids {
		if !validID(id) || seen[id] {
			return ErrInvalid
		}
		seen[id] = true
	}
	return nil
}
func (s *Service) SetUserRoles(ctx context.Context, actor, id string, ids []string) error {
	if !validID(id) {
		return ErrInvalid
	}
	if err := validateIDs(ids); err != nil {
		return err
	}
	return dbError(s.repo.UserRoles(ctx, actor, id, ids))
}
func (s *Service) CreateRole(ctx context.Context, actor string, in RoleInput) (Role, error) {
	if err := normalizeRole(&in); err != nil {
		return Role{}, err
	}
	id, err := s.repo.RoleCreate(ctx, actor, in)
	if err != nil {
		return Role{}, dbError(err)
	}
	return s.repo.Role(ctx, id)
}
func (s *Service) UpdateRole(ctx context.Context, actor, id string, in RoleInput) (Role, error) {
	if !validID(id) {
		return Role{}, ErrInvalid
	}
	if err := normalizeRole(&in); err != nil {
		return Role{}, err
	}
	if err := s.repo.RoleUpdate(ctx, actor, id, in); err != nil {
		return Role{}, dbError(err)
	}
	return s.repo.Role(ctx, id)
}
func (s *Service) DeleteRole(ctx context.Context, actor, id string) error {
	if !validID(id) {
		return ErrInvalid
	}
	return dbError(s.repo.RoleDelete(ctx, actor, id))
}
func (s *Service) SetRolePermissions(ctx context.Context, actor, id string, ids []string) error {
	if !validID(id) {
		return ErrInvalid
	}
	if err := validateIDs(ids); err != nil {
		return err
	}
	return dbError(s.repo.RolePermissions(ctx, actor, id, ids))
}
