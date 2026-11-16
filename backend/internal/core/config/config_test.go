package config

import (
	"net/url"
	"strings"
	"testing"
)

func parse(values map[string]string) (Config, error) {
	return Parse(func(key string) (string, bool) { value, ok := values[key]; return value, ok })
}

func TestDefaultsAndEscaping(t *testing.T) {
	cfg, err := parse(map[string]string{"POSTGRES_PASSWORD": "secret @:/?#%"})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Address() != "127.0.0.1:8080" || cfg.DBPort != 15432 {
		t.Fatalf("unexpected defaults: %s %d", cfg.Address(), cfg.DBPort)
	}
	u, err := url.Parse(cfg.DatabaseURL())
	if err != nil {
		t.Fatal(err)
	}
	password, _ := u.User.Password()
	if password != cfg.DBPassword || u.Query().Get("timezone") != "UTC" {
		t.Fatal("DSN lost password or UTC")
	}
}

func TestInvalidConfiguration(t *testing.T) {
	for _, patch := range []map[string]string{
		{"POSTGRES_PASSWORD": ""}, {"API_PORT": "abc"}, {"API_PORT": "65536"},
		{"POSTGRES_PORT": "0"}, {"APP_ENV": "staging"}, {"LOG_LEVEL": "trace"},
		{"DB_MAX_OPEN_CONNS": "2", "DB_MAX_IDLE_CONNS": "3"},
		{"DB_CONNECT_TIMEOUT": "0s"}, {"HEALTH_TIMEOUT": "2h"},
		{"APP_ENV": "production", "POSTGRES_SSLMODE": "disable"},
		{"TRUSTED_PROXIES": "0.0.0.0/0"}, {"TRUSTED_PROXIES": "not-an-ip"},
		{"COOKIE_SAME_SITE": "none"}, {"COOKIE_SECURE": "invalid"},
		{"CSRF_SECRET": "short"}, {"PUBLIC_ORIGINS": "https://example.test/path"},
		{"CSRF_TTL": "0s"}, {"SESSION_TTL": "1000h"},
	} {
		values := map[string]string{"POSTGRES_PASSWORD": "never-print-this"}
		for key, value := range patch {
			values[key] = value
		}
		_, err := parse(values)
		if err == nil {
			t.Fatalf("accepted invalid configuration: %v", patch)
		}
		if strings.Contains(err.Error(), "never-print-this") {
			t.Fatal("error leaked secret")
		}
	}
}

func TestProductionTLS(t *testing.T) {
	_, err := parse(map[string]string{"POSTGRES_PASSWORD": "local", "APP_ENV": "production", "POSTGRES_SSLMODE": "verify-full", "PUBLIC_ORIGINS": "https://admin.example.test", "CSRF_SECRET": strings.Repeat("x", 32)})
	if err != nil {
		t.Fatal(err)
	}
}

func TestProductionSecurityRequired(t *testing.T) {
	for _, patch := range []map[string]string{{"CSRF_SECRET": ""}, {"COOKIE_SECURE": "false"}, {"PUBLIC_ORIGINS": "http://admin.example.test"}} {
		values := map[string]string{"POSTGRES_PASSWORD": "local", "APP_ENV": "production", "POSTGRES_SSLMODE": "verify-full", "PUBLIC_ORIGINS": "https://admin.example.test", "CSRF_SECRET": strings.Repeat("x", 32)}
		for k, v := range patch {
			values[k] = v
		}
		if _, err := parse(values); err == nil {
			t.Fatal("unsafe production configuration accepted")
		}
	}
}
