package notifications

import "context"

type Service struct{ repo *Repository }

func NewService(repo *Repository) *Service { return &Service{repo} }
func (s *Service) List(ctx context.Context, user string, page, size int) (Page, error) {
	if !uuid.MatchString(user) || page < 1 || page > 1000 || size < 1 || size > 50 {
		return Page{}, ErrInvalid
	}
	return s.repo.List(ctx, user, page, size)
}
func (s *Service) Create(ctx context.Context, sender string, in Input) (Item, error) {
	if !uuid.MatchString(sender) || in.validate() != nil {
		return Item{}, ErrInvalid
	}
	return s.repo.Create(ctx, sender, in)
}
func (s *Service) Read(ctx context.Context, user, id string) (Item, error) {
	if !uuid.MatchString(user) || !uuid.MatchString(id) {
		return Item{}, ErrInvalid
	}
	return s.repo.Read(ctx, user, id)
}
