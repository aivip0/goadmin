package media

import (
	"net/http"
	"testing"
	"time"
)

func TestDeadlineBudgetOnlyRegisteredMediaRoutes(t *testing.T) {
	id := "d9c010a2-793f-4d84-ade5-77427ba9a675"
	for _, tc := range []struct {
		path, method string
		read, write  time.Duration
	}{
		{"/api/v1/admin/media", http.MethodPost, 65 * time.Second, 75 * time.Second},
		{"/api/v1/admin/media/" + id + "/content", http.MethodGet, 10 * time.Second, 95 * time.Second},
		{"/api/v1/admin/media/" + id + "/unknown", http.MethodGet, 10 * time.Second, 15 * time.Second},
		{"/api/v1/admin/media/invalid/content", http.MethodGet, 10 * time.Second, 15 * time.Second},
		{"/api/v1/admin/media/" + id + "/content", http.MethodDelete, 10 * time.Second, 15 * time.Second},
		{"/api/v1/admin/media", http.MethodGet, 10 * time.Second, 15 * time.Second},
		{"/api/v1/auth/login", http.MethodPost, 10 * time.Second, 15 * time.Second},
	} {
		read, write := deadlineBudget(tc.path, tc.method)
		if read != tc.read || write != tc.write {
			t.Errorf("%s %s: got %v/%v, want %v/%v", tc.method, tc.path, read, write, tc.read, tc.write)
		}
	}
}
