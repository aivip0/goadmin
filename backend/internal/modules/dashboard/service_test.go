package dashboard

import (
	"testing"
	"time"
)

func TestUTCBoundsAndSparseTrend(t *testing.T) {
	now := time.Date(2026, 11, 12, 23, 30, 0, 0, time.FixedZone("UTC-5", -5*3600))
	start, end := dayBounds(now, 7)
	if got := start.Format(time.RFC3339); got != "2026-11-07T00:00:00Z" {
		t.Fatal(got)
	}
	if got := end.Format(time.RFC3339); got != "2026-11-14T00:00:00Z" {
		t.Fatal(got)
	}
	points := buildTrends(start, 7, map[string]int64{"2026-11-07": 3, "2026-11-13": 2})
	if len(points) != 7 || points[0].Count != 3 || points[1].Count != 0 || points[6].Count != 2 {
		t.Fatalf("unexpected points: %+v", points)
	}
	start, end = dayBounds(now, 30)
	if len(buildTrends(start, 30, nil)) != 30 || end.Sub(start) != 30*24*time.Hour {
		t.Fatal("30-day UTC range failed")
	}
}
