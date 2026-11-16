package apikeys

import (
	"context"
	"errors"
	"testing"
)

func TestTokenEntropyAndValidation(t *testing.T) {
	a, e := token()
	if e != nil {
		t.Fatal(e)
	}
	b, e := token()
	if e != nil || a == b || len(a) != 47 || digest(a) == digest(b) {
		t.Fatal("invalid token entropy")
	}
	s := NewService(nil)
	if _, e = s.Resolve(context.Background(), "invalid"); !errors.Is(e, ErrUnauthorized) {
		t.Fatal(e)
	}
	if _, e = s.Create(context.Background(), "bad", Input{Name: "test", ExpiresInDays: 1}); !errors.Is(e, ErrInvalid) {
		t.Fatal(e)
	}
}
