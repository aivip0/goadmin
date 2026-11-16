package media

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"os"
	"testing"
)

// Run in two separate Linux containers sharing only a disposable Docker volume.
func TestDisposableDockerVolume(t *testing.T) {
	mode := os.Getenv("MEDIA_VOLUME_TEST_MODE")
	if mode == "" {
		t.Skip("requires an explicitly mounted disposable Docker media volume")
	}
	root := os.Getenv("MEDIA_VOLUME_TEST_ROOT")
	s, err := OpenLocal(root)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	data := []byte("durable private volume fixture\n")
	digest := sha256.Sum256(data)
	switch mode {
	case "write":
		size, got, _, err := s.Stage(context.Background(), testOp, bytes.NewReader(data))
		if err != nil || got != digest {
			t.Fatalf("volume stage/fsync: %v", err)
		}
		if err := s.Put(context.Background(), testOp, testKey, size, digest); err != nil {
			t.Fatalf("volume atomic link/dir fsync: %v", err)
		}
		if err := s.Put(context.Background(), testOp, testKey, size, digest); !errors.Is(err, ErrExists) {
			t.Fatalf("volume no-replace failed: %v", err)
		}
	case "read":
		f, err := s.OpenVerified(context.Background(), testKey, int64(len(data)), digest)
		if err != nil {
			t.Fatalf("remount integrity: %v", err)
		}
		got, err := io.ReadAll(f)
		f.Close()
		if err != nil || !bytes.Equal(got, data) {
			t.Fatalf("remount bytes: %v", err)
		}
	default:
		t.Fatal("invalid disposable volume test mode")
	}
}
