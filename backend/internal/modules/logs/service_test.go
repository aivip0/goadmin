package logs

import (
	"testing"
	"time"
)

func TestFilterBounds(t *testing.T) {
	to := time.Now().UTC().Truncate(24 * time.Hour).Add(24 * time.Hour)
	f := Filter{Page: 1, PageSize: 20, From: to.Add(-30 * 24 * time.Hour), To: to}
	if !validFilter("audit", f) || !validFilter("login", f) {
		t.Fatal("valid filter rejected")
	}
	for _, change := range []func(*Filter){
		func(v *Filter) { v.Page = 0 }, func(v *Filter) { v.Page = 1001 },
		func(v *Filter) { v.PageSize = 51 }, func(v *Filter) { v.UserID = "not-a-uuid" },
		func(v *Filter) { v.From = v.To }, func(v *Filter) { v.From = v.To.Add(-367 * 24 * time.Hour) },
	} {
		copy := f
		change(&copy)
		if validFilter("audit", copy) {
			t.Fatalf("invalid filter accepted: %+v", copy)
		}
	}
	f.Action = "setting_updated"
	if !validFilter("audit", f) || validFilter("login", f) {
		t.Fatal("action filter scope")
	}
	f.Action = ""
	f.Outcome = "rate_limited"
	if !validFilter("login", f) || validFilter("audit", f) {
		t.Fatal("outcome filter scope")
	}
}
