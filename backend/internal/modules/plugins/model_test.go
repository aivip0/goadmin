package plugins

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func validTestManifest() Manifest {
	return Manifest{
		ID: "sample-plugin", Name: "Sample", Version: "1.2.3", Dependencies: []Dependency{},
		Permissions:     []Permission{{Code: "plugin_sample_plugin:view", Name: "View", Description: "Read sample data"}},
		Routes:          []Route{{Key: "overview", Title: "Overview", Permission: "plugin_sample_plugin:view", View: "overview", Methods: []string{"GET"}}},
		Menus:           []Menu{{Key: "main", Name: "Sample", RouteKey: "overview", Icon: "Puzzle", Permission: "plugin_sample_plugin:view"}},
		Config:          []ConfigField{{Key: "api_key", Label: "API key", Type: "string", Secret: true}},
		MigrationSHA256: map[string]string{},
	}
}

func TestValidateManifestAndRejectUnsafeDeclarations(t *testing.T) {
	manifest := validTestManifest()
	if err := validateManifest(manifest); err != nil {
		t.Fatalf("valid manifest rejected: %v", err)
	}
	manifest = validTestManifest()
	manifest.Routes[0].View = "../../server-action"
	if err := validateManifest(manifest); err == nil {
		t.Fatal("unsafe route view accepted")
	}
	manifest = validTestManifest()
	manifest.Permissions[0].Code = "users:delete"
	if err := validateManifest(manifest); err == nil {
		t.Fatal("permission outside plugin namespace accepted")
	}
	manifest = validTestManifest()
	manifest.Routes[0].Methods = []string{"GET", "PATCH"}
	if err := validateManifest(manifest); err == nil {
		t.Fatal("unsupported route method accepted")
	}
	manifest = validTestManifest()
	manifest.Version = "1.0.0-01"
	if err := validateManifest(manifest); err == nil {
		t.Fatal("numeric prerelease identifier with a leading zero accepted")
	}
}

func TestSemanticVersionOrdering(t *testing.T) {
	ordered := [][2]string{
		{"1.0.0-alpha", "1.0.0-alpha.1"},
		{"1.0.0-alpha.1", "1.0.0-alpha.beta"},
		{"1.0.0-alpha.beta", "1.0.0-beta"},
		{"1.0.0-beta.2", "1.0.0-beta.11"},
		{"1.0.0-rc.1", "1.0.0"},
		{"1.0.0", "1.0.1-alpha"},
	}
	for _, pair := range ordered {
		if compareVersion(pair[0], pair[1]) >= 0 {
			t.Errorf("expected %s < %s", pair[0], pair[1])
		}
	}
	if compareVersion("1.0.0+build.1", "1.0.0+build.2") != 0 {
		t.Fatal("build metadata must not affect precedence")
	}
}

func TestSignedLocalPackageVerification(t *testing.T) {
	root := t.TempDir()
	packageDir := filepath.Join(root, "sample-plugin", "1.2.3")
	if err := os.MkdirAll(packageDir, 0700); err != nil {
		t.Fatal(err)
	}
	manifestBytes, err := json.Marshal(validTestManifest())
	if err != nil {
		t.Fatal(err)
	}
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(packageDir, "manifest.json"), manifestBytes, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(packageDir, "manifest.sig"), []byte(base64.StdEncoding.EncodeToString(ed25519.Sign(privateKey, manifestBytes))), 0600); err != nil {
		t.Fatal(err)
	}
	service, err := NewService(&Repository{}, root, [][]byte{publicKey}, nil)
	if err != nil {
		t.Fatal(err)
	}
	verified, err := service.verify("sample-plugin", "1.2.3")
	if err != nil {
		t.Fatalf("valid signed package rejected: %v", err)
	}
	if verified.Manifest.ID != "sample-plugin" || verified.KeyID == "" {
		t.Fatalf("verified package metadata missing: %#v", verified)
	}
	if _, err := service.verify("sample-plugin", "1.2.4"); err == nil {
		t.Fatal("unavailable package version accepted")
	}
	if err := os.WriteFile(filepath.Join(packageDir, "manifest.sig"), []byte(base64.StdEncoding.EncodeToString(make([]byte, ed25519.SignatureSize))), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := service.verify("sample-plugin", "1.2.3"); err == nil {
		t.Fatal("invalid signature accepted")
	}
}

func TestEncryptSecretConfiguration(t *testing.T) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	plain := []byte("sensitive-setting")
	aad := configAAD("sample-plugin", "api_key")
	ciphertext, err := encrypt(key, plain, aad)
	if err != nil {
		t.Fatal(err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		t.Fatal(err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatal(err)
	}
	got, err := gcm.Open(nil, ciphertext[:gcm.NonceSize()], ciphertext[gcm.NonceSize():], aad)
	if err != nil || string(got) != string(plain) {
		t.Fatalf("secret failed round trip: %v", err)
	}
	if _, err := gcm.Open(nil, ciphertext[:gcm.NonceSize()], ciphertext[gcm.NonceSize():], configAAD("other-plugin", "api_key")); err == nil {
		t.Fatal("ciphertext was transferable across plugin identities")
	}
	ciphertext[len(ciphertext)-1] ^= 1
	if _, err := gcm.Open(nil, ciphertext[:gcm.NonceSize()], ciphertext[gcm.NonceSize():], aad); err == nil {
		t.Fatal("tampered secret authenticated")
	}
}

type testProvider struct{}

func (testProvider) HandlePluginRoute(_ context.Context, _, _ string, _ json.RawMessage, _ ConfigReader) (any, error) {
	return map[string]string{"ok": "yes"}, nil
}

func TestExtensionRegistrationIsExplicitAndNonOverwriting(t *testing.T) {
	service, err := NewService(&Repository{}, "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	provider := testProvider{}
	if err := service.RegisterExtension(Extension{ID: "sample-plugin", Provider: provider}); err != nil {
		t.Fatal(err)
	}
	if err := service.RegisterExtension(Extension{ID: "sample-plugin", Provider: provider}); err != ErrConflict {
		t.Fatalf("duplicate extension was not rejected: %v", err)
	}
	if err := service.RegisterExtension(Extension{ID: "invalid id", Provider: provider}); err != ErrInvalid {
		t.Fatalf("invalid extension ID was not rejected: %v", err)
	}
}
