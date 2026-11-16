package articles

import (
	"errors"
	"regexp"
	"strings"
	"time"
)

var (
	ErrInvalid    = errors.New("invalid article input")
	ErrNotFound   = errors.New("article not found")
	ErrConflict   = errors.New("article conflict")
	ErrTransition = errors.New("invalid article transition")
	ErrForbidden  = errors.New("article permission denied")
	IDPattern     = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
	slugPattern   = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)
)

type Article struct {
	ID           string     `json:"id"`
	CategoryID   *string    `json:"category_id"`
	AuthorUserID string     `json:"author_user_id"`
	Title        string     `json:"title"`
	Slug         string     `json:"slug"`
	Summary      string     `json:"summary"`
	Body         string     `json:"body"`
	Status       string     `json:"status"`
	PublishedAt  *time.Time `json:"published_at"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

type Category struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Slug        string    `json:"slug"`
	Description string    `json:"description"`
	Status      string    `json:"status"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type ArticleInput struct {
	CategoryID *string `json:"category_id"`
	Title      string  `json:"title"`
	Slug       string  `json:"slug"`
	Summary    string  `json:"summary"`
	Body       string  `json:"body"`
}

type CategoryInput struct {
	Name        string `json:"name"`
	Slug        string `json:"slug"`
	Description string `json:"description"`
	Status      string `json:"status"`
}

type Filter struct {
	Page       int
	PageSize   int
	Query      string
	Status     string
	CategoryID string
}

type Page[T any] struct {
	Items    []T `json:"items"`
	Total    int `json:"total"`
	Page     int `json:"page"`
	PageSize int `json:"page_size"`
}

func canonicalSlug(value string, max int) (string, error) {
	value = strings.Trim(value, " \t\r\n\v\f")
	bytes := []byte(value)
	for i, b := range bytes {
		if b >= 'A' && b <= 'Z' {
			bytes[i] = b + ('a' - 'A')
		}
	}
	value = string(bytes)
	if len(value) > max || !slugPattern.MatchString(value) {
		return "", ErrInvalid
	}
	return value, nil
}

func normalizeArticle(in *ArticleInput) error {
	in.Title = strings.TrimSpace(in.Title)
	in.Summary = strings.TrimSpace(in.Summary)
	in.Body = strings.TrimSpace(in.Body)
	slug, err := canonicalSlug(in.Slug, 220)
	if err != nil || len(in.Title) == 0 || len(in.Title) > 200 || len(in.Summary) > 500 || len(in.Body) == 0 || len(in.Body) > 100000 ||
		(in.CategoryID != nil && !IDPattern.MatchString(*in.CategoryID)) {
		return ErrInvalid
	}
	in.Slug = slug
	return nil
}

func normalizeCategory(in *CategoryInput) error {
	in.Name = strings.TrimSpace(in.Name)
	in.Description = strings.TrimSpace(in.Description)
	slug, err := canonicalSlug(in.Slug, 120)
	if err != nil || len(in.Name) == 0 || len(in.Name) > 120 || len(in.Description) > 2000 {
		return ErrInvalid
	}
	if in.Status == "" {
		in.Status = "active"
	}
	if in.Status != "active" && in.Status != "disabled" {
		return ErrInvalid
	}
	in.Slug = slug
	return nil
}

func validateFilter(f Filter) error {
	if f.Page < 1 || f.Page > 1000 || f.PageSize < 1 || f.PageSize > 50 || len(f.Query) > 100 ||
		(f.Status != "" && f.Status != "draft" && f.Status != "published") ||
		(f.CategoryID != "" && !IDPattern.MatchString(f.CategoryID)) {
		return ErrInvalid
	}
	return nil
}
