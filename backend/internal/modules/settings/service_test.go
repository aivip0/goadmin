package settings

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"
)

type memoryRepository struct{ data map[string]record }

func (m *memoryRepository) List(_ context.Context) ([]record, error) {
	list := []record{}
	for _, v := range m.data {
		list = append(list, v)
	}
	return list, nil
}
func (m *memoryRepository) Save(_ context.Context, _ string, entries map[string]record) error {
	for k, v := range entries {
		m.data[k] = v
	}
	return nil
}
func TestRegistryAndSecretEncryption(t *testing.T) {
	ctx := context.Background()
	repo := &memoryRepository{data: map[string]record{}}
	s := NewService(repo, nil)
	if err := s.Update(ctx, "actor", map[string]json.RawMessage{"arbitrary.secret": json.RawMessage(`"no"`)}); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
	for _, tc := range []struct{ key, raw string }{
		{"site.name", `" "`}, {"site.name", `null`}, {"site.name", `22`},
		{"localization.timezone", `"../etc/passwd"`}, {"localization.timezone", `"Invalid/Zone"`},
		{"localization.language", `"fr"`}, {"branding.logo_path", `"https://evil.test/image.png"`},
		{"branding.logo_path", `"//evil.test/image.png"`}, {"general.default_page_size", `500`},
	} {
		if err := s.Update(ctx, "actor", map[string]json.RawMessage{tc.key: json.RawMessage(tc.raw)}); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s=%s accepted: %v", tc.key, tc.raw, err)
		}
	}
	if err := s.Update(ctx, "actor", map[string]json.RawMessage{"site.name": json.RawMessage(`"Starter"`), "localization.timezone": json.RawMessage(`"Asia/Shanghai"`)}); err != nil {
		t.Fatal(err)
	}
	got, err := s.Get(ctx)
	if err != nil || got.Settings["site.name"].Value != "Starter" || got.Settings["localization.language"].Value != "en" {
		t.Fatalf("got %+v %v", got, err)
	}
	registry["test.secret"] = definition{defaultValue: "", validate: text(128, true), sensitive: true}
	defer delete(registry, "test.secret")
	if err := s.Update(ctx, "actor", map[string]json.RawMessage{"test.secret": json.RawMessage(`"top secret"`)}); !errors.Is(err, ErrKeyUnavailable) {
		t.Fatal(err)
	}
	key := bytes.Repeat([]byte{47}, 32)
	s = NewService(repo, key)
	if err := s.Update(ctx, "actor", map[string]json.RawMessage{"test.secret": json.RawMessage(`"top secret"`)}); err != nil {
		t.Fatal(err)
	}
	stored := repo.data["test.secret"]
	if !stored.sensitive || stored.public != nil || stored.version != 1 || bytes.Contains(stored.encrypted, []byte("top secret")) {
		t.Fatal("secret stored in plaintext")
	}
	got, err = s.Get(ctx)
	if err != nil || got.Settings["test.secret"].Value != nil || !got.Settings["test.secret"].Configured {
		t.Fatalf("secret leaked: %+v %v", got, err)
	}
	if _, err = NewService(repo, nil).Get(ctx); !errors.Is(err, ErrKeyUnavailable) {
		t.Fatal("missing key did not fail closed", err)
	}
}
