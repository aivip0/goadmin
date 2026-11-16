package jobs

import (
	"context"
	"database/sql"
	"errors"
)

type Service struct{ repo *Repository }

func NewService(repo *Repository) *Service { return &Service{repo} }
func (s *Service) List(ctx context.Context, page, size int) (Page, error) {
	if page < 1 || page > 1000 || size < 1 || size > 50 {
		return Page{}, ErrInvalid
	}
	return s.repo.List(ctx, page, size)
}
func (s *Service) Detail(ctx context.Context, id string) (Detail, error) {
	if !uuid.MatchString(id) {
		return Detail{}, ErrInvalid
	}
	return s.repo.Detail(ctx, id)
}
func (s *Service) Enqueue(ctx context.Context, actor, kind string) (Item, error) {
	if !uuid.MatchString(actor) || kind != SessionCleanup {
		return Item{}, ErrInvalid
	}
	return s.repo.Enqueue(ctx, actor, kind, "")
}
func (s *Service) Retry(ctx context.Context, actor, id string) (Item, error) {
	if !uuid.MatchString(actor) || !uuid.MatchString(id) {
		return Item{}, ErrInvalid
	}
	return s.repo.Enqueue(ctx, actor, SessionCleanup, id)
}
func (s *Service) RunOnce(ctx context.Context) (bool, error) {
	c, err := s.repo.Claim(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if c.id == "" {
		return false, nil
	}
	if err = s.repo.Finish(ctx, c); err != nil {
		_ = s.repo.Fail(ctx, c)
		return true, err
	}
	return true, nil
}
