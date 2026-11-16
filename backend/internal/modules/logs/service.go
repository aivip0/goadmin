package logs

import (
	"context"
	"time"
)

type Service struct{ repo *Repository }

func NewService(repo *Repository) *Service { return &Service{repo: repo} }

func (s *Service) List(ctx context.Context, kind string, f Filter) (Page, error) {
	if !validFilter(kind, f) {
		return Page{}, ErrInvalid
	}
	return s.repo.List(ctx, kind, f)
}

func (s *Service) Get(ctx context.Context, kind, id string) (Entry, error) {
	if (kind != "audit" && kind != "login") || !uuidPattern.MatchString(id) {
		return Entry{}, ErrInvalid
	}
	return s.repo.Get(ctx, kind, id)
}

func validFilter(kind string, f Filter) bool {
	if kind != "audit" && kind != "login" || f.Page < 1 || f.Page > 1000 || f.PageSize < 1 || f.PageSize > 50 || f.From.IsZero() || f.To.IsZero() || !f.From.Before(f.To) || f.To.Sub(f.From) > 366*24*time.Hour || time.Until(f.To) > 24*time.Hour {
		return false
	}
	if f.UserID != "" && !uuidPattern.MatchString(f.UserID) {
		return false
	}
	if kind == "audit" {
		return f.Outcome == "" && (f.Action == "" || actionPattern.MatchString(f.Action))
	}
	return f.Action == "" && (f.Outcome == "" || f.Outcome == "success" || f.Outcome == "failure" || f.Outcome == "rate_limited")
}
