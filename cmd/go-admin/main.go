package main

import (
	"archive/tar"
	"bufio"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	defaultRepo      = "aivip0/goadmin"
	cliVersion       = "0.1.2"
	maxArchiveBytes  = 64 << 20
	maxExpandedBytes = 128 << 20
)

var (
	projectNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
	modulePattern      = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9./_-]*$`)
	semverPattern      = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?$`)
	displayNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9 ._-]{0,99}$`)
	secretKeys         = []string{"POSTGRES_PASSWORD", "CSRF_SECRET", "SETTINGS_ENCRYPTION_KEY", "PLUGIN_CONFIG_ENCRYPTION_KEY"}
	releaseClient      = &http.Client{Timeout: 90 * time.Second}
)

func main() {
	if len(os.Args) == 1 {
		usage(os.Stdout)
		return
	}
	if len(os.Args) == 3 && (os.Args[2] == "--help" || os.Args[2] == "-h") {
		usage(os.Stdout)
		return
	}
	switch os.Args[1] {
	case "new":
		if err := commandNew(os.Args[2:]); err != nil {
			fatal(err)
		}
	case "doctor":
		if err := commandDoctor(os.Args[2:]); err != nil {
			fatal(err)
		}
	case "init":
		if err := commandInit(os.Args[2:]); err != nil {
			fatal(err)
		}
	case "version", "--version", "-v":
		fmt.Printf("go-admin %s (template %s)\n", cliVersion, templateVersion())
	case "help", "--help", "-h":
		usage(os.Stdout)
	default:
		usage(os.Stderr)
		os.Exit(2)
	}
}

func usage(w io.Writer) {
	fmt.Fprintln(w, "go-admin - GoAdmin Kit project generator")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Usage:")
	fmt.Fprintln(w, "  go-admin new <directory> [--version vX.Y.Z] [--module module/path]")
	fmt.Fprintln(w, "  go-admin doctor")
	fmt.Fprintln(w, "  go-admin init")
	fmt.Fprintln(w, "  go-admin init --database <exact-name> [--skip-install]")
	fmt.Fprintln(w, "  go-admin version")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "new uses the latest stable GitHub Release by default. Use --source-dir for local template testing.")
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "go-admin:", err)
	os.Exit(1)
}

func commandNew(args []string) error {
	if len(args) == 1 && (args[0] == "--help" || args[0] == "-h") {
		usage(os.Stdout)
		return nil
	}
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		return errors.New("new requires a target directory")
	}
	fs := flag.NewFlagSet("new", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	version := fs.String("version", "", "exact template release")
	module := fs.String("module", "", "Go module path")
	displayName := fs.String("name", "", "display name")
	sourceDir := fs.String("source-dir", "", "local template directory for offline generation")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}
	name := filepath.Clean(args[0])
	if filepath.Base(name) != name || !projectNamePattern.MatchString(name) || name == "." || name == ".." {
		return fmt.Errorf("invalid project directory %q; use one path segment containing letters, numbers, '.', '_' or '-'", args[0])
	}
	if *version != "" && !semverPattern.MatchString(*version) {
		return fmt.Errorf("version must be an exact release tag such as v0.1.0")
	}
	if *module != "" && !validModule(*module) {
		return fmt.Errorf("invalid Go module path %q", *module)
	}
	if *displayName == "" {
		*displayName = name
	}
	if !displayNamePattern.MatchString(*displayName) {
		return errors.New("display name must contain 1–100 letters, numbers, spaces, '.', '_' or '-'")
	}
	if *module == "" {
		*module = "example.com/" + name
	}
	target, err := filepath.Abs(name)
	if err != nil {
		return err
	}
	if err := ensureTargetAvailable(target); err != nil {
		return err
	}

	source, releaseVersion, cleanup, err := resolveSource(*sourceDir, *version)
	if err != nil {
		return err
	}
	defer cleanup()

	stage, err := os.MkdirTemp(filepath.Dir(target), ".go-admin-stage-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	if err := materializeTemplate(source, stage, *module, *displayName); err != nil {
		return err
	}
	if err := os.Mkdir(target, 0o755); err != nil {
		if os.IsExist(err) {
			return fmt.Errorf("target %s appeared during generation; refusing to overwrite it", target)
		}
		return err
	}
	if err := copyTree(stage, target); err != nil {
		return fmt.Errorf("copy generated project: %w", err)
	}
	if err := os.WriteFile(filepath.Join(target, ".go-admin-template"), []byte(releaseVersion+"\n"), 0o644); err != nil {
		return err
	}
	fmt.Printf("Created %s from GoAdmin Kit %s\n", target, releaseVersion)
	fmt.Printf("Next steps:\n  cd %s\n  go-admin doctor\n  go-admin init\n", target)
	return nil
}

func commandDoctor(args []string) error {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("doctor does not accept positional arguments")
	}
	root, err := findProjectRoot()
	if err != nil {
		return err
	}
	checks := []struct {
		name string
		fn   func() error
	}{
		{"Go toolchain", requireCommand("go", "version")},
		{"Node.js", requireCommand("node", "--version")},
		{"npm", requireCommand("npm", "--version")},
		{"backend module", func() error { return requireFile(filepath.Join(root, "backend", "go.mod")) }},
		{"frontend package", func() error { return requireFile(filepath.Join(root, "frontend", "package.json")) }},
		{".env", func() error { return requireFile(filepath.Join(root, ".env")) }},
		{"environment configuration", func() error { return checkEnv(filepath.Join(root, ".env")) }},
	}
	failed := false
	for _, check := range checks {
		if err := check.fn(); err != nil {
			failed = true
			fmt.Printf("FAIL %-24s %v\n", check.name, err)
			continue
		}
		fmt.Printf("OK   %s\n", check.name)
	}
	if failed {
		return errors.New("one or more doctor checks failed")
	}
	fmt.Println("Project is ready for local development. Database migrations require an explicitly selected target.")
	return nil
}

func commandInit(args []string) error {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	database := fs.String("database", "", "explicit database name to migrate (opt-in)")
	skipInstall := fs.Bool("skip-install", false, "skip dependency installation")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("init does not accept positional arguments")
	}
	root, err := findProjectRoot()
	if err != nil {
		return err
	}
	envPath := filepath.Join(root, ".env")
	if _, err := os.Stat(envPath); os.IsNotExist(err) {
		if err := writeGeneratedEnv(root, "GoAdmin Kit"); err != nil {
			return err
		}
		fmt.Println("Created .env with fresh local secrets")
	} else if err != nil {
		return err
	} else {
		fmt.Println("Keeping existing .env")
	}
	if err := checkEnv(envPath); err != nil {
		return err
	}
	values, err := readEnv(envPath)
	if err != nil {
		return err
	}
	if *database != "" {
		if err := checkDatabaseTarget(*database, values); err != nil {
			return err
		}
	}
	if !*skipInstall {
		if err := runCommand(filepath.Join(root, "backend"), "go", "mod", "download"); err != nil {
			return fmt.Errorf("backend dependencies: %w", err)
		}
		if err := runCommand(filepath.Join(root, "frontend"), "npm", "ci"); err != nil {
			return fmt.Errorf("frontend dependencies: %w", err)
		}
	}
	if *database != "" {
		fmt.Printf("Migration target: host=%s port=%s database=%s\n", effectiveEnv(values, "POSTGRES_HOST"), effectiveEnv(values, "POSTGRES_PORT"), *database)
		if err := runCommand(filepath.Join(root, "backend"), "go", "run", "./cmd/migrate", "up"); err != nil {
			return fmt.Errorf("migration refused or failed: %w", err)
		}
		return nil
	}
	fmt.Println("Dependencies initialized. No database was contacted; run migrations only with an explicitly selected target.")
	return nil
}

func resolveSource(sourceDir, requestedVersion string) (string, string, func(), error) {
	if sourceDir != "" {
		abs, err := filepath.Abs(sourceDir)
		if err != nil {
			return "", "", func() {}, err
		}
		if err := requireFile(filepath.Join(abs, "backend", "go.mod")); err != nil {
			return "", "", func() {}, fmt.Errorf("source-dir: %w", err)
		}
		version := requestedVersion
		actual := readVersion(abs)
		if actual != "" && !strings.HasPrefix(actual, "v") {
			actual = "v" + actual
		}
		if version != "" && actual != version {
			return "", "", func() {}, errors.New("source VERSION does not match --version")
		}
		if version == "" {
			version = actual
		}
		if version == "" {
			version = "local"
		}
		return abs, version, func() {}, nil
	}
	version := requestedVersion
	if version == "" {
		var err error
		version, err = latestRelease()
		if err != nil {
			return "", "", func() {}, err
		}
	}
	archivePath, err := downloadRelease(version)
	if err != nil {
		return "", "", func() {}, err
	}
	stage, err := os.MkdirTemp("", "go-admin-release-")
	if err != nil {
		os.Remove(archivePath)
		return "", "", func() {}, err
	}
	if err := extractTarGz(archivePath, stage); err != nil {
		os.Remove(archivePath)
		os.RemoveAll(stage)
		return "", "", func() {}, err
	}
	os.Remove(archivePath)
	entries, err := os.ReadDir(stage)
	if err != nil || len(entries) != 1 || !entries[0].IsDir() {
		os.RemoveAll(stage)
		return "", "", func() {}, errors.New("release archive has an unexpected top-level layout")
	}
	root := filepath.Join(stage, entries[0].Name())
	if "v"+strings.TrimPrefix(readVersion(root), "v") != version {
		os.RemoveAll(stage)
		return "", "", func() {}, errors.New("release VERSION does not match requested tag")
	}
	return root, version, func() { os.RemoveAll(stage) }, nil
}

func latestRelease() (string, error) {
	resp, err := releaseGET("https://github.com/" + defaultRepo + "/releases/latest")
	if err != nil {
		return "", fmt.Errorf("find latest stable release: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("find latest stable release: GitHub returned %s", resp.Status)
	}
	if resp.Request == nil || resp.Request.URL == nil {
		return "", errors.New("latest release response has no final URL")
	}
	finalURL := resp.Request.URL
	prefix := "/" + defaultRepo + "/releases/tag/"
	if finalURL.Scheme != "https" || finalURL.Host != "github.com" || !strings.HasPrefix(finalURL.Path, prefix) {
		return "", errors.New("GitHub latest release resolved outside the expected repository tag page")
	}
	tag := strings.TrimPrefix(finalURL.Path, prefix)
	if strings.Contains(tag, "/") || !semverPattern.MatchString(tag) || strings.Contains(strings.Split(tag, "+")[0], "-") {
		return "", fmt.Errorf("GitHub latest release has invalid tag %q", tag)
	}
	return tag, nil
}

func downloadRelease(version string) (string, error) {
	if !semverPattern.MatchString(version) {
		return "", fmt.Errorf("version must be an exact release tag such as v0.1.0")
	}
	resp, err := releaseGET("https://github.com/" + defaultRepo + "/archive/refs/tags/" + version + ".tar.gz")
	if err != nil {
		return "", fmt.Errorf("download release %s: %w", version, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download release %s: GitHub returned %s", version, resp.Status)
	}
	file, err := os.CreateTemp("", "go-admin-release-*.tar.gz")
	if err != nil {
		return "", err
	}
	name := file.Name()
	written, copyErr := io.Copy(file, io.LimitReader(resp.Body, maxArchiveBytes+1))
	if copyErr != nil || written > maxArchiveBytes {
		file.Close()
		os.Remove(name)
		return "", errors.New("release download failed or exceeded 64 MiB")
	}
	if err := file.Close(); err != nil {
		os.Remove(name)
		return "", err
	}
	return name, nil
}

func extractTarGz(archivePath, destination string) error {
	file, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer file.Close()
	gzipReader, err := newGzipReader(file)
	if err != nil {
		return err
	}
	defer gzipReader.Close()
	reader := tar.NewReader(gzipReader)
	var expanded int64
	entries := 0
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		entries++
		if entries > 10000 || header.Size < 0 || header.Size > maxExpandedBytes-expanded {
			return errors.New("release archive exceeds extraction limits")
		}
		expanded += header.Size
		if header.Name == "" || strings.Contains(header.Name, "\\") || filepath.IsAbs(header.Name) || strings.Contains(filepath.ToSlash(header.Name), "../") {
			return fmt.Errorf("unsafe archive path %q", header.Name)
		}
		destinationPath := filepath.Join(destination, filepath.FromSlash(header.Name))
		if !pathWithin(destination, destinationPath) {
			return fmt.Errorf("archive path escapes destination: %q", header.Name)
		}
		switch header.Typeflag {
		case tar.TypeXGlobalHeader:
			// GitHub source archives include a PAX global metadata header. It
			// describes following entries but is not a filesystem object.
			continue
		case tar.TypeDir:
			if err := os.MkdirAll(destinationPath, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(destinationPath), 0o755); err != nil {
				return err
			}
			file, err := os.OpenFile(destinationPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644|os.FileMode(header.Mode)&0o111)
			if err != nil {
				return err
			}
			if _, err := io.CopyN(file, reader, header.Size); err != nil {
				file.Close()
				return err
			}
			if err := file.Close(); err != nil {
				return err
			}
		default:
			return fmt.Errorf("release archive contains unsupported entry %q", header.Name)
		}
	}
}

func materializeTemplate(source, destination, module, displayName string) error {
	allowed := map[string]bool{
		"backend": true, "frontend": true, "deploy": true, "scripts": true, "docs": true,
		".dockerignore": true, ".env.example": true, ".gitignore": true, "docker-compose.yml": true,
		"Makefile": true, "README.md": true, "LICENSE": true, "THIRD_PARTY_NOTICES.md": true, "CHANGELOG.md": true, "VERSION": true,
	}
	entries, err := os.ReadDir(source)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !allowed[entry.Name()] {
			continue
		}
		if entry.Name() == "docs" {
			if err := copyPublicDocs(filepath.Join(source, entry.Name()), filepath.Join(destination, entry.Name())); err != nil {
				return err
			}
			continue
		}
		if err := copyFiltered(filepath.Join(source, entry.Name()), filepath.Join(destination, entry.Name()), nil); err != nil {
			return err
		}
	}
	if err := rewriteBackend(filepath.Join(destination, "backend", "go.mod"), module+"/backend"); err != nil {
		return err
	}
	if err := rewriteFrontend(filepath.Join(destination, "frontend"), displayName); err != nil {
		return err
	}
	if err := writeGeneratedEnv(destination, displayName); err != nil {
		return err
	}
	readmePath := filepath.Join(destination, "README.md")
	if data, err := os.ReadFile(readmePath); err == nil {
		if err := os.WriteFile(readmePath, []byte(strings.ReplaceAll(string(data), "go run ./cmd/go-admin", "go-admin")), 0o644); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	// Compose namespaces and image names must not collide with the source project.
	composePath := filepath.Join(destination, "docker-compose.yml")
	if data, err := os.ReadFile(composePath); err == nil {
		projectID := strings.TrimSuffix(packageName(displayName), "-frontend")
		if err := os.WriteFile(composePath, []byte(strings.ReplaceAll(string(data), "goadmin-kit", projectID)), 0o644); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	return nil
}

func copyPublicDocs(source, destination string) error {
	allowed := map[string]bool{"ARCHITECTURE.md": true, "DATABASE.md": true, "DEVELOPMENT.md": true, "DEPLOYMENT.md": true, "API.md": true, "MODULE_DEVELOPMENT.md": true, "INSTALL.md": true, "QUICKSTART.md": true}
	if err := os.MkdirAll(destination, 0o755); err != nil {
		return err
	}
	for name := range allowed {
		path := filepath.Join(source, name)
		if _, err := os.Lstat(path); os.IsNotExist(err) {
			continue
		} else if err != nil {
			return err
		}
		if err := copyFiltered(path, filepath.Join(destination, name), nil); err != nil {
			return err
		}
	}
	return nil
}

func copyFiltered(source, destination string, include func(string, os.FileInfo) bool) error {
	info, err := os.Lstat(source)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("refusing symlink in template: %s", source)
	}
	if info.IsDir() {
		if err := os.MkdirAll(destination, 0o755); err != nil {
			return err
		}
		entries, err := os.ReadDir(source)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if ignoredTemplateEntry(entry.Name()) {
				continue
			}
			rel := entry.Name()
			if include != nil {
				childInfo, err := entry.Info()
				if err != nil {
					return err
				}
				if !include(rel, childInfo) {
					continue
				}
			}
			if err := copyFiltered(filepath.Join(source, entry.Name()), filepath.Join(destination, entry.Name()), include); err != nil {
				return err
			}
		}
		return nil
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("refusing special file: %s", source)
	}
	data, err := os.ReadFile(source)
	if err != nil {
		return err
	}
	return os.WriteFile(destination, data, info.Mode().Perm())
}

func ignoredTemplateEntry(name string) bool {
	if strings.HasPrefix(name, ".env") && name != ".env.example" {
		return true
	}
	if strings.HasSuffix(name, ".log") || strings.HasSuffix(name, ".tsbuildinfo") || strings.HasSuffix(name, ".pem") || strings.HasSuffix(name, ".key") {
		return true
	}
	switch name {
	case ".git", ".codex", ".agents", ".idea", "AGENTS.md", "node_modules", ".next", "out", "dist", "coverage", "test-results", "playwright-report", "tsconfig.tsbuildinfo", "bin", "vendor":
		return true
	default:
		return false
	}
}

func rewriteBackend(path, module string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("backend module: %w", err)
	}
	match := regexp.MustCompile(`(?m)^module ([^\s]+)`).FindSubmatch(data)
	if len(match) != 2 {
		return errors.New("backend go.mod has no module declaration")
	}
	oldModule := string(match[1])
	text := strings.Replace(string(data), "module "+oldModule, "module "+module, 1)
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		return err
	}
	return filepath.Walk(filepath.Dir(path), func(file string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || filepath.Ext(file) != ".go" {
			return err
		}
		data, err := os.ReadFile(file)
		if err != nil {
			return err
		}
		text := strings.ReplaceAll(string(data), oldModule, module)
		return os.WriteFile(file, []byte(text), info.Mode().Perm())
	})
}

func rewriteFrontend(root, displayName string) error {
	packagePath := filepath.Join(root, "package.json")
	data, err := os.ReadFile(packagePath)
	if err != nil {
		return err
	}
	var manifest map[string]any
	if err := json.Unmarshal(data, &manifest); err != nil {
		return err
	}
	manifest["name"] = packageName(displayName)
	manifest["private"] = true
	data, err = json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := os.WriteFile(packagePath, data, 0o644); err != nil {
		return err
	}
	lockPath := filepath.Join(root, "package-lock.json")
	if data, err := os.ReadFile(lockPath); err == nil {
		var lock map[string]any
		if err := json.Unmarshal(data, &lock); err != nil {
			return err
		}
		lock["name"] = packageName(displayName)
		if packages, ok := lock["packages"].(map[string]any); ok {
			if rootPackage, ok := packages[""].(map[string]any); ok {
				rootPackage["name"] = packageName(displayName)
			}
		}
		data, err = json.MarshalIndent(lock, "", "  ")
		if err != nil {
			return err
		}
		if err := os.WriteFile(lockPath, append(data, '\n'), 0o644); err != nil {
			return err
		}
	}
	for _, rel := range []string{"app/layout.tsx", "app/login/page.tsx", "app/page.tsx", "components/admin-shell.tsx", "components/locale-provider.tsx"} {
		path := filepath.Join(root, rel)
		data, err := os.ReadFile(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		if err := os.WriteFile(path, []byte(strings.ReplaceAll(string(data), "GoAdmin Kit", displayName)), 0o644); err != nil {
			return err
		}
	}
	return nil
}

func writeGeneratedEnv(root, displayName string) error {
	example, err := os.ReadFile(filepath.Join(root, ".env.example"))
	if err != nil {
		return err
	}
	values := map[string]string{
		"APP_NAME":                     displayName,
		"POSTGRES_DB":                  databaseName(displayName),
		"POSTGRES_USER":                databaseName(displayName),
		"POSTGRES_PASSWORD":            randomSecret(24),
		"CSRF_SECRET":                  randomSecret(32),
		"SETTINGS_ENCRYPTION_KEY":      base64.StdEncoding.EncodeToString(randomBytes(32)),
		"PLUGIN_CONFIG_ENCRYPTION_KEY": base64.StdEncoding.EncodeToString(randomBytes(32)),
	}
	lines := strings.Split(string(example), "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		for key, value := range values {
			if strings.HasPrefix(trimmed, key+"=") {
				lines[i] = key + "=" + value
			}
		}
	}
	file, err := os.OpenFile(filepath.Join(root, ".env"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	_, writeErr := file.WriteString(strings.Join(lines, "\n"))
	closeErr := file.Close()
	return errors.Join(writeErr, closeErr)
}

func randomBytes(size int) []byte {
	data := make([]byte, size)
	if _, err := rand.Read(data); err != nil {
		panic(err)
	}
	return data
}

func randomSecret(size int) string { return base64.RawURLEncoding.EncodeToString(randomBytes(size)) }

func packageName(name string) string {
	name = strings.ToLower(name)
	name = regexp.MustCompile(`[^a-z0-9]+`).ReplaceAllString(name, "-")
	return strings.Trim(name, "-") + "-frontend"
}

func databaseName(name string) string {
	name = strings.TrimSuffix(packageName(name), "-frontend")
	name = strings.ReplaceAll(name, "-", "_")
	if len(name) > 63 {
		name = name[:63]
	}
	return name
}

func copyTree(source, destination string) error {
	if err := copyFiltered(source, destination, nil); err != nil {
		return err
	}
	// Only the generator-owned staging environment can be copied here. Source
	// environment files are excluded by materializeTemplate before this call.
	return copyFiltered(filepath.Join(source, ".env"), filepath.Join(destination, ".env"), nil)
}

func pathWithin(root, target string) bool {
	rel, err := filepath.Rel(root, target)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator))
}

func findProjectRoot() (string, error) {
	current, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(current, "backend", "go.mod")); err == nil {
			return current, nil
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", errors.New("not inside a GoAdmin Kit project")
		}
		current = parent
	}
}

func requireCommand(name string, versionArgs ...string) func() error {
	return func() error {
		path, err := exec.LookPath(name)
		if err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, path, versionArgs...)
		output, err := cmd.CombinedOutput()
		if err != nil {
			return err
		}
		if err := validateToolVersion(name, string(output)); err != nil {
			return err
		}
		fmt.Printf("     %-24s %s\n", name, strings.TrimSpace(string(output)))
		return nil
	}
}

func requireFile(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return fmt.Errorf("is a directory")
	}
	return nil
}

func checkEnv(path string) error {
	values, err := readEnv(path)
	if err != nil {
		return err
	}
	for _, key := range append([]string{"POSTGRES_HOST", "POSTGRES_PORT", "POSTGRES_USER", "POSTGRES_DB", "API_PORT", "FRONTEND_PORT", "API_INTERNAL_URL", "PUBLIC_ORIGINS"}, secretKeys...) {
		if values[key] == "" {
			return fmt.Errorf("%s is empty", key)
		}
	}
	for _, key := range []string{"POSTGRES_PORT", "API_PORT", "FRONTEND_PORT"} {
		port, err := strconv.Atoi(values[key])
		if err != nil || port < 1 || port > 65535 {
			return fmt.Errorf("%s must be a valid port", key)
		}
	}
	for _, key := range []string{"SETTINGS_ENCRYPTION_KEY", "PLUGIN_CONFIG_ENCRYPTION_KEY"} {
		decoded, err := base64.StdEncoding.DecodeString(values[key])
		if err != nil || len(decoded) != 32 {
			return fmt.Errorf("%s must encode 32 bytes", key)
		}
	}
	if len(values["CSRF_SECRET"]) < 32 {
		return errors.New("CSRF_SECRET must contain at least 32 bytes")
	}
	return nil
}

func readEnv(path string) (map[string]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	values := map[string]string{}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if ok {
			values[strings.TrimSpace(key)] = strings.Trim(strings.TrimSpace(value), "\"'")
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return values, nil
}

func runCommand(dir string, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Stdout, cmd.Stderr, cmd.Stdin = os.Stdout, os.Stderr, os.Stdin
	return cmd.Run()
}

func readVersion(root string) string {
	data, err := os.ReadFile(filepath.Join(root, "VERSION"))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func templateVersion() string {
	if data, err := os.ReadFile(".go-admin-template"); err == nil {
		return strings.TrimSpace(string(data))
	}
	if value := readVersion("."); value != "" {
		return value
	}
	return cliVersion
}

func releaseGET(url string) (*http.Response, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "go-admin/"+cliVersion)
	return releaseClient.Do(req)
}

func ensureTargetAvailable(path string) error {
	if _, err := os.Lstat(path); err == nil {
		return fmt.Errorf("target %s already exists; refusing to overwrite it", path)
	} else if !os.IsNotExist(err) {
		return err
	}
	return nil
}

func validModule(module string) bool {
	if !modulePattern.MatchString(module) || strings.Contains(module, "//") || strings.HasSuffix(module, "/") {
		return false
	}
	for _, part := range strings.Split(module, "/") {
		if part == "." || part == ".." || part == "" {
			return false
		}
	}
	return true
}

func effectiveEnv(values map[string]string, key string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return values[key]
}

func checkDatabaseTarget(database string, values map[string]string) error {
	if !regexp.MustCompile(`^[a-zA-Z0-9_][a-zA-Z0-9_-]{0,62}$`).MatchString(database) || effectiveEnv(values, "POSTGRES_DB") != database {
		return errors.New("--database must exactly match the explicitly configured POSTGRES_DB")
	}
	return nil
}

func validateToolVersion(name, output string) error {
	version := regexp.MustCompile(`[0-9]+\.[0-9]+`).FindString(output)
	var major, minor int
	if _, err := fmt.Sscanf(version, "%d.%d", &major, &minor); err != nil {
		return fmt.Errorf("cannot determine %s version", name)
	}
	if name == "go" && (major < 1 || major == 1 && minor < 26) {
		return errors.New("Go 1.26 or newer is required")
	}
	if name == "node" && major != 24 {
		return errors.New("Node.js 24 is required by frontend/package.json")
	}
	if name == "npm" && major < 10 {
		return errors.New("npm 10 or newer is required")
	}
	return nil
}
