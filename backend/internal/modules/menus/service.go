package menus

import (
	"context"
	"database/sql"
	"regexp"
)

type Service struct{ repo *Repository }

func NewService(db *sql.DB) *Service { return &Service{repo: NewRepository(db)} }

var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

func validID(id string) bool { return uuidPattern.MatchString(id) }

func (s *Service) List(ctx context.Context) ([]Menu, error) { return s.repo.List(ctx) }
func (s *Service) Tree(ctx context.Context) ([]Node, error) {
	items, err := s.repo.List(ctx)
	if err != nil {
		return nil, err
	}
	return BuildTree(items), nil
}
func (s *Service) My(ctx context.Context, userID string) ([]Node, error) {
	items, err := s.repo.My(ctx, userID)
	if err != nil {
		return nil, err
	}
	return BuildTree(items), nil
}
func (s *Service) Get(ctx context.Context, id string) (Menu, error) {
	if !validID(id) {
		return Menu{}, ErrInvalid
	}
	return s.repo.Get(ctx, id)
}
func (s *Service) Create(ctx context.Context, actor string, in Input) (Menu, error) {
	if err := normalize(&in); err != nil {
		return Menu{}, err
	}
	id, err := s.repo.Create(ctx, actor, in)
	if err != nil {
		return Menu{}, err
	}
	return s.repo.Get(ctx, id)
}
func (s *Service) Update(ctx context.Context, actor, id string, in Input) (Menu, error) {
	if !validID(id) {
		return Menu{}, ErrInvalid
	}
	if err := normalize(&in); err != nil {
		return Menu{}, err
	}
	if err := s.repo.Update(ctx, actor, id, in); err != nil {
		return Menu{}, err
	}
	return s.repo.Get(ctx, id)
}
func (s *Service) Delete(ctx context.Context, actor, id string) error {
	if !validID(id) {
		return ErrInvalid
	}
	return s.repo.Delete(ctx, actor, id)
}
func (s *Service) SetRoles(ctx context.Context, actor, id string, roles []string) error {
	if !validID(id) || roles == nil || len(roles) > 500 {
		return ErrInvalid
	}
	return s.repo.SetRoles(ctx, actor, id, roles)
}
func (s *Service) Reorder(ctx context.Context, actor string, items []ReorderItem) error {
	if len(items) == 0 || len(items) > 500 {
		return ErrInvalid
	}
	return s.repo.Reorder(ctx, actor, items)
}
func (s *Service) Roles(ctx context.Context) ([]Role, error) { return s.repo.Roles(ctx) }
func (s *Service) Permissions(ctx context.Context) ([]Permission, error) {
	return s.repo.Permissions(ctx)
}
func (s *Service) Initialize(ctx context.Context, actor string) error {
	return s.repo.Initialize(ctx, actor)
}
func (s *Service) InitializeBootstrap(ctx context.Context) error {
	actor, err := s.repo.BootstrapActor(ctx)
	if err != nil {
		return err
	}
	return s.Initialize(ctx, actor)
}
