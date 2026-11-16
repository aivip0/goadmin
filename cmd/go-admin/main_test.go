package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
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

func TestBuildUpgradePlanDetectsConflictsAndPreservesLocalFiles(t *testing.T) {
	base, target, project := t.TempDir(), t.TempDir(), t.TempDir()
	writeUpgradeFixture(t, base, map[string]string{
		"update.txt":       "old template\n",
		"local-edit.txt":   "same upstream\n",
		"local-delete.txt": "old value\n",
		"removed-upstream": "old file\n",
		"conflicting-edit": "base\n",
	})
	writeUpgradeFixture(t, target, map[string]string{
		"update.txt":        "new template\n",
		"local-edit.txt":    "same upstream\n",
		"local-delete.txt":  "new value\n",
		"conflicting-edit":  "upstream edit\n",
		"new-upstream-file": "new file\n",
	})
	writeUpgradeFixture(t, project, map[string]string{
		".go-admin-template": "v1.0.0\n",
		".env":               "APP_NAME=Private\nCSRF_SECRET=not-for-upgrade\n",
		"update.txt":         "old template\n",
		"local-edit.txt":     "user customization\n",
		"removed-upstream":   "keep user file\n",
		"conflicting-edit":   "local edit\n",
	})

	plan, err := buildUpgradePlan(project, base, target, "v1.0.0", "v1.1.0")
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.updates) != 1 || len(plan.adds) != 1 || len(plan.conflicts) != 1 {
		t.Fatalf("unexpected plan: updates=%v adds=%v conflicts=%v", upgradePaths(plan.updates), upgradePaths(plan.adds), plan.conflicts)
	}
	if !containsString(plan.preserved, "local-edit.txt") || !containsString(plan.preserved, "local-delete.txt (locally deleted)") {
		t.Fatalf("local changes were not reported as preserved: %v", plan.preserved)
	}
	if !containsString(plan.removed, "removed-upstream") {
		t.Fatalf("upstream deletion was not retained/reported: %v", plan.removed)
	}
	if err := applyUpgradePlan(project, plan); err == nil {
		t.Fatal("conflicted plan was applied")
	}
	marker, _ := os.ReadFile(filepath.Join(project, ".go-admin-template"))
	if string(marker) != "v1.0.0\n" {
		t.Fatalf("conflicted upgrade advanced marker: %q", marker)
	}
	privateEnv, _ := os.ReadFile(filepath.Join(project, ".env"))
	if string(privateEnv) != "APP_NAME=Private\nCSRF_SECRET=not-for-upgrade\n" {
		t.Fatalf("upgrade changed .env: %q", privateEnv)
	}
}

func TestApplyUpgradePlanAndRepeatAreSafe(t *testing.T) {
	base, target, project := t.TempDir(), t.TempDir(), t.TempDir()
	writeUpgradeFixture(t, base, map[string]string{
		"backend/value.go": "old\n",
		"local.txt":        "same upstream\n",
		"deleted-local":    "base\n",
		"removed-upstream": "base\n",
	})
	writeUpgradeFixture(t, target, map[string]string{
		"backend/value.go": "new\n",
		"local.txt":        "same upstream\n",
		"deleted-local":    "changed upstream\n",
		"added.txt":        "new file\n",
	})
	writeUpgradeFixture(t, project, map[string]string{
		".go-admin-template": "v1.0.0\n",
		".env":               "APP_NAME=My Admin\nCSRF_SECRET=keep-me\n",
		"backend/value.go":   "old\n",
		"local.txt":          "my local version\n",
		"removed-upstream":   "retain despite upstream deletion\n",
	})

	plan, err := buildUpgradePlan(project, base, target, "v1.0.0", "v1.1.0")
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.conflicts) != 0 {
		t.Fatalf("unexpected conflicts: %v", plan.conflicts)
	}
	if err := applyUpgradePlan(project, plan); err != nil {
		t.Fatal(err)
	}
	for path, expected := range map[string]string{
		"backend/value.go":   "new\n",
		"local.txt":          "my local version\n",
		"removed-upstream":   "retain despite upstream deletion\n",
		".go-admin-template": "v1.1.0\n",
		".env":               "APP_NAME=My Admin\nCSRF_SECRET=keep-me\n",
	} {
		data, err := os.ReadFile(filepath.Join(project, path))
		if err != nil || string(data) != expected {
			t.Fatalf("%s = %q, %v; want %q", path, data, err, expected)
		}
	}
	if _, err := os.Stat(filepath.Join(project, "deleted-local")); !os.IsNotExist(err) {
		t.Fatalf("locally deleted file was restored: %v", err)
	}
	added, err := os.ReadFile(filepath.Join(project, "added.txt"))
	if err != nil || string(added) != "new file\n" {
		t.Fatalf("new upstream file missing or wrong: %q %v", added, err)
	}

	repeat, err := buildUpgradePlan(project, target, target, "v1.1.0", "v1.1.0")
	if err != nil {
		t.Fatal(err)
	}
	if len(repeat.updates) != 0 || len(repeat.adds) != 0 || len(repeat.conflicts) != 0 {
		t.Fatalf("same-version scan was not idempotent: %+v", repeat)
	}
}

func TestUpgradeRefusesChangedFileAndSymlinkParent(t *testing.T) {
	base, target, project := t.TempDir(), t.TempDir(), t.TempDir()
	writeUpgradeFixture(t, base, map[string]string{"value.txt": "base\n"})
	writeUpgradeFixture(t, target, map[string]string{"value.txt": "upstream\n"})
	writeUpgradeFixture(t, project, map[string]string{".go-admin-template": "v1.0.0\n", "value.txt": "base\n"})
	plan, err := buildUpgradePlan(project, base, target, "v1.0.0", "v1.1.0")
	if err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(project, "value.txt"), "edited after preview\n")
	if err := applyUpgradePlan(project, plan); err == nil {
		t.Fatal("upgrade overwrote a file changed after planning")
	}
	data, _ := os.ReadFile(filepath.Join(project, "value.txt"))
	marker, _ := os.ReadFile(filepath.Join(project, ".go-admin-template"))
	if string(data) != "edited after preview\n" || string(marker) != "v1.0.0\n" {
		t.Fatalf("stale plan overwrote project state: file=%q marker=%q", data, marker)
	}

	external := t.TempDir()
	if err := os.Symlink(external, filepath.Join(project, "linked")); err != nil {
		t.Fatal(err)
	}
	if err := writeUpgradeFile(project, upgradeFile{path: "linked/created.txt", content: []byte("unsafe")}, false); err == nil {
		t.Fatal("upgrade followed a symlinked project directory")
	}
	if _, err := os.Stat(filepath.Join(external, "created.txt")); !os.IsNotExist(err) {
		t.Fatalf("write escaped through symlink: %v", err)
	}
}

func TestStableVersionComparison(t *testing.T) {
	for _, tc := range []struct {
		left, right string
		want        int
	}{{"v1.10.0", "v1.9.9", 1}, {"v1.2.0", "v1.2.1", -1}, {"v2.0.0", "v2.0.0", 0}} {
		got, err := compareStableVersions(tc.left, tc.right)
		if err != nil || got != tc.want {
			t.Fatalf("compareStableVersions(%q,%q) = %d, %v", tc.left, tc.right, got, err)
		}
	}
	for _, invalid := range []string{"main", "v1.0.0-rc.1", "1.0.0", "v1.0"} {
		if _, err := compareStableVersions(invalid, "v1.0.0"); err == nil {
			t.Fatalf("accepted unstable version %q", invalid)
		}
	}
}

func TestUpgradeCommandUsesReleaseBaselineAndKeepsProjectConfig(t *testing.T) {
	oldTemplate := fixture(t)
	mustWrite(t, filepath.Join(oldTemplate, "VERSION"), "0.1.2\n")
	newTemplate := fixture(t)
	mustWrite(t, filepath.Join(newTemplate, "VERSION"), "0.1.3\n")
	mustWrite(t, filepath.Join(newTemplate, "backend", "main.go"), "package main\n// upstream template update\n")
	mustWrite(t, filepath.Join(newTemplate, "backend", "internal", "new_feature.go"), "package backend\n// new upstream file\n")

	project := t.TempDir()
	if err := materializeTemplate(oldTemplate, project, "example.com/upgrade-project", "Upgrade Project"); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(project, ".go-admin-template"), "v0.1.2\n")
	envBefore, err := os.ReadFile(filepath.Join(project, ".env"))
	if err != nil {
		t.Fatal(err)
	}
	archive := makeReleaseArchive(t, oldTemplate, "goadmin-v0.1.2")
	oldClient := releaseClient
	releaseClient = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.Path != "/aivip0/goadmin/archive/refs/tags/v0.1.2.tar.gz" {
			return nil, errors.New("unexpected release archive request: " + request.URL.String())
		}
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(archive)), Request: request}, nil
	})}
	t.Cleanup(func() { releaseClient = oldClient })
	t.Chdir(project)
	if err := commandUpgrade([]string{"--version", "v0.1.3", "--source-dir", newTemplate, "--apply"}); err != nil {
		t.Fatal(err)
	}
	updated, err := os.ReadFile(filepath.Join(project, "backend", "main.go"))
	if err != nil || !strings.Contains(string(updated), "upstream template update") {
		t.Fatalf("upstream change not installed: %q, %v", updated, err)
	}
	added, err := os.ReadFile(filepath.Join(project, "backend", "internal", "new_feature.go"))
	if err != nil || !strings.Contains(string(added), "new upstream file") {
		t.Fatalf("new upstream file not installed: %q, %v", added, err)
	}
	envAfter, err := os.ReadFile(filepath.Join(project, ".env"))
	if err != nil || !bytes.Equal(envBefore, envAfter) {
		t.Fatalf("upgrade changed .env: %v", err)
	}
	marker, err := os.ReadFile(filepath.Join(project, ".go-admin-template"))
	if err != nil || string(marker) != "v0.1.3\n" {
		t.Fatalf("template marker not advanced last: %q, %v", marker, err)
	}
}

func writeUpgradeFixture(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for path, content := range files {
		mustWrite(t, filepath.Join(root, filepath.FromSlash(path)), content)
	}
}

func upgradePaths(files []upgradeFile) []string {
	paths := make([]string, 0, len(files))
	for _, file := range files {
		paths = append(paths, file.path)
	}
	return paths
}

func containsString(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}

func makeReleaseArchive(t *testing.T, root, topDirectory string) []byte {
	t.Helper()
	var archive bytes.Buffer
	compressed := gzip.NewWriter(&archive)
	tarWriter := tar.NewWriter(compressed)
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == root {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		name := filepath.ToSlash(filepath.Join(topDirectory, relative))
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return tarWriter.WriteHeader(&tar.Header{Name: name + "/", Typeflag: tar.TypeDir, Mode: int64(info.Mode().Perm())})
		}
		if !info.Mode().IsRegular() {
			return errors.New("test release contains a non-regular file")
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if err := tarWriter.WriteHeader(&tar.Header{Name: name, Typeflag: tar.TypeReg, Mode: int64(info.Mode().Perm()), Size: int64(len(content))}); err != nil {
			return err
		}
		_, err = tarWriter.Write(content)
		return err
	})
	if err := errors.Join(err, tarWriter.Close(), compressed.Close()); err != nil {
		t.Fatal(err)
	}
	return archive.Bytes()
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
	for _, finalURL := range []string{
		"https://github.com/aivip0/goadmin/releases/tag/main",
		"https://github.com/aivip0/goadmin/releases/tag/v1.2.3-rc1",
		"https://github.com/other/repo/releases/tag/v1.2.3",
		"https://example.com/aivip0/goadmin/releases/tag/v1.2.3",
		"https://github.com/aivip0/goadmin/releases/tag/v1.2.3/extra",
	} {
		releaseClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			u, err := url.Parse(finalURL)
			if err != nil {
				t.Fatal(err)
			}
			request := r.Clone(r.Context())
			request.URL = u
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("release page")), Request: request}, nil
		})}
		if _, err := latestRelease(); err == nil {
			t.Fatal("unstable release accepted")
		}
	}
	releaseClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("User-Agent") == "" {
			t.Fatal("missing user-agent")
		}
		u, _ := url.Parse("https://github.com/aivip0/goadmin/releases/tag/v1.2.3")
		request := r.Clone(r.Context())
		request.URL = u
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("release page")), Request: request}, nil
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
