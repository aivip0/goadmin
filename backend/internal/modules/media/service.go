package media

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"io/fs"
	"mime"
	"mime/multipart"
	"os"
	"strings"
	"time"
)

type Service struct {
	repo  *Repository
	store *LocalStorage
	lock  *sql.Conn
}

func NewService(ctx context.Context, r *Repository, s *LocalStorage) (*Service, error) {
	lock, err := r.LockInstance(ctx)
	if err != nil {
		return nil, err
	}
	svc := &Service{repo: r, store: s, lock: lock}
	if err = svc.rootMatches(ctx); err != nil {
		lock.Close()
		return nil, err
	}
	verifyCtx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	if err = r.VerifyAll(verifyCtx, s); err != nil {
		lock.Close()
		return nil, err
	}
	return svc, nil
}
func (s *Service) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, err := s.lock.ExecContext(ctx, `SELECT pg_advisory_unlock($1)`, apiLockKey)
	return errors.Join(err, s.lock.Close())
}
func (s *Service) rootMatches(ctx context.Context) error {
	id, err := s.repo.RootID(ctx)
	if err != nil {
		return ErrNotReady
	}
	f, err := s.store.root.Open(".goadmin-media-root")
	if err != nil {
		return ErrNotReady
	}
	defer f.Close()
	if info, e := s.store.root.Lstat(".goadmin-media-root"); e != nil || !info.Mode().IsRegular() {
		return ErrNotReady
	}
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > 64 {
		return ErrNotReady
	}
	b, err := io.ReadAll(io.LimitReader(f, 65))
	if err != nil || strings.TrimSpace(string(b)) != id {
		return ErrNotReady
	}
	return nil
}
func (s *Service) gate(ctx context.Context) error {
	if err := s.repo.CheckLock(ctx, s.lock); err != nil {
		return ErrNotReady
	}
	if err := s.rootMatches(ctx); err != nil {
		return err
	}
	_, err := s.repo.generation(ctx)
	return err
}

func BindRoot(ctx context.Context, r *Repository, s *LocalStorage, actor string) error {
	if !uuidName.MatchString(actor) {
		return ErrInvalid
	}
	if _, err := r.RootID(ctx); err == nil {
		return ErrConflict
	} else if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	for _, dir := range []string{"staging", "objects/.incoming", "downloads", "quarantine"} {
		entries, err := readEntries(s.root, dir)
		if err != nil || len(entries) != 0 {
			return ErrConflict
		}
	}
	entries, err := readEntries(s.root, "objects")
	if err != nil || len(entries) != 1 || entries[0].Name() != ".incoming" {
		return ErrConflict
	}
	rootEntries, err := readEntries(s.root, ".")
	if err != nil || len(rootEntries) != 4 {
		return ErrConflict
	}
	id := uuid()
	f, err := s.root.OpenFile(".goadmin-media-root.tmp", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	if _, err = f.WriteString(id + "\n"); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	if err = s.root.Link(".goadmin-media-root.tmp", ".goadmin-media-root"); err != nil {
		return err
	}
	if err = s.syncDir("."); err != nil {
		return err
	}
	if err = s.root.Remove(".goadmin-media-root.tmp"); err != nil {
		return err
	}
	if err = s.syncDir("."); err != nil {
		return err
	}
	return r.Bind(ctx, id, actor)
}

func (s *Service) Upload(ctx context.Context, actor string, reader *multipart.Reader) (File, error) {
	var f File
	if err := s.gate(ctx); err != nil {
		return f, err
	}
	part, err := reader.NextPart()
	if err != nil {
		return f, ErrInvalid
	}
	_, params, err := mime.ParseMediaType(part.Header.Get("Content-Disposition"))
	if err != nil || part.FormName() != "file" || params["filename"] == "" {
		return f, ErrInvalid
	}
	name, _, err := validateName(params["filename"])
	if err != nil {
		return f, err
	}
	f.OperationID = uuid()
	f.Key = randomName()
	f.OriginalName = name
	var head []byte
	f.Size, f.Digest, head, err = s.store.Stage(ctx, f.OperationID, part)
	part.Close()
	if err != nil {
		return File{}, err
	}
	removeOnError := true
	defer func() {
		if removeOnError {
			_ = s.store.RemoveStage(f.OperationID)
		}
	}()
	if extra, e := reader.NextPart(); e != io.EOF {
		if extra != nil {
			extra.Close()
		}
		return File{}, ErrInvalid
	}
	f.MIME, f.Width, f.Height, err = inspect(s.store, f.OperationID, f.OriginalName, f.Size, head)
	if err != nil {
		return File{}, err
	}
	f.SHA256 = digestHex(f.Digest)
	if err = s.repo.Pending(ctx, actor, &f); err != nil {
		return File{}, err
	}
	// Once pending is durable, recovery owns the staging file even if this request fails.
	removeOnError = false
	if err = s.store.Put(ctx, f.OperationID, f.Key, f.Size, f.Digest); err != nil {
		// The final link may have been published before fsync failed: leave pending for recovery.
		return File{}, ErrNotReady
	}
	if err = s.gate(ctx); err != nil {
		return File{}, err
	}
	if err = s.repo.Ready(ctx, actor, &f); err != nil {
		return File{}, err
	}
	// A failed cleanup is recoverable; it does not undo a committed ready row.
	_ = s.store.RemoveStage(f.OperationID)
	return f, nil
}

func (s *Service) List(ctx context.Context, page, size int, kind, q string) ([]File, int64, error) {
	if err := s.gate(ctx); err != nil {
		return nil, 0, err
	}
	if page < 1 || page > 1000 || size < 1 || size > 100 || len(q) > 80 || (kind != "" && kind != "image" && kind != "document") {
		return nil, 0, ErrInvalid
	}
	return s.repo.List(ctx, page, size, kind, q)
}
func (s *Service) Get(ctx context.Context, id string) (File, error) {
	if !uuidName.MatchString(id) {
		return File{}, ErrInvalid
	}
	if err := s.gate(ctx); err != nil {
		return File{}, err
	}
	return s.repo.Get(ctx, id)
}
func (s *Service) Content(ctx context.Context, id string) (File, io.ReadSeekCloser, error) {
	f, err := s.Get(ctx, id)
	if err != nil {
		return f, nil, err
	}
	stream, err := s.store.OpenVerified(ctx, f.Key, f.Size, f.Digest)
	if errors.Is(err, ErrCorrupt) || errors.Is(err, os.ErrNotExist) {
		_ = s.repo.Unavailable(ctx, id)
		return File{}, nil, ErrNotReady
	}
	return f, stream, err
}
func (s *Service) Delete(ctx context.Context, actor, id string) error {
	if !uuidName.MatchString(id) {
		return ErrInvalid
	}
	if err := s.gate(ctx); err != nil {
		return err
	}
	f, err := s.repo.BeginDelete(ctx, actor, id)
	if err != nil {
		return err
	}
	if err = s.store.Delete(ctx, f.Key); err != nil {
		_ = s.repo.DeleteFailed(ctx, f)
		return ErrNotReady
	}
	return s.repo.Deleted(ctx, actor, f)
}

func readEntries(root *os.Root, path string) ([]fs.DirEntry, error) {
	f, err := root.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return f.ReadDir(-1)
}
