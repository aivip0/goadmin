package tests

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"goadminkit/backend/internal/modules/auth"
	"goadminkit/backend/internal/modules/plugins"
)

type integrationPluginProvider struct{}

func (integrationPluginProvider) HandlePluginRoute(ctx context.Context, key, method string, _ json.RawMessage, config plugins.ConfigReader) (any, error) {
	result := map[string]string{"route": key, "method": method}
	if secret, err := config.Secret(ctx, "integration-plugin", "secret"); err == nil {
		result["secret"] = string(secret)
	}
	return result, nil
}

func TestPostgreSQLPluginLifecycleAndConfig(t *testing.T) {
	db, _ := authDatabase(t)
	ctx := context.Background()
	identity, err := auth.NewService(auth.NewRepository(db.SQL), time.Hour, 30*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	root, err := identity.Initialize(ctx, auth.InitialAdmin{Username: "plugin_root", Email: "plugin_root@example.test", Password: "isolated plugin integration password"})
	if err != nil {
		t.Fatal(err)
	}

	migrationSQL := []byte("CREATE TABLE plugin_integration_data (id integer PRIMARY KEY);")
	migrationHash := sha256.Sum256(migrationSQL)
	manifest := plugins.Manifest{
		ID: "integration-plugin", Name: "Integration Plugin", Version: "1.0.0", Description: "DB integration fixture", Author: "GoAdmin tests",
		Dependencies:    []plugins.Dependency{},
		Permissions:     []plugins.Permission{{Code: "plugin_integration_plugin:view", Name: "View integration plugin", Description: "View test plugin route"}},
		Routes:          []plugins.Route{{Key: "overview", Title: "Overview", Permission: "plugin_integration_plugin:view", View: "overview", Methods: []string{"GET"}}},
		Menus:           []plugins.Menu{{Key: "main", Name: "Integration plugin", RouteKey: "overview", Icon: "Puzzle", Permission: "plugin_integration_plugin:view", SortOrder: 900}},
		Config:          []plugins.ConfigField{{Key: "secret", Label: "Secret", Type: "string", Secret: true}},
		MigrationSHA256: map[string]string{"000001_create_data.sql": hex.EncodeToString(migrationHash[:])},
	}
	manifestBytes, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	packageDir := filepath.Join(t.TempDir(), manifest.ID, manifest.Version)
	if err = os.MkdirAll(packageDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(packageDir, "manifest.json"), manifestBytes, 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(packageDir, "manifest.sig"), []byte(base64.StdEncoding.EncodeToString(ed25519.Sign(privateKey, manifestBytes))), 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.Mkdir(filepath.Join(packageDir, "migrations"), 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(packageDir, "migrations", "000001_create_data.sql"), migrationSQL, 0600); err != nil {
		t.Fatal(err)
	}
	var encryptionKey [32]byte
	if _, err = rand.Read(encryptionKey[:]); err != nil {
		t.Fatal(err)
	}
	service, err := plugins.NewService(plugins.NewRepository(db.SQL), filepath.Dir(filepath.Dir(packageDir)), [][]byte{publicKey}, encryptionKey[:])
	if err != nil {
		t.Fatal(err)
	}
	if err = service.RegisterExtension(plugins.Extension{ID: manifest.ID, Provider: integrationPluginProvider{}}); err != nil {
		t.Fatal(err)
	}
	packages, err := service.Packages(ctx)
	if err != nil || len(packages) != 1 || packages[0].Manifest.ID != manifest.ID {
		t.Fatalf("trusted package discovery: %#v, %v", packages, err)
	}
	if err = service.Install(ctx, root.ID, manifest.ID, manifest.Version); err != nil {
		t.Fatalf("install: %v", err)
	}
	var pluginTableCount, pluginMigrationCount int
	if err = db.SQL.QueryRowContext(ctx, `SELECT count(*) FROM information_schema.tables WHERE table_schema=current_schema() AND table_name='plugin_integration_data'`).Scan(&pluginTableCount); err != nil || pluginTableCount != 1 {
		t.Fatalf("plugin migration table: %d, %v", pluginTableCount, err)
	}
	if err = db.SQL.QueryRowContext(ctx, `SELECT count(*) FROM admin_plugin_migrations WHERE plugin_id=$1`, manifest.ID).Scan(&pluginMigrationCount); err != nil || pluginMigrationCount != 1 {
		t.Fatalf("plugin migration history: %d, %v", pluginMigrationCount, err)
	}
	if err = service.Install(ctx, root.ID, manifest.ID, manifest.Version); err == nil {
		t.Fatal("duplicate installation unexpectedly succeeded")
	}
	if err = service.Lifecycle(ctx, root.ID, manifest.ID, "enable"); err != nil {
		t.Fatalf("enable: %v", err)
	}
	var menuStatus string
	if err = db.SQL.QueryRowContext(ctx, `SELECT status FROM menus WHERE code='plugin_integration_plugin_main'`).Scan(&menuStatus); err != nil || menuStatus != "active" {
		t.Fatalf("plugin menu not activated: %q, %v", menuStatus, err)
	}
	value, err := service.Route(ctx, manifest.ID, "overview", "GET", nil)
	if err != nil || value.(map[string]string)["route"] != "overview" {
		t.Fatalf("registered provider route: %#v, %v", value, err)
	}
	if _, err = service.Route(ctx, manifest.ID, "overview", "POST", nil); err != plugins.ErrNotFound {
		t.Fatalf("undeclared route method accepted: %v", err)
	}
	secret := "database-only-encrypted-secret"
	if err = service.SaveConfig(ctx, root.ID, manifest.ID, map[string]json.RawMessage{"secret": json.RawMessage(`"` + secret + `"`)}); err != nil {
		t.Fatalf("save secret config: %v", err)
	}
	var cipherBytes []byte
	if err = db.SQL.QueryRowContext(ctx, `SELECT secret_ciphertext FROM admin_plugin_config WHERE plugin_id=$1 AND config_key='secret'`, manifest.ID).Scan(&cipherBytes); err != nil {
		t.Fatal(err)
	}
	if len(cipherBytes) == 0 || string(cipherBytes) == secret || string(cipherBytes) == base64.StdEncoding.EncodeToString([]byte(secret)) {
		t.Fatal("secret was not stored as ciphertext")
	}
	value, err = service.Route(ctx, manifest.ID, "overview", "GET", nil)
	if err != nil || value.(map[string]string)["secret"] != secret {
		t.Fatalf("secret config roundtrip through extension interface: %#v, %v", value, err)
	}
	if err = service.Lifecycle(ctx, root.ID, manifest.ID, "disable"); err != nil {
		t.Fatalf("disable: %v", err)
	}
	if _, err = service.Route(ctx, manifest.ID, "overview", "GET", nil); err != plugins.ErrNotFound {
		t.Fatalf("disabled route remained available: %v", err)
	}
	if err = db.SQL.QueryRowContext(ctx, `SELECT status FROM menus WHERE code='plugin_integration_plugin_main'`).Scan(&menuStatus); err != nil || menuStatus != "disabled" {
		t.Fatalf("plugin menu not disabled: %q, %v", menuStatus, err)
	}
	events, err := service.Events(ctx, manifest.ID, 20)
	if err != nil || len(events) < 4 {
		t.Fatalf("lifecycle/config events missing: %d, %v", len(events), err)
	}
	var storedHash []byte
	if err = db.SQL.QueryRowContext(ctx, `SELECT manifest_sha256 FROM admin_plugins WHERE plugin_id=$1`, manifest.ID).Scan(&storedHash); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(manifestBytes)
	if hex.EncodeToString(digest[:]) != hex.EncodeToString(storedHash) {
		t.Fatal("persisted signed manifest hash mismatch")
	}
}
