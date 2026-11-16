package apikeys

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"strings"
)

type Service struct{ repo *Repository }

func NewService(repo *Repository) *Service { return &Service{repo} }
func token() (string, error) {
	var entropy [32]byte
	if _, err := rand.Read(entropy[:]); err != nil {
		return "", err
	}
	return "gak_" + base64.RawURLEncoding.EncodeToString(entropy[:]), nil
}
func digest(raw string) [32]byte { return sha256.Sum256([]byte(raw)) }
func (s *Service) Create(ctx context.Context, owner string, in Input) (Created, error) {
	if !uuid.MatchString(owner) || in.validate() != nil {
		return Created{}, ErrInvalid
	}
	raw, err := token()
	if err != nil {
		return Created{}, err
	}
	hash := digest(raw)
	v, err := s.repo.Create(ctx, owner, in, raw[:12], hash[:])
	if err != nil {
		return Created{}, err
	}
	return Created{Item: v, Token: raw}, nil
}
func (s *Service) List(ctx context.Context, owner string, page, size int) (Page, error) {
	if !uuid.MatchString(owner) || page < 1 || page > 1000 || size < 1 || size > 50 {
		return Page{}, ErrInvalid
	}
	return s.repo.List(ctx, owner, page, size)
}
func (s *Service) Revoke(ctx context.Context, owner, id string) error {
	if !uuid.MatchString(owner) || !uuid.MatchString(id) {
		return ErrInvalid
	}
	return s.repo.Revoke(ctx, owner, id)
}
func (s *Service) Resolve(ctx context.Context, raw string) (string, error) {
	if len(raw) != 47 || !strings.HasPrefix(raw, "gak_") {
		return "", ErrUnauthorized
	}
	if decoded, err := base64.RawURLEncoding.DecodeString(raw[4:]); err != nil || len(decoded) != 32 {
		return "", ErrUnauthorized
	}
	hash := digest(raw)
	owner, err := s.repo.Resolve(ctx, hash[:])
	if err == nil {
		s.repo.Used(ctx, hash[:])
	}
	return owner, err
}
