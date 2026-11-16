package articles

import "context"

type Service struct{ repo *Repository }

func NewService(repo *Repository) *Service { return &Service{repo: repo} }

func (s *Service) List(ctx context.Context, f Filter) (Page[Article], error) {
	if err := validateFilter(f); err != nil {
		return Page[Article]{}, err
	}
	return s.repo.List(ctx, f)
}
func (s *Service) Categories(ctx context.Context, page, size int) (Page[Category], error) {
	if err := validateFilter(Filter{Page: page, PageSize: size}); err != nil {
		return Page[Category]{}, err
	}
	return s.repo.Categories(ctx, page, size)
}
func (s *Service) Get(ctx context.Context, id string) (Article, error) {
	if !IDPattern.MatchString(id) {
		return Article{}, ErrInvalid
	}
	return s.repo.Get(ctx, id)
}
func (s *Service) Create(ctx context.Context, actor string, in ArticleInput) (Article, error) {
	if err := normalizeArticle(&in); err != nil {
		return Article{}, err
	}
	return s.repo.Create(ctx, actor, in)
}
func (s *Service) Update(ctx context.Context, actor, id string, in ArticleInput) (Article, error) {
	if !IDPattern.MatchString(id) {
		return Article{}, ErrInvalid
	}
	if err := normalizeArticle(&in); err != nil {
		return Article{}, err
	}
	return s.repo.Update(ctx, actor, id, in)
}
func (s *Service) Transition(ctx context.Context, actor, id, action string) (Article, error) {
	if !IDPattern.MatchString(id) || (action != "publish" && action != "unpublish") {
		return Article{}, ErrInvalid
	}
	return s.repo.Transition(ctx, actor, id, action)
}
func (s *Service) Delete(ctx context.Context, actor, id string) error {
	if !IDPattern.MatchString(id) {
		return ErrInvalid
	}
	return s.repo.Delete(ctx, actor, id)
}
func (s *Service) CreateCategory(ctx context.Context, actor string, in CategoryInput) (Category, error) {
	if err := normalizeCategory(&in); err != nil {
		return Category{}, err
	}
	return s.repo.CreateCategory(ctx, actor, in)
}
func (s *Service) UpdateCategory(ctx context.Context, actor, id string, in CategoryInput) (Category, error) {
	if !IDPattern.MatchString(id) {
		return Category{}, ErrInvalid
	}
	if err := normalizeCategory(&in); err != nil {
		return Category{}, err
	}
	return s.repo.UpdateCategory(ctx, actor, id, in)
}
func (s *Service) DeleteCategory(ctx context.Context, actor, id string) error {
	if !IDPattern.MatchString(id) {
		return ErrInvalid
	}
	return s.repo.DeleteCategory(ctx, actor, id)
}
