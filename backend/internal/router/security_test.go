package router

import (
	"github.com/gin-gonic/gin"
	"goadminkit/backend/internal/core/security"
	"io"
	"log/slog"
	"net/http/httptest"
	"testing"
	"time"
)

func TestTrustedClientIP(t *testing.T) {
	for _, tt := range []struct {
		trusted []string
		want    string
	}{
		{nil, "192.0.2.10"}, {[]string{"192.0.2.10"}, "198.51.100.5"},
		{[]string{"10.0.0.0/8"}, "192.0.2.10"},
	} {
		r := New(nil, slog.New(slog.NewTextHandler(io.Discard, nil)), time.Second)
		if err := r.SetTrustedProxies(tt.trusted); err != nil {
			t.Fatal(err)
		}
		r.RemoteIPHeaders = []string{"X-Forwarded-For"}
		r.GET("/ip", func(c *gin.Context) { c.String(200, c.ClientIP()) })
		req := httptest.NewRequest("GET", "/ip", nil)
		req.RemoteAddr = "192.0.2.10:1234"
		req.Header.Set("X-Forwarded-For", "198.51.100.5")
		req.Header.Set("Forwarded", "for=203.0.113.1")
		req.Header.Set("X-Real-IP", "203.0.113.2")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Body.String() != tt.want {
			t.Fatalf("got client IP %s", w.Body.String())
		}
	}
}

func TestSecurityDoesNotChangeInfrastructureMethodErrors(t *testing.T) {
	g, err := security.New(security.Defaults())
	if err != nil {
		t.Fatal(err)
	}
	r := New(nil, slog.New(slog.NewTextHandler(io.Discard, nil)), time.Second, g)
	for _, tt := range []struct {
		method, path string
		status       int
	}{{"POST", "/api/v1/health", 405}, {"GET", "/api/v1/health/live", 200}, {"GET", "/api/v1/openapi.json", 200}, {"POST", "/api/v1/auth/login", 403}} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(tt.method, tt.path, nil))
		if w.Code != tt.status {
			t.Fatalf("%s: %d", tt.path, w.Code)
		}
	}
}
