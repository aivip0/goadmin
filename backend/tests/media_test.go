package tests

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/joho/godotenv"
	_ "github.com/lib/pq"
	"goadminkit/backend/internal/core/config"
	"goadminkit/backend/internal/core/database"
	"goadminkit/backend/internal/modules/media"
	"goadminkit/backend/migrations"
)

func TestMediaPostgreSQLAndStorage(t *testing.T) {
	if os.Getenv("RUN_DB_TESTS") != "1" {
		t.Skip("set RUN_DB_TESTS=1 for isolated PostgreSQL integration tests")
	}
	_ = godotenv.Load("../../.env")
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	db, err := database.Open(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer db.SQL.Close()
	b := make([]byte, 8)
	if _, err = io.ReadFull(rand.Reader, b); err != nil {
		t.Fatal(err)
	}
	schema := "media_test_" + hex.EncodeToString(b)
	if _, err = db.SQL.ExecContext(ctx, `CREATE SCHEMA "`+schema+`"`); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, e := db.SQL.ExecContext(context.Background(), `DROP SCHEMA "`+schema+`" CASCADE`); e != nil {
			t.Error(e)
		}
	}()
	dsn, _ := url.Parse(cfg.DatabaseURL())
	q := dsn.Query()
	q.Set("search_path", schema)
	dsn.RawQuery = q.Encode()
	if err = migrations.Up(dsn.String()); err != nil {
		t.Fatal(err)
	}
	isolated, err := sql.Open("postgres", dsn.String())
	if err != nil {
		t.Fatal(err)
	}
	defer isolated.Close()
	repo := media.NewRepository(isolated)
	var actor, role string
	if err = isolated.QueryRowContext(ctx, `INSERT INTO admin_users(username,email,password_hash) VALUES('media_tester','media@example.test','test-hash') RETURNING id::text`).Scan(&actor); err != nil {
		t.Fatal(err)
	}
	if err = isolated.QueryRowContext(ctx, `INSERT INTO roles(code,name,is_super_admin) VALUES('super_admin','Super administrator',true) RETURNING id::text`).Scan(&role); err != nil {
		t.Fatal(err)
	}
	if _, err = isolated.ExecContext(ctx, `INSERT INTO user_roles(user_id,role_id) VALUES($1,$2)`, actor, role); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err = os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	store, err := media.OpenLocal(root)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err = media.BindRoot(ctx, repo, store, actor); err != nil {
		t.Fatal(err)
	}
	svc, err := media.NewService(ctx, repo, store)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = svc.Close() }()
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	part, err := w.CreateFormFile("file", "example.txt")
	if err != nil {
		t.Fatal(err)
	}
	content := []byte("A private example document.\n")
	if _, err = part.Write(content); err != nil {
		t.Fatal(err)
	}
	if err = w.Close(); err != nil {
		t.Fatal(err)
	}
	file, err := svc.Upload(ctx, actor, mustMultipart(t, &body, w.Boundary()))
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(content)
	if file.SHA256 != hex.EncodeToString(digest[:]) {
		t.Fatal("digest mismatch")
	}
	// A known final object must survive the same shard sweep that isolates a
	// genuinely unreferenced, aged object. Never delete unknown bytes.
	var knownKey string
	if err = isolated.QueryRowContext(ctx, `SELECT storage_key FROM media_files WHERE id=$1`, file.ID).Scan(&knownKey); err != nil {
		t.Fatal(err)
	}
	orphanKey := knownKey[:2] + strings.Repeat("e", 30)
	if orphanKey == knownKey {
		orphanKey = knownKey[:2] + strings.Repeat("d", 30)
	}
	orphanPath := filepath.Join(root, "objects", orphanKey[:2], orphanKey)
	if err = os.WriteFile(orphanPath, []byte("unknown object"), 0600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-25 * time.Hour)
	if err = os.Chtimes(orphanPath, old, old); err != nil {
		t.Fatal(err)
	}
	if err = os.Chtimes(filepath.Join(root, "objects", knownKey[:2], knownKey), old, old); err != nil {
		t.Fatal(err)
	}
	if _, err = isolated.ExecContext(ctx, `UPDATE media_storage_binding SET local_scan_cursors=jsonb_build_object('object_shard',$1::text,'object_key','')`, knownKey[:2]); err != nil {
		t.Fatal(err)
	}
	if report, e := media.Reconcile(ctx, repo, store, actor); e != nil || report.ObjectsScanned < 2 || report.OrphansIsolated != 1 {
		t.Fatalf("final orphan sweep: %+v %v", report, e)
	}
	if _, err = os.Stat(filepath.Join(root, "quarantine", "object-"+orphanKey)); err != nil {
		t.Fatalf("unknown object not quarantined: %v", err)
	}
	if _, err = os.Stat(filepath.Join(root, "objects", knownKey[:2], knownKey)); err != nil {
		t.Fatalf("known object removed: %v", err)
	}
	if err = repo.RetryOne(ctx, actor, file.ID, "failed"); err != media.ErrConflict {
		t.Fatalf("retry of ready object: %v", err)
	}
	for _, point := range []string{"staging", "pending", "partial-incoming", "published", "ready-write-failed", "ready"} {
		var unique [16]byte
		if _, err = rand.Read(unique[:]); err != nil {
			t.Fatal(err)
		}
		opID := fmt.Sprintf("%x-%x-%x-%x-%x", unique[:4], unique[4:6], unique[6:8], unique[8:10], unique[10:])
		key := hex.EncodeToString(unique[:])
		crash := exec.Command(os.Args[0], "-test.run=^TestMediaCrashChild$")
		crash.Env = append(os.Environ(), "MEDIA_CRASH_POINT="+point, "MEDIA_CRASH_DSN="+dsn.String(), "MEDIA_CRASH_ROOT="+root, "MEDIA_CRASH_ACTOR="+actor, "MEDIA_CRASH_OP="+opID, "MEDIA_CRASH_KEY="+key)
		if output, e := crash.CombinedOutput(); e == nil || !strings.Contains(e.Error(), "exit status 73") {
			t.Fatalf("crash %s did not terminate at point: %v %s", point, e, output)
		}
		if err = svc.Close(); err != nil {
			t.Fatal(err)
		}
		svc, err = media.NewService(ctx, repo, store)
		if err != nil {
			t.Fatalf("startup after crash %s: %v", point, err)
		}
		if point == "staging" {
			stage := filepath.Join(root, "staging", opID+".part")
			if err = os.Chtimes(stage, old, old); err != nil {
				t.Fatal(err)
			}
			if _, err = isolated.ExecContext(ctx, `UPDATE media_storage_binding SET local_scan_cursors=jsonb_set(local_scan_cursors,'{staging}',to_jsonb(''::text))`); err != nil {
				t.Fatal(err)
			}
		} else if point != "ready" {
			if _, err = isolated.ExecContext(ctx, `UPDATE media_files SET next_reconcile_at=clock_timestamp()-interval '1 second' WHERE upload_operation_id=$1`, opID); err != nil {
				t.Fatal(err)
			}
		}
		result, e := media.Reconcile(ctx, repo, store, actor)
		if e != nil || result.Failed != 0 {
			t.Fatalf("reconcile crash %s: %+v %v", point, result, e)
		}
		if point == "staging" {
			if _, err = os.Stat(filepath.Join(root, "quarantine", "staging-"+opID+".part")); err != nil {
				t.Fatalf("pre-pending orphan not isolated: %v", err)
			}
			continue
		}
		var state, recoveredID string
		if err = isolated.QueryRowContext(ctx, `SELECT status,id::text FROM media_files WHERE upload_operation_id=$1`, opID).Scan(&state, &recoveredID); err != nil || state != "ready" {
			t.Fatalf("crash %s not ready: %s %v", point, state, err)
		}
		if _, content, e := svc.Content(ctx, recoveredID); e != nil {
			t.Fatalf("crash %s not readable: %v", point, e)
		} else {
			actual, readErr := io.ReadAll(content)
			content.Close()
			if readErr != nil || string(actual) != "crash fixture\n" {
				t.Fatalf("crash %s wrong bytes: %v", point, readErr)
			}
		}
		if _, err = os.Stat(filepath.Join(root, "staging", opID+".part")); !os.IsNotExist(err) {
			t.Fatalf("crash %s staging remains: %v", point, err)
		}
		var count int
		if err = isolated.QueryRowContext(ctx, `SELECT count(*) FROM security_events WHERE target_id=$1 AND action IN ('media_uploaded','media_recovered')`, recoveredID).Scan(&count); err != nil || count != 1 {
			t.Fatalf("crash %s audit count %d %v", point, count, err)
		}
	}
	if container := os.Getenv("MEDIA_BACKUP_CONTAINER"); container != "" {
		verifyPairedRestore(t, cfg, dsn, schema, container, root, file.ID, content)
	}
	list, total, err := svc.List(ctx, 1, 25, "document", "")
	found := false
	for _, listed := range list {
		found = found || listed.ID == file.ID
	}
	if err != nil || total != int64(len(list)) || total != 6 || !found {
		t.Fatalf("list after five recovered crashes and one original: count=%d rows=%d found=%t err=%v", total, len(list), found, err)
	}
	_, stream, err := svc.Content(ctx, file.ID)
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(stream)
	stream.Close()
	if err != nil || !bytes.Equal(got, content) {
		t.Fatal("content differs")
	}
	if err = svc.Delete(ctx, actor, file.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err = svc.Content(ctx, file.ID); err != media.ErrNotFound {
		t.Fatalf("deleted still visible: %v", err)
	}
	var status string
	if err = isolated.QueryRowContext(ctx, `SELECT status FROM media_files WHERE id=$1`, file.ID).Scan(&status); err != nil || status != "deleted" {
		t.Fatalf("tombstone %s %v", status, err)
	}
	// Simulate a crash after pending commit but during the incoming object publication.
	op := "b79f0a41-945e-4741-ae6f-0317fdb33831"
	key := "a07337e5147617c77665d9f205092035"
	size, hash, _, err := store.Stage(ctx, op, bytes.NewReader(content))
	if err != nil {
		t.Fatal(err)
	}
	pending := media.File{OperationID: op, Key: key, OriginalName: "recovered.txt", MIME: "text/plain", Size: size, Digest: hash}
	if err = repo.Pending(ctx, actor, &pending); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(root, "objects/.incoming", op+".part"), content, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = isolated.ExecContext(ctx, `UPDATE media_files SET next_reconcile_at=clock_timestamp()-interval '1 second' WHERE id=$1`, pending.ID); err != nil {
		t.Fatal(err)
	}
	if report, e := media.Reconcile(ctx, repo, store, actor); e != nil || report.Processed != 1 || report.Failed != 0 {
		t.Fatalf("recovery: %+v %v", report, e)
	}
	if _, err = repo.Get(ctx, pending.ID); err != nil {
		t.Fatalf("recovered object invisible: %v", err)
	}
	var events int
	if err = isolated.QueryRowContext(ctx, `SELECT count(*) FROM security_events WHERE action='media_recovered' AND target_id=$1`, pending.ID).Scan(&events); err != nil || events != 1 {
		t.Fatalf("recovery audit: %d %v", events, err)
	}
	if _, err = os.Stat(filepath.Join(root, "staging", op+".part")); !os.IsNotExist(err) {
		t.Fatalf("staging remains: %v", err)
	}
	// Reopening the API rotates verification generation and rechecks every ready byte.
	if err = svc.Close(); err != nil {
		t.Fatal(err)
	}
	svc, err = media.NewService(ctx, repo, store)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = svc.Get(ctx, pending.ID); err != nil {
		t.Fatalf("file lost on restart: %v", err)
	}
	if err = os.WriteFile(filepath.Join(root, "objects", key[:2], key), []byte("damaged"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = svc.Close(); err != nil {
		t.Fatal(err)
	}
	svc, err = media.NewService(ctx, repo, store)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = svc.Get(ctx, pending.ID); err != media.ErrNotFound {
		t.Fatalf("corrupt ready object still visible: %v", err)
	}
	if err = isolated.QueryRowContext(ctx, `SELECT status FROM media_files WHERE id=$1`, pending.ID).Scan(&status); err != nil || status != "unavailable" {
		t.Fatalf("corruption status: %s %v", status, err)
	}
	m, e := migrations.New(dsn.String())
	if e != nil {
		t.Fatal(e)
	}
	if e = m.Migrate(6); e == nil {
		t.Fatal("down erased media history")
	}
	m.Close()
	if report, e := media.Reconcile(ctx, repo, store, actor); e != nil || report.Failed != 0 {
		t.Fatalf("reconcile: %+v %v", report, e)
	}
	// A known failed operation can be scheduled once by an authorized actor.
	retry := media.File{OperationID: "c79f0a41-945e-4741-ae6f-0317fdb33831", Key: "b07337e5147617c77665d9f205092035", OriginalName: "retry.txt", MIME: "text/plain", Size: size, Digest: hash}
	if err = repo.Pending(ctx, actor, &retry); err != nil {
		t.Fatal(err)
	}
	if _, err = isolated.ExecContext(ctx, `UPDATE media_files SET status='failed',reconcile_attempts=5,next_reconcile_at=NULL WHERE id=$1`, retry.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = isolated.ExecContext(ctx, `UPDATE media_files SET reconcile_claim_id=$2,reconcile_claim_until=clock_timestamp()+interval '1 minute' WHERE id=$1`, retry.ID, "a79f0a41-945e-4741-ae6f-0317fdb33831"); err != nil {
		t.Fatal(err)
	}
	if err = repo.RetryOne(ctx, actor, retry.ID, "failed"); err != media.ErrConflict {
		t.Fatalf("active claim manual retry: %v", err)
	}
	if _, err = isolated.ExecContext(ctx, `UPDATE media_files SET reconcile_claim_until=clock_timestamp()-interval '1 second' WHERE id=$1`, retry.ID); err != nil {
		t.Fatal(err)
	}
	if err = repo.RetryOne(ctx, actor, retry.ID, "failed"); err != nil {
		t.Fatal(err)
	}
	if err = repo.RetryOne(ctx, actor, retry.ID, "failed"); err != media.ErrConflict {
		t.Fatalf("repeat manual retry: %v", err)
	}
	var attempts int
	if err = isolated.QueryRowContext(ctx, `SELECT status,reconcile_attempts FROM media_files WHERE id=$1`, retry.ID).Scan(&status, &attempts); err != nil || status != "pending" || attempts != 0 {
		t.Fatalf("retry row: %s %d %v", status, attempts, err)
	}
	var claimID sql.NullString
	if err = isolated.QueryRowContext(ctx, `SELECT reconcile_claim_id::text FROM media_files WHERE id=$1`, retry.ID).Scan(&claimID); err != nil || claimID.Valid {
		t.Fatalf("expired claim not cleared: %v %v", claimID, err)
	}
	if err = isolated.QueryRowContext(ctx, `SELECT count(*) FROM security_events WHERE action='media_retry' AND target_id=$1`, retry.ID).Scan(&events); err != nil || events != 1 {
		t.Fatalf("retry audit: %d %v", events, err)
	}
}

func TestMediaCrashChild(t *testing.T) {
	point := os.Getenv("MEDIA_CRASH_POINT")
	if point == "" {
		t.Skip("only invoked by isolated integration parent")
	}
	ctx := context.Background()
	db, err := sql.Open("postgres", os.Getenv("MEDIA_CRASH_DSN"))
	if err != nil {
		os.Exit(74)
	}
	store, err := media.OpenLocal(os.Getenv("MEDIA_CRASH_ROOT"))
	if err != nil {
		os.Exit(74)
	}
	op := os.Getenv("MEDIA_CRASH_OP")
	key := os.Getenv("MEDIA_CRASH_KEY")
	actor := os.Getenv("MEDIA_CRASH_ACTOR")
	data := []byte("crash fixture\n")
	size, digest, _, err := store.Stage(ctx, op, bytes.NewReader(data))
	if err != nil {
		os.Exit(74)
	}
	if point == "staging" {
		os.Exit(73)
	}
	f := media.File{OperationID: op, Key: key, OriginalName: "crash.txt", MIME: "text/plain", Size: size, Digest: digest}
	r := media.NewRepository(db)
	if r.Pending(ctx, actor, &f) != nil {
		os.Exit(74)
	}
	if point == "pending" {
		os.Exit(73)
	}
	if point == "partial-incoming" {
		if os.WriteFile(filepath.Join(os.Getenv("MEDIA_CRASH_ROOT"), "objects/.incoming", op+".part"), data[:3], 0600) != nil {
			os.Exit(74)
		}
		os.Exit(73)
	}
	if store.Put(ctx, op, key, size, digest) != nil {
		os.Exit(74)
	}
	if point == "published" {
		os.Exit(73)
	}
	if point == "ready-write-failed" {
		if r.Ready(ctx, "00000000-0000-0000-0000-000000000000", &f) != media.ErrForbidden {
			os.Exit(74)
		}
		os.Exit(73)
	}
	if point == "ready" && r.Ready(ctx, actor, &f) == nil {
		os.Exit(73)
	}
	os.Exit(74)
}

// A paired dump and private-root copy must become readable only after startup verification.
func verifyPairedRestore(t *testing.T, cfg config.Config, source *url.URL, schema, container, root, id string, want []byte) {
	t.Helper()
	if container != "goadmin-media-test-20261113" {
		t.Fatal("backup test requires the named disposable Media PostgreSQL container")
	}
	backupDB := "phase33_restore_" + schema[len("media_test_"):]
	run := func(input []byte, args ...string) []byte {
		t.Helper()
		command := exec.Command("docker", append([]string{"exec", "-i", container}, args...)...)
		command.Stdin = bytes.NewReader(input)
		output, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("isolated backup/restore command failed: %v: %s", err, output)
		}
		return output
	}
	dump := run(nil, "pg_dump", "-U", cfg.DBUser, "-d", cfg.DBName, "-n", schema, "-Fc")
	run(nil, "createdb", "-U", cfg.DBUser, backupDB)
	t.Cleanup(func() {
		command := exec.Command("docker", "exec", container, "dropdb", "-U", cfg.DBUser, backupDB)
		if output, err := command.CombinedOutput(); err != nil {
			t.Errorf("drop isolated restore database: %v: %s", err, output)
		}
	})
	run(dump, "pg_restore", "-U", cfg.DBUser, "-d", backupDB, "--no-owner", "--no-privileges")
	restoredRoot := t.TempDir()
	if err := os.Chmod(restoredRoot, 0700); err != nil {
		t.Fatal(err)
	}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(root, path)
		if err != nil || rel == "." {
			return err
		}
		target := filepath.Join(restoredRoot, rel)
		if entry.IsDir() {
			return os.MkdirAll(target, 0700)
		}
		if !entry.Type().IsRegular() {
			return media.ErrCorrupt
		}
		src, err := os.Open(path)
		if err != nil {
			return err
		}
		dst, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			src.Close()
			return err
		}
		_, err = io.Copy(dst, src)
		return errors.Join(err, dst.Close(), src.Close())
	})
	if err != nil {
		t.Fatal(err)
	}
	restoredURL := *source
	restoredURL.Path = "/" + backupDB
	restoredDB, err := sql.Open("postgres", restoredURL.String())
	if err != nil {
		t.Fatal(err)
	}
	defer restoredDB.Close()
	restoredStore, err := media.OpenLocal(restoredRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer restoredStore.Close()
	checkCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	restored, err := media.NewService(checkCtx, media.NewRepository(restoredDB), restoredStore)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	_, stream, err := restored.Content(checkCtx, id)
	if err != nil {
		t.Fatal(err)
	}
	actual, err := io.ReadAll(stream)
	stream.Close()
	if err != nil || !bytes.Equal(actual, want) {
		t.Fatalf("restored bytes differ: %v", err)
	}
}

func mustMultipart(t *testing.T, b *bytes.Buffer, boundary string) *multipart.Reader {
	t.Helper()
	return multipart.NewReader(bytes.NewReader(b.Bytes()), boundary)
}
