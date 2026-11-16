package router

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

type probe func(context.Context) error

func (f probe) Ready(ctx context.Context) error { return f(ctx) }

func TestRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		method, path string
		ready        bool
		status, code int
	}{
		{"GET", "/api/v1/health", true, 200, 0}, {"GET", "/api/v1/health/ready", true, 200, 0},
		{"GET", "/api/v1/health", false, 503, 50301}, {"GET", "/api/v1/health/live", false, 200, 0},
		{"GET", "/api/v1/users", true, 404, 40400}, {"POST", "/api/v1/health", true, 405, 40500},
	} {
		t.Run(tc.method+tc.path+httpStatus(tc.status), func(t *testing.T) {
			r := New(probe(func(context.Context) error {
				if tc.ready {
					return nil
				}
				return errors.New("private database secret")
			}), slog.New(slog.NewTextHandler(io.Discard, nil)), time.Second)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(tc.method, tc.path, nil))
			if w.Code != tc.status {
				t.Fatalf("status %d: %s", w.Code, w.Body)
			}
			var body struct {
				Code int
				Data any
			}
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if body.Code != tc.code {
				t.Fatalf("code %d", body.Code)
			}
			if tc.status >= 400 && body.Data != nil {
				t.Fatal("error data must be null")
			}
			if len(w.Header().Get("X-Request-ID")) != 32 {
				t.Fatal("request id missing")
			}
		})
	}
}

func httpStatus(n int) string {
	if n == 200 {
		return "-ok"
	}
	return "-error"
}

func TestReadinessDeadline(t *testing.T) {
	r := New(probe(func(ctx context.Context) error { <-ctx.Done(); return ctx.Err() }), slog.New(slog.NewTextHandler(io.Discard, nil)), time.Millisecond)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/health", nil))
	if w.Code != 503 {
		t.Fatalf("status %d", w.Code)
	}
}

func TestRecoveryAndOpenAPI(t *testing.T) {
	r := New(probe(func(context.Context) error { return nil }), slog.New(slog.NewTextHandler(io.Discard, nil)), time.Second)
	r.GET("/panic", func(*gin.Context) { panic("secret") })
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/panic", nil))
	if w.Code != 500 || w.Body.String() != "{\"code\":50000,\"message\":\"Internal server error\",\"data\":null}" {
		t.Fatalf("unsafe recovery: %s", w.Body)
	}
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/openapi.json", nil))
	if w.Code != 200 || !json.Valid(w.Body.Bytes()) {
		t.Fatal("invalid OpenAPI JSON")
	}
}
