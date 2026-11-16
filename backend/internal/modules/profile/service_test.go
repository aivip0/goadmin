package profile

import (
	"context"
	"errors"
	"testing"

	"goadminkit/backend/internal/modules/auth"
)

type fakeRepository struct {
	credential credential
	updated    Input
	password   PasswordInput
	change     bool
	loadErr    error
	revokeID   string
	revokeAll  bool
}

func (f *fakeRepository) Load(context.Context, string) (credential, error) {
	return f.credential, f.loadErr
}
func (f *fakeRepository) Update(_ context.Context, _ string, in Input) (auth.User, error) {
	f.updated = in
	f.credential.User.Email, f.credential.User.DisplayName = in.Email, in.DisplayName
	return f.credential.User, nil
}
func (f *fakeRepository) PasswordAttempt(context.Context, string) error { return nil }
func (f *fakeRepository) ChangePassword(context.Context, string, string, string) error {
	f.change = true
	return nil
}
func (f *fakeRepository) Sessions(context.Context, string, int, int) (SessionPage, error) {
	return SessionPage{Items: []Session{}}, nil
}
func (f *fakeRepository) Revoke(_ context.Context, _, id string, all bool) (bool, error) {
	f.revokeID, f.revokeAll = id, all
	return false, nil
}

func TestProfileValidationAndNormalization(t *testing.T) {
	repo := &fakeRepository{}
	s := NewService(repo)
	user, err := s.Update(context.Background(), "session", Input{Email: " Person@Example.Test ", DisplayName: " Ada "})
	if err != nil || user.Email != "person@example.test" || user.DisplayName != "Ada" {
		t.Fatalf("normalized update: %#v %v", user, err)
	}
	for _, in := range []Input{{Email: "not-an-email", DisplayName: "Ada"}, {Email: "x@example.test", DisplayName: "bad\nname"}} {
		if _, err = s.Update(context.Background(), "session", in); !errors.Is(err, ErrInvalid) {
			t.Fatalf("accepted invalid profile input: %#v, %v", in, err)
		}
	}
	if repo.updated.Email != "person@example.test" {
		t.Fatalf("invalid value reached repository: %#v", repo.updated)
	}
}

func TestPasswordPolicyAndSessionBounds(t *testing.T) {
	s := NewService(&fakeRepository{})
	for _, in := range []PasswordInput{{CurrentPassword: "x", NewPassword: "short"}, {CurrentPassword: "same password long", NewPassword: "same password long"}} {
		if err := s.Password(context.Background(), "session", in); !errors.Is(err, ErrInvalid) {
			t.Fatalf("accepted invalid password change: %v", err)
		}
	}
	for _, pair := range [][2]int{{0, 20}, {1, 0}, {1001, 20}, {1, 51}} {
		if _, err := s.Sessions(context.Background(), "session", pair[0], pair[1]); !errors.Is(err, ErrInvalid) {
			t.Fatalf("accepted invalid page: %v", pair)
		}
	}
}

func TestRevokeRequiresUUID(t *testing.T) {
	repo := &fakeRepository{}
	s := NewService(repo)
	if _, err := s.Revoke(context.Background(), "session", "not-a-uuid", false); !errors.Is(err, ErrInvalid) {
		t.Fatalf("accepted arbitrary identifier: %v", err)
	}
	if _, err := s.Revoke(context.Background(), "session", "", true); err != nil || !repo.revokeAll {
		t.Fatalf("revoke others: %v", err)
	}
}
