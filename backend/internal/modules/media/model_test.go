package media

import (
	"bytes"
	"context"
	"errors"
	"os"
	"strings"
	"testing"
)

func TestMediaValidation(t *testing.T) {
	for _, name := range []string{"../x.txt", "..\\x.txt", "foo\n.txt", "bad.svg.txt", "bad.js.txt", "", ".hidden.txt"} {
		_, _, err := validateName(name)
		if err == nil {
			t.Errorf("unsafe name accepted: %q", name)
		}
	}
	root := t.TempDir()
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	s, err := OpenLocal(root)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for _, tc := range []struct {
		name, data string
		want       error
	}{
		{"broken.png", "not an image", ErrUnsupported},
		{"mismatched.pdf", "plain text", ErrUnsupported},
		{"injected.txt", "<html><script>unsafe</script></html>", ErrUnsupported},
		{"binary.txt", "plain\x00text", ErrUnsupported},
		{"unknown.svg", "<svg></svg>", ErrUnsupported},
	} {
		op := uuid()
		size, _, head, e := s.Stage(context.Background(), op, strings.NewReader(tc.data))
		if e != nil {
			t.Fatal(e)
		}
		_, _, _, e = inspect(s, op, tc.name, size, head)
		if !errors.Is(e, tc.want) {
			t.Errorf("%s: got %v want %v", tc.name, e, tc.want)
		}
	}
}

func TestStorageCancellationAndLimits(t *testing.T) {
	root := t.TempDir()
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	s, err := OpenLocal(root)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, _, err = s.Stage(ctx, uuid(), bytes.NewReader([]byte("abc"))); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel: %v", err)
	}
	if _, _, _, err = s.Stage(context.Background(), uuid(), bytes.NewReader(make([]byte, MaxFileSize+1))); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("limit: %v", err)
	}
}
