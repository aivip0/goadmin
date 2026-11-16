package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/mail"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"goadminkit/backend/internal/core/audit"
)

var usernamePattern = regexp.MustCompile(`^[a-zA-Z0-9_.-]{3,64}$`)

type Service struct {
	repo      Repository
	dummy     string
	ttl, idle time.Duration
	hashing   chan struct{}
}

func NewService(repo Repository, ttl, idle time.Duration) (*Service, error) {
	var seed [32]byte
	if _, err := rand.Read(seed[:]); err != nil {
		return nil, err
	}
	dummy, err := HashPassword(hex.EncodeToString(seed[:]))
	if err != nil {
		return nil, err
	}
	return &Service{repo: repo, dummy: dummy, ttl: ttl, idle: idle, hashing: make(chan struct{}, 4)}, nil
}

func (s *Service) Initialize(ctx context.Context, input InitialAdmin) (User, error) {
	input.Username = strings.ToLower(strings.TrimSpace(input.Username))
	input.Email = strings.ToLower(strings.TrimSpace(input.Email))
	address, err := mail.ParseAddress(input.Email)
	if !usernamePattern.MatchString(input.Username) || err != nil || address.Address != input.Email || len(input.Email) > 254 || utf8.RuneCountInString(input.Password) < 15 || len(input.Password) > 128 || len(input.DisplayName) > 128 {
		return User{}, ErrInvalid
	}
	hash, err := HashPassword(input.Password)
	if err != nil {
		return User{}, err
	}
	return s.repo.Initialize(ctx, input, hash)
}

func (s *Service) Login(ctx context.Context, name, password, ip string) (User, string, error) {
	return s.LoginWithContext(ctx, name, password, ip, "")
}

func (s *Service) LoginWithContext(ctx context.Context, name, password, ip, agent string) (User, string, error) {
	name = strings.ToLower(strings.TrimSpace(name))
	if !usernamePattern.MatchString(name) || password == "" || len(password) > 128 {
		return User{}, "", ErrInvalid
	}
	key := tokenHash("account:" + name)
	event := LoginOutcome{IP: ip, UserAgent: audit.BrowserFamily(agent)}
	if err := s.repo.Attempt(ctx, key, tokenHash("ip:"+ip), event); err != nil {
		return User{}, "", err
	}
	// A bounded hashing pool controls Argon2 memory; persistent buckets remain authoritative.
	select {
	case s.hashing <- struct{}{}:
		defer func() { <-s.hashing }()
	case <-ctx.Done():
		return User{}, "", ctx.Err()
	}
	a, err := s.repo.Find(ctx, name)
	if err != nil && !errors.Is(err, ErrCredentials) {
		return User{}, "", err
	}
	hash := a.PasswordHash
	if err != nil {
		hash = s.dummy
	}
	valid := VerifyPassword(hash, password)
	if !valid || err != nil || a.Status != "active" {
		if err == nil {
			event.UserID = a.ID
		}
		if err = s.repo.Failure(ctx, key, event); err != nil {
			return User{}, "", err
		}
		return User{}, "", ErrCredentials
	}
	token, digest, err := NewToken()
	if err != nil {
		return User{}, "", err
	}
	u, err := s.repo.Create(ctx, a, digest, key, s.ttl, event)
	if err != nil {
		if errors.Is(err, ErrCredentials) {
			if e := s.repo.Failure(ctx, key, LoginOutcome{UserID: a.ID, IP: ip, UserAgent: event.UserAgent}); e != nil {
				return User{}, "", e
			}
		}
		return User{}, "", err
	}
	return u, token, nil
}

func (s *Service) Me(ctx context.Context, token string) (User, error) {
	if !validToken(token) {
		return User{}, ErrUnauthorized
	}
	session, err := s.repo.Authenticate(ctx, tokenHash(token), s.idle)
	return session.User, err
}
func (s *Service) Logout(ctx context.Context, token string) error {
	if !validToken(token) {
		return nil
	}
	return s.repo.Revoke(ctx, tokenHash(token))
}
func (s *Service) LogoutAll(ctx context.Context, token string) error {
	if !validToken(token) {
		return ErrUnauthorized
	}
	return s.repo.RevokeAll(ctx, tokenHash(token), s.idle)
}
