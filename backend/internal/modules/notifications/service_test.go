package notifications

import (
	"context"
	"errors"
	"testing"
)

func TestValidation(t *testing.T) {
	id := "00000000-0000-4000-8000-000000000001"
	if !uuid.MatchString(id) {
		t.Fatalf("valid UUID rejected by %s", uuid.String())
	}
	s := NewService(nil)
	for _, in := range []Input{{RecipientID: id, Title: " ", Body: "body"}, {RecipientID: "bad", Title: "hello", Body: "body"}, {RecipientID: id, Title: "hello", Body: " "}} {
		if _, err := s.Create(context.Background(), id, in); !errors.Is(err, ErrInvalid) {
			t.Fatalf("invalid input: %v", err)
		}
	}
	if _, err := s.List(context.Background(), id, 0, 20); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
	if _, err := s.Read(context.Background(), id, "bad"); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
}
