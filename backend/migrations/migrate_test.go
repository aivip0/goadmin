package migrations

import "testing"

func TestLatestVersion(t *testing.T) {
	version, err := LatestVersion()
	if err != nil || version != 13 {
		t.Fatalf("latest version = %d, error = %v", version, err)
	}
}
