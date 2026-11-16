package main

import (
	"archive/tar"
	"compress/gzip"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMaterializeTemplateRewritesProjectAndOmitsPrivateFiles(t *testing.T) {
	source := t.TempDir()
	mustWrite(t, filepath.Join(source, "backend", "go.mod"), "module goadminkit/backend\n")
	mustWrite(t, filepath.Join(source, "backend", "internal", "x.go"), "package x\n// goadminkit/backend\n")
	mustWrite(t, filepath.Join(source, "frontend", "package.json"), `{"name":"goadmin-kit-frontend","private":true}`+"\n")
	mustWrite(t, filepath.Join(source, "frontend", "package-lock.json"), `{"name":"goadmin-kit-frontend","packages":{"":{"name":"goadmin-kit-frontend"}}}`+"\n")
	mustWrite(t, filepath.Join(source, "frontend", "app", "page.tsx"), "export const x = 'GoAdmin Kit'\n")
	mustWrite(t, filepath.Join(source, ".env.example"), "APP_NAME=GoAdmin Kit\nPOSTGRES_PASSWORD=\nCSRF_SECRET=\nSETTINGS_ENCRYPTION_KEY=\nPLUGIN_CONFIG_ENCRYPTION_KEY=\n")
	mustWrite(t, filepath.Join(source, "DEVELOPMENT_SPEC.md"), "private\n")
	mustWrite(t, filepath.Join(source, "docs", "ARCHITECTURE.md"), "public\n")
	mustWrite(t, filepath.Join(source, "docs", "PHASE1_ACCEPTANCE.md"), "private\n")

	destination := t.TempDir()
	if err := materializeTemplate(source, destination, "example.com/demo", "Demo Admin"); err != nil {
		t.Fatal(err)
	}
	backend, err := os.ReadFile(filepath.Join(destination, "backend", "go.mod"))
	if err != nil || !strings.Contains(string(backend), "module example.com/demo/backend") {
		t.Fatalf("backend module was not rewritten: %s %v", backend, err)
	}
	frontend, err := os.ReadFile(filepath.Join(destination, "frontend", "package.json"))
	if err != nil || !strings.Contains(string(frontend), `"name": "demo-admin-frontend"`) {
		t.Fatalf("frontend package was not rewritten: %s %v", frontend, err)
	}
	page, err := os.ReadFile(filepath.Join(destination, "frontend", "app", "page.tsx"))
	if err != nil || strings.Contains(string(page), "GoAdmin Kit") || !strings.Contains(string(page), "Demo Admin") {
		t.Fatalf("frontend branding was not rewritten: %s %v", page, err)
	}
	if data, err := os.ReadFile(filepath.Join(destination, "docs", "ARCHITECTURE.md")); err != nil || string(data) != "public\n" {
		t.Fatal("public docs missing", err)
	}
	for _, private := range []string{"DEVELOPMENT_SPEC.md", filepath.Join("docs", "PHASE1_ACCEPTANCE.md")} {
		if _, err := os.Stat(filepath.Join(destination, private)); !os.IsNotExist(err) {
			t.Fatalf("private file copied: %s", private)
		}
	}
	env, err := os.ReadFile(filepath.Join(destination, ".env"))
	if err != nil || strings.Contains(string(env), "CSRF_SECRET=\n") {
		t.Fatalf("generated env has missing secret: %v", err)
	}
}

func TestNewRejectsExistingTarget(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "existing")
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
	if err := ensureTargetAvailable(path); err == nil {
		t.Fatal("existing target was accepted")
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func fixture(t *testing.T) string {
	t.Helper()
	source := t.TempDir()
	mustWrite(t, filepath.Join(source, "VERSION"), "0.1.0\n")
	mustWrite(t, filepath.Join(source, "backend", "go.mod"), "module goadminkit/backend\n\ngo 1.26.0\n")
	mustWrite(t, filepath.Join(source, "backend", "main.go"), "package main\nimport _ \"goadminkit/backend/internal/example\"\n")
	mustWrite(t, filepath.Join(source, "frontend", "package.json"), `{"name":"original","private":true}`)
	mustWrite(t, filepath.Join(source, "frontend", "package-lock.json"), `{"name":"original","packages":{"":{"name":"original"}}}`)
	mustWrite(t, filepath.Join(source, "frontend", "app", "page.tsx"), "export const title='GoAdmin Kit';")
	mustWrite(t, filepath.Join(source, ".env.example"), "APP_NAME=GoAdmin Kit\nPOSTGRES_HOST=127.0.0.1\nPOSTGRES_PORT=5432\nPOSTGRES_DB=project\nPOSTGRES_USER=project\nPOSTGRES_PASSWORD=\nCSRF_SECRET=\nSETTINGS_ENCRYPTION_KEY=\nPLUGIN_CONFIG_ENCRYPTION_KEY=\nAPI_PORT=8080\nFRONTEND_PORT=3000\nAPI_INTERNAL_URL=http://127.0.0.1:8080\nPUBLIC_ORIGINS=http://127.0.0.1:3000\n")
	mustWrite(t, filepath.Join(source, "docker-compose.yml"), "name: goadmin-kit\nservices:\n  api:\n    image: goadmin-kit-api:local\n")
	return source
}

func TestNewCommandIsolationAndRepeat(t *testing.T) {
	source := fixture(t)
	mustWrite(t, filepath.Join(source, ".env"), "SHOULD_NEVER_COPY=secret")
	mustWrite(t, filepath.Join(source, "backend", ".env.local"), "SHOULD_NEVER_COPY=secret")
	mustWrite(t, filepath.Join(source, "frontend", "node_modules", "junk"), "junk")
	mustWrite(t, filepath.Join(source, "docs", "ARCHITECTURE.md"), "Public")
	mustWrite(t, filepath.Join(source, "docs", "PHASE_PRIVATE.md"), "Private")
	t.Chdir(t.TempDir())
	args := []string{"new-project", "--source-dir", source, "--version", "v0.1.0", "--module", "example.com/new-project", "--name", "New Project"}
	if err := commandNew(args); err != nil {
		t.Fatal(err)
	}
	if err := checkEnv("new-project/.env"); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile("new-project/.env")
	if err := commandNew(args); err == nil {
		t.Fatal("existing project overwritten")
	}
	after, _ := os.ReadFile("new-project/.env")
	if string(before) != string(after) {
		t.Fatal("repeat changed environment")
	}
	if strings.Contains(string(before), "SHOULD_NEVER_COPY") {
		t.Fatal("source secrets copied")
	}
	for _, path := range []string{"backend/.env.local", "frontend/node_modules", "docs/PHASE_PRIVATE.md", ".git", "go.mod"} {
		if _, err := os.Lstat(filepath.Join("new-project", path)); !os.IsNotExist(err) {
			t.Fatalf("unexpected generated file %s", path)
		}
	}
	values, _ := readEnv("new-project/.env")
	if values["APP_NAME"] != "New Project" {
		t.Fatal("project name missing")
	}
	for _, key := range []string{"SETTINGS_ENCRYPTION_KEY", "PLUGIN_CONFIG_ENCRYPTION_KEY"} {
		data, err := base64.StdEncoding.DecodeString(values[key])
		if err != nil || len(data) != 32 {
			t.Fatal("invalid encryption key")
		}
	}
	if values["SETTINGS_ENCRYPTION_KEY"] == values["PLUGIN_CONFIG_ENCRYPTION_KEY"] {
		t.Fatal("encryption keys reused")
	}
	if info, _ := os.Stat("new-project/.env"); info.Mode().Perm() != 0600 {
		t.Fatal("insecure env mode")
	}
	if err := commandNew([]string{"second-project", "--source-dir", source}); err != nil {
		t.Fatal(err)
	}
	second, _ := readEnv("second-project/.env")
	for _, key := range secretKeys {
		if values[key] == second[key] {
			t.Fatalf("secret reused: %s", key)
		}
	}
	compose, _ := os.ReadFile("new-project/docker-compose.yml")
	if strings.Contains(string(compose), "goadmin-kit") {
		t.Fatal("Compose namespace not independent")
	}
	if _, err := os.Stat("new-project/docs/ARCHITECTURE.md"); err != nil {
		t.Fatal(err)
	}
	if err := writeGeneratedEnv("new-project", "Changed"); err == nil {
		t.Fatal("environment overwritten")
	}
	if err := commandInit([]string{"--skip-install"}); err == nil {
		t.Fatal("init outside project succeeded")
	}
	t.Chdir("new-project")
	if err := commandInit([]string{"--skip-install"}); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(".env"); string(before) != string(data) {
		t.Fatal("init modified existing env")
	}
	var pkg map[string]any
	data, _ := os.ReadFile("frontend/package-lock.json")
	json.Unmarshal(data, &pkg)
	if pkg["name"] != "new-project-frontend" {
		t.Fatal("lockfile name mismatch")
	}
}

func TestInputAndSourceValidation(t *testing.T) {
	source := fixture(t)
	t.Chdir(t.TempDir())
	for _, args := range [][]string{{}, {"../escape"}, {"."}, {"project", "--version", "main"}, {"project", "--module", "a/../b"}, {"project", "--name", "bad'code"}, {"project", "--source-dir", source, "--version", "v9.0.0"}} {
		if err := commandNew(args); err == nil {
			t.Fatalf("accepted invalid input %v", args)
		}
	}
	if err := os.Symlink("missing", "existing-link"); err != nil {
		t.Fatal(err)
	}
	if err := commandNew([]string{"existing-link", "--source-dir", source}); err == nil {
		t.Fatal("dangling target symlink accepted")
	}
	if err := os.Symlink("/etc/passwd", filepath.Join(source, "backend", "leak")); err != nil {
		t.Fatal(err)
	}
	if err := materializeTemplate(source, t.TempDir(), "example.com/test", "Test"); err == nil {
		t.Fatal("source symlink accepted")
	}
}

func TestArchiveTraversalAndLimits(t *testing.T) {
	for _, entry := range []*tar.Header{{Name: "../escape", Mode: 0644, Typeflag: tar.TypeReg}, {Name: "/absolute", Mode: 0644, Typeflag: tar.TypeReg}, {Name: "root/link", Typeflag: tar.TypeSymlink, Linkname: "/etc/passwd"}, {Name: "root/big", Typeflag: tar.TypeReg, Size: maxExpandedBytes + 1}} {
		t.Run(entry.Name, func(t *testing.T) {
			file, err := os.CreateTemp(t.TempDir(), "*.tgz")
			if err != nil {
				t.Fatal(err)
			}
			zw := gzip.NewWriter(file)
			tw := tar.NewWriter(zw)
			if err := tw.WriteHeader(entry); err != nil {
				t.Fatal(err)
			}
			tw.Close()
			zw.Close()
			file.Close()
			if err := extractTarGz(file.Name(), t.TempDir()); err == nil {
				t.Fatal("unsafe archive accepted")
			}
		})
	}
}

func TestGitHubPAXGlobalHeader(t *testing.T) {
	archive := filepath.Join(t.TempDir(), "release.tar.gz")
	file, err := os.Create(archive)
	if err != nil {
		t.Fatal(err)
	}
	gz := gzip.NewWriter(file)
	w := tar.NewWriter(gz)
	if err := w.WriteHeader(&tar.Header{Typeflag: tar.TypeXGlobalHeader, PAXRecords: map[string]string{"comment": "GitHub source archive"}}); err != nil {
		t.Fatal(err)
	}
	content := []byte("release payload")
	if err := w.WriteHeader(&tar.Header{Name: "goadmin-v0.1.1/README.md", Typeflag: tar.TypeReg, Mode: 0o644, Size: int64(len(content))}); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := errors.Join(w.Close(), gz.Close(), file.Close()); err != nil {
		t.Fatal(err)
	}
	destination := t.TempDir()
	if err := extractTarGz(archive, destination); err != nil {
		t.Fatal(err)
	}
	actual, err := os.ReadFile(filepath.Join(destination, "goadmin-v0.1.1", "README.md"))
	if err != nil || string(actual) != string(content) {
		t.Fatalf("PAX archive entry missing or changed: %q, %v", actual, err)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestLatestReleaseAndNetworkFailure(t *testing.T) {
	old := releaseClient
	t.Cleanup(func() { releaseClient = old })
	for _, body := range []string{`{"tag_name":"main"}`, `{"tag_name":"v1.2.3","prerelease":true}`, `{"tag_name":"v1.2.3","draft":true}`, `{"tag_name":"v1.2.3-rc1"}`} {
		releaseClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}, nil
		})}
		if _, err := latestRelease(); err == nil {
			t.Fatal("unstable release accepted")
		}
	}
	releaseClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("User-Agent") == "" {
			t.Fatal("missing user-agent")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"tag_name":"v1.2.3"}`))}, nil
	})}
	if version, err := latestRelease(); err != nil || version != "v1.2.3" {
		t.Fatal(version, err)
	}
	releaseClient = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, errors.New("network unavailable") })}
	if _, err := latestRelease(); err == nil {
		t.Fatal("network error swallowed")
	}
}

func TestDoctorVersionsAndDatabaseTarget(t *testing.T) {
	for _, item := range []struct {
		name, version string
		ok            bool
	}{{"go", "go version go1.26.2 darwin/arm64", true}, {"go", "go version go1.25.0", false}, {"node", "v24.12.0", true}, {"node", "v20.12.2", false}, {"npm", "11.18.0", true}, {"npm", "9.0.0", false}} {
		if err := validateToolVersion(item.name, item.version); (err == nil) != item.ok {
			t.Fatal(item, err)
		}
	}
	t.Setenv("POSTGRES_DB", "isolated")
	if err := checkDatabaseTarget("other", map[string]string{}); err == nil {
		t.Fatal("wrong database accepted")
	}
	if err := checkDatabaseTarget("isolated", map[string]string{}); err != nil {
		t.Fatal(err)
	}
}
