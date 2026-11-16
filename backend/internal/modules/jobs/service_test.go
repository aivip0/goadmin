package jobs

import (
	"context"
	"errors"
	"testing"
)

func TestValidation(t *testing.T) {
	s := NewService(nil)
	if _, err := s.Enqueue(context.Background(), "bad", SessionCleanup); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
	if _, err := s.Enqueue(context.Background(), "00000000-0000-4000-8000-000000000001", "arbitrary"); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
	if _, err := s.Detail(context.Background(), "../../etc"); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
}
