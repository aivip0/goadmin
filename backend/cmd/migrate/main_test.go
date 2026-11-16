package main

import (
	"strings"
	"testing"
)

func TestAuthorizeUpgrade(t *testing.T) {
	confirmation := map[string]string{
		"MIGRATION_CONFIRM_DATABASE":     "isolated",
		"MIGRATION_CONFIRM_FROM_VERSION": "1",
		"MIGRATION_CONFIRM_TO_VERSION":   "9",
		"MIGRATION_BACKUP_REFERENCE":     "verified-backup-2026-11-13",
	}
	lookup := func(values map[string]string) func(string) (string, bool) {
		return func(key string) (string, bool) { value, ok := values[key]; return value, ok }
	}
	for _, tc := range []struct {
		name        string
		current     uint
		latest      uint
		environment string
		database    string
		values      map[string]string
		allowed     bool
	}{
		{"fresh development", 0, 9, "development", "isolated", nil, true},
		{"fresh test", 0, 9, "test", "isolated", nil, true},
		{"fresh production without confirmation", 0, 9, "production", "isolated", nil, false},
		{"existing without confirmation", 1, 9, "development", "isolated", nil, false},
		{"exact existing upgrade", 1, 9, "development", "isolated", confirmation, true},
		{"wrong database", 1, 9, "development", "persistent", confirmation, false},
		{"wrong actual version", 2, 9, "development", "isolated", confirmation, false},
		{"wrong binary version", 1, 10, "development", "isolated", confirmation, false},
		{"newer database", 10, 9, "development", "isolated", confirmation, false},
		{"current no-op", 9, 9, "development", "isolated", nil, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := authorize(tc.current, tc.latest, tc.environment, tc.database, lookup(tc.values))
			if (err == nil) != tc.allowed {
				t.Fatalf("allowed=%t, error=%v", tc.allowed, err)
			}
		})
	}
	for _, key := range []string{"MIGRATION_CONFIRM_DATABASE", "MIGRATION_CONFIRM_FROM_VERSION", "MIGRATION_CONFIRM_TO_VERSION", "MIGRATION_BACKUP_REFERENCE"} {
		clone := make(map[string]string, len(confirmation))
		for name, value := range confirmation {
			clone[name] = value
		}
		delete(clone, key)
		if err := authorize(1, 9, "production", "isolated", lookup(clone)); err == nil {
			t.Fatalf("missing %s permitted existing upgrade", key)
		}
	}
	blank := make(map[string]string, len(confirmation))
	for name, value := range confirmation {
		blank[name] = value
	}
	blank["MIGRATION_BACKUP_REFERENCE"] = strings.Repeat(" ", 3)
	if err := authorize(1, 9, "production", "isolated", lookup(blank)); err == nil {
		t.Fatal("blank backup reference permitted existing upgrade")
	}
}
