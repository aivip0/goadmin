package auth

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestPasswordAndTokens(t *testing.T) {
	hash, err := HashPassword("a dedicated unit test password")
	if err != nil {
		t.Fatal(err)
	}
	other, err := HashPassword("a dedicated unit test password")
	if err != nil {
		t.Fatal(err)
	}
	if hash == other || !VerifyPassword(hash, "a dedicated unit test password") || VerifyPassword(hash, "wrong") {
		t.Fatal("salt or password verification failed")
	}
	for _, bad := range []string{"", "$argon2id$v=19$m=999999999,t=3,p=1$a$b", passwordPrefix + "a$b"} {
		if VerifyPassword(bad, "wrong") {
			t.Fatal("malformed hash accepted")
		}
	}
	seen := map[string]bool{}
	for range 100 {
		token, hash, err := NewToken()
		if err != nil || !validToken(token) || len(hash) != 32 || seen[token] || string(hash) == token {
			t.Fatal("unsafe session generation")
		}
		seen[token] = true
	}
}

type memoryRepo struct {
	Repository
	a         account
	absent    bool
	failures  int
	limited   bool
	stored    []byte
	revoked   bool
	expired   bool
	reset     bool
	createErr error
}

func (r *memoryRepo) Find(context.Context, string) (account, error) {
	if r.absent {
		return account{}, ErrCredentials
	}
	return r.a, nil
}
func (r *memoryRepo) Attempt(context.Context, []byte, []byte, LoginOutcome) error {
	if r.limited {
		return &Limited{Retry: time.Second}
	}
	return nil
}
func (r *memoryRepo) Failure(context.Context, []byte, LoginOutcome) error { r.failures++; return nil }
func (r *memoryRepo) Create(_ context.Context, a account, hash, key []byte, ttl time.Duration, _ LoginOutcome) (User, error) {
	if r.createErr != nil {
		return User{}, r.createErr
	}
	r.stored = append([]byte(nil), hash...)
	r.failures = 0
	r.reset = true
	return a.User, nil
}
func (r *memoryRepo) Authenticate(_ context.Context, hash []byte, _ time.Duration) (session, error) {
	if r.revoked || r.expired || r.a.Status != "active" || string(hash) != string(r.stored) {
		return session{}, ErrUnauthorized
	}
	return session{User: r.a.User}, nil
}
func (r *memoryRepo) Revoke(context.Context, []byte) error { r.revoked = true; return nil }
func (r *memoryRepo) RevokeAll(context.Context, []byte, time.Duration) error {
	r.revoked = true
	return nil
}

func TestServiceAuthenticationLifecycle(t *testing.T) {
	hash, _ := HashPassword("a dedicated unit test password")
	r := &memoryRepo{a: account{User: User{ID: "test-user", Username: "admin"}, PasswordHash: hash, Status: "active", AuthVersion: 1}}
	s, err := NewService(r, time.Hour, 30*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for _, mode := range []string{"wrong", "absent", "disabled"} {
		r.absent = mode == "absent"
		r.a.Status = "active"
		password := "a dedicated unit test password"
		if mode == "wrong" {
			password = "wrong"
		}
		if mode == "disabled" {
			r.a.Status = "disabled"
		}
		if _, raw, err := s.Login(ctx, "admin", password, "127.0.0.1"); !errors.Is(err, ErrCredentials) || raw != "" {
			t.Fatal("credential failure disclosed or accepted", mode)
		}
	}
	if r.failures != 3 {
		t.Fatal("failures not recorded")
	}
	r.absent = false
	r.a.Status = "active"
	r.limited = true
	var limited *Limited
	if _, _, err := s.Login(ctx, "admin", "a dedicated unit test password", "127.0.0.1"); !errors.As(err, &limited) {
		t.Fatal("rate limit bypass")
	}
	r.limited = false
	_, raw, err := s.Login(ctx, "admin", "a dedicated unit test password", "127.0.0.1")
	if err != nil || !r.reset || r.failures != 0 || string(r.stored) == raw || string(r.stored) != string(tokenHash(raw)) {
		t.Fatal("unsafe session persistence", err)
	}
	if _, err = s.Me(ctx, raw); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"expired", "revoked", "disabled"} {
		r.expired = mode == "expired"
		r.revoked = mode == "revoked"
		r.a.Status = "active"
		if mode == "disabled" {
			r.a.Status = "disabled"
		}
		if _, err = s.Me(ctx, raw); !errors.Is(err, ErrUnauthorized) {
			t.Fatal("invalid session accepted", mode)
		}
	}
	r.expired = false
	r.revoked = false
	r.a.Status = "active"
	if err = s.Logout(ctx, raw); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Me(ctx, raw); !errors.Is(err, ErrUnauthorized) {
		t.Fatal("logout token reusable")
	}
	if err = s.Logout(ctx, ""); err != nil {
		t.Fatal("logout not idempotent")
	}
	r.createErr = errors.New("transaction failed")
	if _, raw, err = s.Login(ctx, "admin", "a dedicated unit test password", "127.0.0.1"); err == nil || raw != "" {
		t.Fatal("failed transaction leaked token")
	}
}
