package plugins

import (
	"encoding/hex"
	"errors"
	"regexp"
	"slices"
	"sort"
	"strings"
	"unicode/utf8"
)

var (
	ErrInvalid        = errors.New("invalid plugin request")
	ErrNotFound       = errors.New("plugin not found")
	ErrConflict       = errors.New("plugin conflict")
	ErrUnavailable    = errors.New("plugin operation unavailable")
	pluginIDPattern   = regexp.MustCompile(`^[a-z][a-z0-9-]{2,31}$`)
	migrationPattern  = regexp.MustCompile(`^[0-9]{6}_[a-z0-9_-]{1,40}\.sql$`)
	versionPattern    = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z.-]+)?(\+[0-9A-Za-z.-]+)?$`)
	keyPattern        = regexp.MustCompile(`^[a-z][a-z0-9_-]{1,47}$`)
	permissionPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{2,63}:[a-z][a-z0-9_]{1,31}$`)
)

type Dependency struct {
	ID      string `json:"id"`
	Version string `json:"version"`
}
type Permission struct {
	Code        string `json:"code"`
	Name        string `json:"name"`
	Description string `json:"description"`
}
type Menu struct {
	Key        string `json:"key"`
	Name       string `json:"name"`
	RouteKey   string `json:"route_key"`
	Icon       string `json:"icon"`
	SortOrder  int    `json:"sort_order"`
	Permission string `json:"permission"`
}
type Route struct {
	Key        string   `json:"key"`
	Title      string   `json:"title"`
	Permission string   `json:"permission"`
	View       string   `json:"view"`
	Methods    []string `json:"methods"`
}
type ConfigField struct {
	Key      string `json:"key"`
	Label    string `json:"label"`
	Type     string `json:"type"`
	Required bool   `json:"required"`
	Secret   bool   `json:"secret"`
}

// Manifest is signed as raw JSON. Every SQL file digest is covered by the signature.
type Manifest struct {
	ID              string            `json:"id"`
	Name            string            `json:"name"`
	Version         string            `json:"version"`
	Description     string            `json:"description"`
	Author          string            `json:"author"`
	Dependencies    []Dependency      `json:"dependencies"`
	Permissions     []Permission      `json:"permissions"`
	Menus           []Menu            `json:"menus"`
	Routes          []Route           `json:"routes"`
	Config          []ConfigField     `json:"config"`
	MigrationSHA256 map[string]string `json:"migration_sha256"`
}

type Plugin struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Version     string   `json:"version"`
	Description string   `json:"description"`
	Author      string   `json:"author"`
	Status      string   `json:"status"`
	FailureCode string   `json:"failure_code,omitempty"`
	InstalledAt string   `json:"installed_at"`
	UpdatedAt   string   `json:"updated_at"`
	Manifest    Manifest `json:"manifest"`
}
type Package struct {
	Manifest Manifest `json:"manifest"`
	Version  string   `json:"version"`
	KeyID    string   `json:"key_id"`
}
type Event struct {
	ID            string `json:"id"`
	PluginID      string `json:"plugin_id"`
	PluginVersion string `json:"plugin_version"`
	EventType     string `json:"event_type"`
	Outcome       string `json:"outcome"`
	FailureCode   string `json:"failure_code,omitempty"`
	CreatedAt     string `json:"created_at"`
}
type ConfigValue struct {
	Key        string `json:"key"`
	Label      string `json:"label"`
	Type       string `json:"type"`
	Required   bool   `json:"required"`
	Secret     bool   `json:"secret"`
	Configured bool   `json:"configured"`
	Value      any    `json:"value,omitempty"`
}

func validateManifest(m Manifest) error {
	if !pluginIDPattern.MatchString(m.ID) || !validVersion(m.Version) || strings.TrimSpace(m.Name) == "" || len(m.Name) > 120 || len(m.Description) > 1000 || len(m.Author) > 120 || !utf8.ValidString(m.Name+m.Description+m.Author) {
		return ErrInvalid
	}
	if len(m.Dependencies) > 32 || len(m.Permissions) > 100 || len(m.Menus) > 50 || len(m.Routes) > 50 || len(m.Config) > 100 || len(m.MigrationSHA256) > 100 {
		return ErrInvalid
	}
	routes := map[string]bool{}
	for _, r := range m.Routes {
		if !keyPattern.MatchString(r.Key) || routes[r.Key] || strings.TrimSpace(r.Title) == "" || len(r.Title) > 120 || r.View != "overview" && r.View != "settings" && r.View != "records" || len(r.Methods) == 0 || len(r.Methods) > 4 {
			return ErrInvalid
		}
		for _, method := range r.Methods {
			if !slices.Contains([]string{"GET", "POST", "PUT", "DELETE"}, method) {
				return ErrInvalid
			}
		}
		routes[r.Key] = true
	}
	perms := map[string]bool{}
	for _, p := range m.Permissions {
		if !permissionPattern.MatchString(p.Code) || !strings.HasPrefix(p.Code, "plugin_"+strings.ReplaceAll(m.ID, "-", "_")+":") || strings.TrimSpace(p.Name) == "" || len(p.Name) > 120 || len(p.Description) > 500 || perms[p.Code] {
			return ErrInvalid
		}
		perms[p.Code] = true
	}
	for _, r := range m.Routes {
		if !perms[r.Permission] {
			return ErrInvalid
		}
	}
	menuKeys := map[string]bool{}
	for _, v := range m.Menus {
		if !keyPattern.MatchString(v.Key) || menuKeys[v.Key] || !routes[v.RouteKey] || !perms[v.Permission] || v.SortOrder < 0 || v.SortOrder > 10000 || len("plugin_"+strings.ReplaceAll(m.ID, "-", "_")+"_"+v.Key) > 64 || v.Icon != "Puzzle" && v.Icon != "LayoutDashboard" && v.Icon != "Settings" && v.Icon != "ListTree" && v.Icon != "FileText" && v.Icon != "Activity" {
			return ErrInvalid
		}
		menuKeys[v.Key] = true
	}
	deps := map[string]bool{}
	for _, d := range m.Dependencies {
		if !pluginIDPattern.MatchString(d.ID) || d.ID == m.ID || !validVersion(d.Version) || deps[d.ID] {
			return ErrInvalid
		}
		deps[d.ID] = true
	}
	fields := map[string]bool{}
	for _, f := range m.Config {
		if !keyPattern.MatchString(f.Key) || fields[f.Key] || strings.TrimSpace(f.Label) == "" || len(f.Label) > 120 || !oneOf(f.Type, "string", "integer", "boolean", "json") {
			return ErrInvalid
		}
		fields[f.Key] = true
	}
	for name, hash := range m.MigrationSHA256 {
		if !migrationName(name) || len(hash) != 64 {
			return ErrInvalid
		}
		if _, err := decodeHex(hash); err != nil {
			return ErrInvalid
		}
	}
	return nil
}
func oneOf(s string, values ...string) bool {
	for _, v := range values {
		if s == v {
			return true
		}
	}
	return false
}
func validVersion(version string) bool {
	if !versionPattern.MatchString(version) {
		return false
	}
	withoutBuild := strings.SplitN(version, "+", 2)
	if len(withoutBuild) == 2 && !validVersionIdentifiers(withoutBuild[1], false) {
		return false
	}
	coreAndPrerelease := strings.SplitN(withoutBuild[0], "-", 2)
	return len(coreAndPrerelease) == 1 || validVersionIdentifiers(coreAndPrerelease[1], true)
}

func validVersionIdentifiers(value string, prerelease bool) bool {
	for _, identifier := range strings.Split(value, ".") {
		if identifier == "" {
			return false
		}
		digits := true
		for _, r := range identifier {
			if !(r >= '0' && r <= '9' || r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r == '-') {
				return false
			}
			if r < '0' || r > '9' {
				digits = false
			}
		}
		if prerelease && digits && len(identifier) > 1 && identifier[0] == '0' {
			return false
		}
	}
	return true
}

func migrationName(s string) bool        { return migrationPattern.MatchString(s) }
func decodeHex(s string) ([]byte, error) { return hex.DecodeString(s) }
func sortedMigrationNames(m Manifest) []string {
	names := make([]string, 0, len(m.MigrationSHA256))
	for n := range m.MigrationSHA256 {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}
