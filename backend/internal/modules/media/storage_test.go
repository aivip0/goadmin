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

const testOp = "d9c010a2-793f-4d84-ade5-77427ba9a675"
const testKey = "a93f85487a3adbc0a71b6486f49d91c7"

func TestLocalStorageIntegrityAndNoReplace(t *testing.T) {
	root := t.TempDir()
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	s, err := OpenLocal(root)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	data := []byte("sample document\n")
	size, digest, _, err := s.Stage(context.Background(), testOp, bytes.NewReader(data))
	if err != nil || size != int64(len(data)) || digest != sha256.Sum256(data) {
		t.Fatalf("stage: %v", err)
	}
	if err = s.Put(context.Background(), testOp, testKey, size, digest); err != nil {
		t.Fatal(err)
	}
	if err = s.Put(context.Background(), testOp, testKey, size, digest); !errors.Is(err, ErrExists) {
		t.Fatalf("overwrite: %v", err)
	}
	f, err := s.OpenVerified(context.Background(), testKey, size, digest)
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(f)
	f.Close()
	if err != nil || !bytes.Equal(got, data) {
		t.Fatal("bad verified snapshot")
	}
	// The opened snapshot must remain the verified bytes even if the object changes.
	snapshot, err := s.OpenVerified(context.Background(), testKey, size, digest)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(root+"/objects/a9/"+testKey, []byte("tampered"), 0600); err != nil {
		t.Fatal(err)
	}
	got, err = io.ReadAll(snapshot)
	snapshot.Close()
	if err != nil || !bytes.Equal(got, data) {
		t.Fatal("verified snapshot changed with source")
	}
	if err = s.RemoveStage(testOp); err != nil {
		t.Fatal(err)
	}
	if _, err = s.OpenVerified(context.Background(), testKey, size, digest); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("corruption not detected: %v", err)
	}
	if err = s.Delete(context.Background(), testKey); err != nil {
		t.Fatal(err)
	}
	if err = s.Delete(context.Background(), testKey); err != nil {
		t.Fatal(err)
	}
}

func TestLocalStorageRejectsSymlinkAndInvalidKey(t *testing.T) {
	root := t.TempDir()
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(t.TempDir(), root+"/objects"); err != nil {
		t.Fatal(err)
	}
	if s, err := OpenLocal(root); err == nil {
		s.Close()
		t.Fatal("symlink accepted")
	}
	if _, err := objectPath("../../secrets"); err == nil {
		t.Fatal("key accepted")
	}
}
