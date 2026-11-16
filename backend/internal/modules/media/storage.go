package media

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

const MaxFileSize int64 = 25 << 20
const MaxImageSize int64 = 10 << 20
const MaxMultipartSize int64 = 26 << 20

var uuidName = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
var keyName = regexp.MustCompile(`^[0-9a-f]{32}$`)

var ErrCorrupt = errors.New("media integrity failure")
var ErrExists = errors.New("media object already exists")

// Storage deals only in generated IDs and opaque keys, never caller-supplied paths.
type Storage interface {
	Stage(context.Context, string, io.Reader) (int64, [32]byte, []byte, error)
	Put(context.Context, string, string, int64, [32]byte) error
	OpenVerified(context.Context, string, int64, [32]byte) (io.ReadSeekCloser, error)
	Delete(context.Context, string) error
	RemoveStage(string) error
}

type LocalStorage struct {
	root    *os.Root
	path    string
	minFree int64
}

func OpenLocal(path string) (*LocalStorage, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || path == "/" {
		return nil, errors.New("MEDIA_STORAGE_ROOT must be an absolute private directory")
	}
	info, err := os.Lstat(path)
	if err != nil || !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		return nil, errors.New("media root must exist, be private, and not be a symlink")
	}
	r, err := os.OpenRoot(path)
	if err != nil {
		return nil, err
	}
	s := &LocalStorage{root: r, path: path}
	for _, dir := range []string{"staging", "objects", "objects/.incoming", "downloads", "quarantine"} {
		if err = s.ensureDir(dir); err != nil {
			r.Close()
			return nil, err
		}
	}
	return s, nil
}

func (s *LocalStorage) Close() error           { return s.root.Close() }
func (s *LocalStorage) SetMinFree(bytes int64) { s.minFree = bytes }

func (s *LocalStorage) capacity() error {
	var stat unix.Statfs_t
	if err := unix.Statfs(s.path, &stat); err != nil {
		return err
	}
	if int64(stat.Bavail)*int64(stat.Bsize) < 3*MaxFileSize+s.minFree {
		return ErrNotReady
	}
	return nil
}

func (s *LocalStorage) ensureDir(name string) error {
	created := false
	if err := s.root.Mkdir(name, 0700); err != nil {
		if !errors.Is(err, os.ErrExist) {
			return err
		}
	} else {
		created = true
	}
	if created {
		if err := s.syncDir(filepath.Dir(name)); err != nil {
			return err
		}
	}
	info, err := s.root.Lstat(name)
	if err != nil || !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		return errors.New("media directory must be private and not a symlink")
	}
	return nil
}

func objectPath(key string) (string, error) {
	if !keyName.MatchString(key) {
		return "", errors.New("invalid opaque key")
	}
	return "objects/" + key[:2] + "/" + key, nil
}

func (s *LocalStorage) syncDir(dir string) error {
	f, err := s.root.Open(dir)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}

func (s *LocalStorage) Stage(ctx context.Context, op string, reader io.Reader) (int64, [32]byte, []byte, error) {
	var empty [32]byte
	if !uuidName.MatchString(op) {
		return 0, empty, nil, errors.New("invalid operation ID")
	}
	if err := s.capacity(); err != nil {
		return 0, empty, nil, err
	}
	f, err := s.root.OpenFile("staging/"+op+".part", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return 0, empty, nil, err
	}
	defer f.Close()
	ok := false
	defer func() {
		if !ok {
			_ = s.RemoveStage(op)
		}
	}()
	h := sha256.New()
	var head []byte
	buf := make([]byte, 32<<10)
	var size int64
	for {
		if err = ctx.Err(); err != nil {
			return 0, empty, nil, err
		}
		n, e := reader.Read(buf)
		if n > 0 {
			size += int64(n)
			if size > MaxFileSize {
				return 0, empty, nil, ErrTooLarge
			}
			if len(head) < 512 {
				head = append(head, buf[:min(n, 512-len(head))]...)
			}
			if _, err = f.Write(buf[:n]); err != nil {
				return 0, empty, nil, err
			}
			h.Write(buf[:n])
		}
		if e == io.EOF {
			break
		}
		if e != nil {
			return 0, empty, nil, e
		}
	}
	if size == 0 {
		return 0, empty, nil, ErrInvalid
	}
	if err = f.Sync(); err != nil {
		return 0, empty, nil, err
	}
	if err = s.syncDir("staging"); err != nil {
		return 0, empty, nil, err
	}
	var digest [32]byte
	copy(digest[:], h.Sum(nil))
	ok = true
	return size, digest, head, nil
}

func (s *LocalStorage) RemoveStage(op string) error {
	if !uuidName.MatchString(op) {
		return errors.New("invalid operation ID")
	}
	err := s.root.Remove("staging/" + op + ".part")
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	return s.syncDir("staging")
}

func (s *LocalStorage) OpenStage(op string) (io.ReadCloser, error) {
	if !uuidName.MatchString(op) {
		return nil, ErrInvalid
	}
	return s.root.Open("staging/" + op + ".part")
}

func (s *LocalStorage) Put(ctx context.Context, op, key string, size int64, digest [32]byte) error {
	_, err := objectPath(key)
	if err != nil || !uuidName.MatchString(op) || size < 1 || size > MaxFileSize {
		return ErrInvalid
	}
	if err = s.capacity(); err != nil {
		return err
	}
	if err = s.ensureDir("objects/" + key[:2]); err != nil {
		return err
	}
	stage, err := s.root.Open("staging/" + op + ".part")
	if err != nil {
		return err
	}
	defer stage.Close()
	incoming := "objects/.incoming/" + op + ".part"
	dst, err := s.root.OpenFile(incoming, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	// Preserve the incoming object on uncertain publication errors for reconcile.
	h := sha256.New()
	actual, copyErr := io.Copy(io.MultiWriter(dst, h), &contextReader{ctx, io.LimitReader(stage, MaxFileSize+1)})
	if copyErr == nil && (actual != size || actual > MaxFileSize || !equalHash(h.Sum(nil), digest)) {
		copyErr = ErrCorrupt
	}
	if copyErr == nil {
		copyErr = dst.Sync()
	}
	closeErr := dst.Close()
	if copyErr != nil {
		_ = s.root.Remove(incoming)
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	return s.PublishIncoming(ctx, op, key, size, digest)
}

// PublishIncoming resumes a fully durable incoming object without recopying staging.
func (s *LocalStorage) PublishIncoming(ctx context.Context, op, key string, size int64, digest [32]byte) error {
	final, err := objectPath(key)
	if err != nil || !uuidName.MatchString(op) || size < 1 || size > MaxFileSize {
		return ErrInvalid
	}
	incoming := "objects/.incoming/" + op + ".part"
	info, err := s.root.Lstat(incoming)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() != size {
		return ErrCorrupt
	}
	f, err := s.root.Open(incoming)
	if err != nil {
		return err
	}
	h := sha256.New()
	n, e := io.Copy(h, &contextReader{ctx, io.LimitReader(f, MaxFileSize+1)})
	if e == nil {
		e = f.Sync()
	}
	f.Close()
	if e != nil {
		return e
	}
	if n != size || !equalHash(h.Sum(nil), digest) {
		return ErrCorrupt
	}
	if err = s.syncDir("objects/.incoming"); err != nil {
		return err
	}
	if err = s.ensureDir("objects/" + key[:2]); err != nil {
		return err
	}
	// hard link is an atomic no-replace operation on one filesystem; never rename over an object.
	if err = s.root.Link(incoming, final); err != nil {
		if errors.Is(err, os.ErrExist) {
			return ErrExists
		} else {
			return err
		}
	}
	if err = s.syncDir("objects/" + key[:2]); err != nil {
		return err
	}
	if err = s.root.Remove(incoming); err != nil {
		return err
	}
	return s.syncDir("objects/.incoming")
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r *contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}
func equalHash(got []byte, want [32]byte) bool {
	return len(got) == 32 && subtle.ConstantTimeCompare(got, want[:]) == 1
}

func (s *LocalStorage) OpenVerified(ctx context.Context, key string, size int64, digest [32]byte) (io.ReadSeekCloser, error) {
	name, err := objectPath(key)
	if err != nil {
		return nil, err
	}
	source, err := s.root.Open(name)
	if err != nil {
		return nil, err
	}
	defer source.Close()
	info, err := source.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() != size {
		return nil, ErrCorrupt
	}
	// Snapshot is private and immutable for this request; bytes sent are precisely the checked bytes.
	tmpName := "downloads/" + randomName() + ".part"
	tmp, err := s.root.OpenFile(tmpName, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return nil, err
	}
	n, err := io.Copy(tmp, &contextReader{ctx, io.LimitReader(source, MaxFileSize+1)})
	closeErr := tmp.Close()
	if err != nil || closeErr != nil || n != size {
		_ = s.root.Remove(tmpName)
		return nil, ErrCorrupt
	}
	verified, err := s.root.Open(tmpName)
	if err != nil {
		_ = s.root.Remove(tmpName)
		return nil, err
	}
	// Verify and serve from this same read-only descriptor; unlink prevents replacement.
	if err = s.root.Remove(tmpName); err != nil {
		verified.Close()
		return nil, err
	}
	h := sha256.New()
	n, err = io.Copy(h, &contextReader{ctx, io.LimitReader(verified, MaxFileSize+1)})
	if err != nil || n != size || !equalHash(h.Sum(nil), digest) {
		verified.Close()
		return nil, ErrCorrupt
	}
	if _, err = verified.Seek(0, io.SeekStart); err != nil {
		verified.Close()
		return nil, err
	}
	return verified, nil
}

func (s *LocalStorage) Delete(ctx context.Context, key string) error {
	name, err := objectPath(key)
	if err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	err = s.root.Remove(name)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	return s.syncDir("objects/" + strings.ToLower(key[:2]))
}

func (s *LocalStorage) VerifyObject(ctx context.Context, key string, size int64, digest [32]byte) error {
	f, err := s.OpenVerified(ctx, key, size, digest)
	if err != nil {
		return err
	}
	return f.Close()
}

// LocalMaintenance exposes typed, bounded operations, never arbitrary paths.
type LocalMaintenance interface {
	InspectIncoming(string) (bool, error)
	InspectStage(string) (bool, error)
	RemoveIncoming(string) error
	RemoveStage(string) error
	VerifyObject(context.Context, string, int64, [32]byte) error
	ScanTemp(string, int, string) ([]TempEntry, error)
	QuarantineTemp(string, string) error
	ScanObjects(string, string, int) ([]ObjectEntry, error)
	QuarantineObject(string) error
}

type ObjectEntry struct {
	Key      string
	Modified time.Time
}

var prefixName = regexp.MustCompile(`^[a-f0-9]{2}$`)

// ScanObjects never accepts a caller path. A crowded shard fails closed instead
// of loading an unbounded directory into memory or silently missing files.
func (s *LocalStorage) ScanObjects(prefix, cursor string, limit int) ([]ObjectEntry, error) {
	if !prefixName.MatchString(prefix) || (cursor != "" && (!keyName.MatchString(cursor) || cursor[:2] != prefix)) || limit < 1 || limit > 100 {
		return nil, ErrInvalid
	}
	if info, err := s.root.Lstat("objects/" + prefix); errors.Is(err, os.ErrNotExist) {
		return nil, nil
	} else if err != nil || !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		return nil, ErrCorrupt
	}
	f, err := s.root.Open("objects/" + prefix)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	entries, err := f.ReadDir(501)
	if err != nil && err != io.EOF {
		return nil, err
	}
	if len(entries) > 500 {
		return nil, ErrNotReady
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	result := make([]ObjectEntry, 0, min(limit, len(entries)))
	for _, entry := range entries {
		key := entry.Name()
		if !keyName.MatchString(key) || key[:2] != prefix || !entry.Type().IsRegular() {
			return nil, ErrCorrupt
		}
		if key <= cursor {
			continue
		}
		info, e := s.root.Lstat("objects/" + prefix + "/" + key)
		if e != nil || !info.Mode().IsRegular() {
			return nil, ErrCorrupt
		}
		result = append(result, ObjectEntry{Key: key, Modified: info.ModTime()})
		if len(result) == limit {
			break
		}
	}
	return result, nil
}

func (s *LocalStorage) QuarantineObject(key string) error {
	source, err := objectPath(key)
	if err != nil {
		return ErrInvalid
	}
	info, err := s.root.Lstat(source)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || time.Since(info.ModTime()) < 24*time.Hour {
		return ErrConflict
	}
	// Link is no-replace. A failed sync leaves the source in place for review.
	destination := "quarantine/object-" + key
	if err = s.root.Link(source, destination); err != nil {
		return err
	}
	if err = s.syncDir("quarantine"); err != nil {
		return err
	}
	if err = s.root.Remove(source); err != nil {
		return err
	}
	return s.syncDir("objects/" + key[:2])
}

type TempEntry struct {
	OperationID string
	Modified    time.Time
}

func tempDirectory(kind string) (string, error) {
	switch kind {
	case "staging":
		return "staging", nil
	case "incoming":
		return "objects/.incoming", nil
	default:
		return "", ErrInvalid
	}
}

func (s *LocalStorage) ScanTemp(kind string, limit int, cursor string) ([]TempEntry, error) {
	dir, err := tempDirectory(kind)
	if err != nil || limit < 1 || limit > 100 {
		return nil, ErrInvalid
	}
	f, err := s.root.Open(dir)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	entries, err := f.ReadDir(501)
	if err != nil && err != io.EOF {
		return nil, err
	}
	if len(entries) > 500 {
		return nil, ErrNotReady
	}
	all := []TempEntry{}
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".part") || !uuidName.MatchString(strings.TrimSuffix(name, ".part")) {
			continue
		}
		info, e := s.root.Lstat(dir + "/" + name)
		if e != nil {
			return nil, e
		}
		if !info.Mode().IsRegular() {
			return nil, ErrCorrupt
		}
		all = append(all, TempEntry{OperationID: strings.TrimSuffix(name, ".part"), Modified: info.ModTime()})
	}
	result := []TempEntry{}
	for _, candidate := range all {
		if candidate.OperationID > cursor {
			result = append(result, candidate)
			if len(result) >= limit {
				break
			}
		}
	}
	if len(result) == 0 && cursor != "" {
		for _, candidate := range all {
			result = append(result, candidate)
			if len(result) >= limit {
				break
			}
		}
	}
	return result, nil
}

func (s *LocalStorage) QuarantineTemp(kind, op string) error {
	dir, err := tempDirectory(kind)
	if err != nil || !uuidName.MatchString(op) {
		return ErrInvalid
	}
	source := dir + "/" + op + ".part"
	info, err := s.root.Lstat(source)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return ErrCorrupt
	}
	destination := "quarantine/" + kind + "-" + op + ".part"
	if err = s.root.Link(source, destination); err != nil {
		return err
	}
	if err = s.syncDir("quarantine"); err != nil {
		return err
	}
	if err = s.root.Remove(source); err != nil {
		return err
	}
	return s.syncDir(dir)
}

func (s *LocalStorage) InspectIncoming(op string) (bool, error) {
	if !uuidName.MatchString(op) {
		return false, ErrInvalid
	}
	info, err := s.root.Lstat("objects/.incoming/" + op + ".part")
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !info.Mode().IsRegular() {
		return false, ErrCorrupt
	}
	return true, nil
}
func (s *LocalStorage) InspectStage(op string) (bool, error) {
	if !uuidName.MatchString(op) {
		return false, ErrInvalid
	}
	info, err := s.root.Lstat("staging/" + op + ".part")
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !info.Mode().IsRegular() {
		return false, ErrCorrupt
	}
	return true, nil
}
func (s *LocalStorage) RemoveIncoming(op string) error {
	if !uuidName.MatchString(op) {
		return ErrInvalid
	}
	err := s.root.Remove("objects/.incoming/" + op + ".part")
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	return s.syncDir("objects/.incoming")
}

func randomName() string {
	// A dedicated random UUID is used for the transient snapshot name.
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("cryptographic randomness unavailable")
	}
	return fmt.Sprintf("%x", b)
}

func uuid() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("cryptographic randomness unavailable")
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[:4], b[4:6], b[6:8], b[8:10], b[10:])
}
