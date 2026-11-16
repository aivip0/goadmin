package profile

import (
	"context"
	"net/mail"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"goadminkit/backend/internal/modules/auth"
)

type Repository interface {
	Load(context.Context, string) (credential, error)
	Update(context.Context, string, Input) (auth.User, error)
	PasswordAttempt(context.Context, string) error
	ChangePassword(context.Context, string, string, string) error
	Sessions(context.Context, string, int, int) (SessionPage, error)
	Revoke(context.Context, string, string, bool) (bool, error)
}

type Service struct {
	repo    Repository
	hashing chan struct{}
}

func NewService(repo Repository) *Service {
	return &Service{repo: repo, hashing: make(chan struct{}, 2)}
}

func (s *Service) Get(ctx context.Context, token string) (auth.User, error) {
	c, err := s.repo.Load(ctx, token)
	return c.User, err
}

func (s *Service) Update(ctx context.Context, token string, in Input) (auth.User, error) {
	in.Email = strings.ToLower(strings.TrimSpace(in.Email))
	in.DisplayName = strings.TrimSpace(in.DisplayName)
	a, err := mail.ParseAddress(in.Email)
	if err != nil || a.Address != in.Email || len(in.Email) > 254 || len(in.DisplayName) > 128 || !utf8.ValidString(in.DisplayName) || strings.ContainsFunc(in.DisplayName, unicode.IsControl) {
		return auth.User{}, ErrInvalid
	}
	return s.repo.Update(ctx, token, in)
}

func (s *Service) Password(ctx context.Context, token string, in PasswordInput) error {
	if in.CurrentPassword == "" || len(in.CurrentPassword) > 128 || !utf8.ValidString(in.NewPassword) || utf8.RuneCountInString(in.NewPassword) < 15 || len(in.NewPassword) > 128 || in.CurrentPassword == in.NewPassword {
		return ErrInvalid
	}
	c, err := s.repo.Load(ctx, token)
	if err != nil {
		return err
	}
	if err = s.repo.PasswordAttempt(ctx, c.User.ID); err != nil {
		return err
	}
	select {
	case s.hashing <- struct{}{}:
		defer func() { <-s.hashing }()
	case <-ctx.Done():
		return ctx.Err()
	}
	if !auth.VerifyPassword(c.Hash, in.CurrentPassword) {
		return ErrPassword
	}
	hash, err := auth.HashPassword(in.NewPassword)
	if err != nil {
		return err
	}
	return s.repo.ChangePassword(ctx, token, c.Hash, hash)
}

func (s *Service) Sessions(ctx context.Context, token string, page, size int) (SessionPage, error) {
	if page < 1 || page > 1000 || size < 1 || size > 50 {
		return SessionPage{}, ErrInvalid
	}
	return s.repo.Sessions(ctx, token, page, size)
}

var uuid = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

func (s *Service) Revoke(ctx context.Context, token, id string, others bool) (bool, error) {
	if !others && !uuid.MatchString(id) {
		return false, ErrInvalid
	}
	return s.repo.Revoke(ctx, token, id, others)
}
