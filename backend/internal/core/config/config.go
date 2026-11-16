package config

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/go-playground/validator/v10"
	"github.com/joho/godotenv"
	"goadminkit/backend/internal/core/security"
)

type Config struct {
	Environment           string `validate:"oneof=development test production"`
	Name                  string `validate:"required"`
	LogLevel              string `validate:"oneof=debug info warn error"`
	Host                  string `validate:"required"`
	Port                  int    `validate:"min=1,max=65535"`
	DBHost                string `validate:"required"`
	DBPort                int    `validate:"min=1,max=65535"`
	DBUser                string `validate:"required"`
	DBPassword            string `validate:"required"`
	DBName                string `validate:"required"`
	DBSSLMode             string `validate:"oneof=disable require verify-ca verify-full"`
	MaxOpen               int    `validate:"min=1,max=1000"`
	MaxIdle               int    `validate:"min=0,max=1000"`
	ConnectTimeout        time.Duration
	HealthTimeout         time.Duration
	SessionIdleTTL        time.Duration
	SettingsEncryptionKey []byte
	MediaStorageRoot      string
	MediaMinFreeBytes     int64
	PluginPackageRoot     string
	PluginTrustedKeys     [][]byte
	PluginConfigKey       []byte
	Security              security.Options
}

// Load accepts the root .env from either the repository root or backend directory.
// Process environment always wins over the local development file.
func Load() (Config, error) {
	for _, path := range []string{".env", "../.env"} {
		if err := godotenv.Load(path); err == nil {
			break
		} else if !errors.Is(err, os.ErrNotExist) {
			return Config{}, fmt.Errorf("read environment file: %w", err)
		}
	}
	return Parse(os.LookupEnv)
}

func Parse(lookup func(string) (string, bool)) (Config, error) {
	get := func(key, fallback string) string {
		if value, ok := lookup(key); ok {
			return value
		}
		return fallback
	}
	c := Config{
		Environment: get("APP_ENV", "development"), Name: get("APP_NAME", "GoAdmin Kit"),
		LogLevel: get("LOG_LEVEL", "info"), Host: get("API_HOST", "127.0.0.1"),
		DBHost: get("POSTGRES_HOST", "127.0.0.1"), DBUser: get("POSTGRES_USER", "goadmin"),
		DBPassword: get("POSTGRES_PASSWORD", ""), DBName: get("POSTGRES_DB", "goadmin"), DBSSLMode: get("POSTGRES_SSLMODE", "disable"),
		MediaStorageRoot: get("MEDIA_STORAGE_ROOT", ""),
	}
	for _, item := range []struct {
		key, fallback string
		target        *int
	}{
		{"API_PORT", "8080", &c.Port}, {"POSTGRES_PORT", "15432", &c.DBPort},
		{"DB_MAX_OPEN_CONNS", "10", &c.MaxOpen}, {"DB_MAX_IDLE_CONNS", "5", &c.MaxIdle},
	} {
		value, err := strconv.Atoi(get(item.key, item.fallback))
		if err != nil {
			return c, fmt.Errorf("%s must be an integer", item.key)
		}
		*item.target = value
	}
	for _, item := range []struct {
		key, fallback string
		target        *time.Duration
	}{
		{"DB_CONNECT_TIMEOUT", "5s", &c.ConnectTimeout}, {"HEALTH_TIMEOUT", "2s", &c.HealthTimeout},
	} {
		value, err := time.ParseDuration(get(item.key, item.fallback))
		if err != nil || value < time.Millisecond || value > time.Minute {
			return c, fmt.Errorf("%s must be a duration between 1ms and 1m", item.key)
		}
		*item.target = value
	}
	if err := validator.New().Struct(c); err != nil {
		return c, errors.New("invalid configuration: check required environment variables and allowed ranges (including POSTGRES_PASSWORD)")
	}
	if c.MaxIdle > c.MaxOpen {
		return c, errors.New("DB_MAX_IDLE_CONNS cannot exceed DB_MAX_OPEN_CONNS")
	}
	if c.MediaStorageRoot != "" && (!filepath.IsAbs(c.MediaStorageRoot) || filepath.Clean(c.MediaStorageRoot) != c.MediaStorageRoot || c.MediaStorageRoot == "/") {
		return c, errors.New("MEDIA_STORAGE_ROOT must be a private absolute directory")
	}
	free, err := strconv.ParseInt(get("MEDIA_MIN_FREE_BYTES", "134217728"), 10, 64)
	if err != nil || free < 0 || free > 1<<40 {
		return c, errors.New("MEDIA_MIN_FREE_BYTES must be a nonnegative integer no greater than 1 TiB")
	}
	c.MediaMinFreeBytes = free
	if c.Environment == "production" && c.DBSSLMode == "disable" {
		return c, errors.New("production requires PostgreSQL TLS")
	}
	c.Security = security.Defaults()
	c.Security.Secure = c.Environment == "production"
	if raw, ok := lookup("COOKIE_SECURE"); ok {
		value, err := strconv.ParseBool(raw)
		if err != nil || (c.Environment == "production" && !value) {
			return c, errors.New("COOKIE_SECURE must be true in production and a boolean elsewhere")
		}
		c.Security.Secure = value
	}
	switch get("COOKIE_SAME_SITE", "lax") {
	case "lax":
		c.Security.SameSite = http.SameSiteLaxMode
	case "strict":
		c.Security.SameSite = http.SameSiteStrictMode
	default:
		return c, errors.New("COOKIE_SAME_SITE must be lax or strict")
	}
	for _, item := range []struct {
		key, fallback string
		target        *time.Duration
	}{
		{"SESSION_TTL", "12h", &c.Security.SessionTTL}, {"CSRF_TTL", "1h", &c.Security.CSRFTTL},
		{"SESSION_IDLE_TTL", "30m", &c.SessionIdleTTL},
	} {
		value, err := time.ParseDuration(get(item.key, item.fallback))
		if err != nil {
			return c, fmt.Errorf("invalid %s duration", item.key)
		}
		*item.target = value
	}
	c.Security.Secret = []byte(get("CSRF_SECRET", ""))
	if encoded := get("SETTINGS_ENCRYPTION_KEY", ""); encoded != "" {
		key, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil || len(key) != 32 {
			return c, errors.New("SETTINGS_ENCRYPTION_KEY must be base64 of 32 bytes")
		}
		c.SettingsEncryptionKey = key
	}
	c.PluginPackageRoot = get("PLUGIN_PACKAGE_ROOT", "")
	if c.PluginPackageRoot != "" && (!filepath.IsAbs(c.PluginPackageRoot) || filepath.Clean(c.PluginPackageRoot) != c.PluginPackageRoot || c.PluginPackageRoot == "/") {
		return c, errors.New("PLUGIN_PACKAGE_ROOT must be a private absolute directory")
	}
	if encoded := get("PLUGIN_CONFIG_ENCRYPTION_KEY", ""); encoded != "" {
		key, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil || len(key) != 32 {
			return c, errors.New("PLUGIN_CONFIG_ENCRYPTION_KEY must be base64 of 32 bytes")
		}
		c.PluginConfigKey = key
	}
	for _, encoded := range strings.Split(get("PLUGIN_TRUSTED_PUBLIC_KEYS", ""), ",") {
		encoded = strings.TrimSpace(encoded)
		if encoded == "" {
			continue
		}
		key, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil || len(key) != 32 {
			return c, errors.New("PLUGIN_TRUSTED_PUBLIC_KEYS must contain comma-separated base64 Ed25519 public keys")
		}
		c.PluginTrustedKeys = append(c.PluginTrustedKeys, key)
	}
	if c.Environment == "production" && len(c.Security.Secret) < 32 {
		return c, errors.New("production requires CSRF_SECRET with at least 32 bytes")
	}
	split := func(raw string) []string {
		var values []string
		for _, v := range strings.Split(raw, ",") {
			if v = strings.TrimSpace(v); v != "" {
				values = append(values, v)
			}
		}
		return values
	}
	c.Security.Origins = split(get("PUBLIC_ORIGINS", "http://127.0.0.1:3000,http://localhost:3000"))
	c.Security.TrustedProxies = split(get("TRUSTED_PROXIES", ""))
	if err := c.Security.Validate(); err != nil {
		return c, err
	}
	if c.SessionIdleTTL < time.Minute || c.SessionIdleTTL > c.Security.SessionTTL {
		return c, errors.New("SESSION_IDLE_TTL must be between 1m and SESSION_TTL")
	}
	return c, nil
}

func (c Config) Address() string { return net.JoinHostPort(c.Host, strconv.Itoa(c.Port)) }

func (c Config) DatabaseURL() string {
	u := url.URL{Scheme: "postgres", User: url.UserPassword(c.DBUser, c.DBPassword), Host: net.JoinHostPort(c.DBHost, strconv.Itoa(c.DBPort)), Path: "/" + c.DBName}
	q := u.Query()
	q.Set("sslmode", c.DBSSLMode)
	q.Set("timezone", "UTC")
	q.Set("connect_timeout", strconv.Itoa(max(1, int(c.ConnectTimeout.Seconds()))))
	u.RawQuery = q.Encode()
	return u.String()
}
