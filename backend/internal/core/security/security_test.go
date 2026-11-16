package security

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestCSRF(t *testing.T) {
	gin.SetMode(gin.TestMode)
	o := Defaults()
	o.Secret = []byte(strings.Repeat("x", 32))
	g, err := New(o)
	if err != nil {
		t.Fatal(err)
	}
	r := gin.New()
	r.Use(g.Protect())
	r.GET("/api/v1/auth/csrf", g.IssueCSRF)
	r.POST("/api/v1/example", func(c *gin.Context) { c.Status(204) })
	get := httptest.NewRecorder()
	r.ServeHTTP(get, httptest.NewRequest("GET", "/api/v1/auth/csrf", nil))
	var data struct {
		Data struct {
			Token string `json:"csrf_token"`
		} `json:"data"`
	}
	if err = json.Unmarshal(get.Body.Bytes(), &data); err != nil {
		t.Fatal(err)
	}
	csrf := get.Result().Cookies()[0]
	if data.Data.Token == "" || !csrf.HttpOnly || csrf.Path != "/" || csrf.SameSite != http.SameSiteLaxMode || csrf.MaxAge != 3600 || csrf.Domain != "" {
		t.Fatal("invalid CSRF cookie policy")
	}
	for _, tt := range []struct {
		name, token, origin, site, session string
		status                             int
	}{
		{"missing", "", "http://127.0.0.1:3000", "", "", 403},
		{"wrong", "wrong", "http://127.0.0.1:3000", "", "", 403},
		{"valid", data.Data.Token, "http://127.0.0.1:3000", "same-origin", "", 204},
		{"no origin", data.Data.Token, "", "", "", 403},
		{"null origin", data.Data.Token, "null", "", "", 403},
		{"hostile origin", data.Data.Token, "https://attacker.invalid", "", "", 403},
		{"cross site", data.Data.Token, "http://127.0.0.1:3000", "cross-site", "", 403},
		{"session changed", data.Data.Token, "http://127.0.0.1:3000", "", "new-session", 403},
	} {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/api/v1/example", nil)
			req.AddCookie(csrf)
			req.Header.Set("X-CSRF-Token", tt.token)
			req.Header.Set("Origin", tt.origin)
			req.Header.Set("Sec-Fetch-Site", tt.site)
			if tt.session != "" {
				req.AddCookie(&http.Cookie{Name: SessionCookie, Value: tt.session})
			}
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			if w.Code != tt.status {
				t.Fatalf("got %d", w.Code)
			}
		})
	}
	for _, payload := range []string{"0." + strings.Repeat("a", 43), "9999999999." + strings.Repeat("a", 43)} {
		token := payload + "." + g.signature(payload, "")
		req := httptest.NewRequest("POST", "/api/v1/example", nil)
		req.Header.Set("Origin", o.Origins[0])
		req.Header.Set("X-CSRF-Token", token)
		req.AddCookie(&http.Cookie{Name: CSRFCookie, Value: token})
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != 403 {
			t.Fatal("expired or future token accepted")
		}
	}
	for _, origin := range []string{"null", "https://attacker.invalid"} {
		req := httptest.NewRequest("GET", "/api/v1/auth/csrf", nil)
		req.Header.Set("Origin", origin)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != 403 || len(w.Result().Cookies()) != 0 {
			t.Fatal("cross-origin token issuance accepted")
		}
	}
	for _, method := range []string{"GET", "POST"} {
		path := "/api/v1/auth/csrf"
		if method == "POST" {
			path = "/api/v1/example"
		}
		req := httptest.NewRequest(method, path, nil)
		req.Header.Set("Origin", o.Origins[0])
		req.Header.Set("X-CSRF-Token", data.Data.Token)
		req.AddCookie(csrf)
		req.AddCookie(&http.Cookie{Name: SessionCookie, Value: ""})
		req.AddCookie(&http.Cookie{Name: SessionCookie, Value: ""})
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != 403 {
			t.Fatal("ambiguous session cookies accepted")
		}
	}
}

func TestCookieLifecycleAndValidation(t *testing.T) {
	o := Defaults()
	o.Secure = true
	o.Origins = []string{"https://admin.example.test"}
	g, err := New(o)
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	g.SetSession(w, "opaque-random-token")
	c := w.Result().Cookies()[0]
	if !c.HttpOnly || !c.Secure || c.MaxAge != 43200 || c.Path != "/" || c.Domain != "" || c.Expires.Before(time.Now()) {
		t.Fatal("unsafe session cookie")
	}
	w = httptest.NewRecorder()
	g.ClearSession(w)
	for _, c := range w.Result().Cookies() {
		if c.MaxAge != -1 || c.Value != "" || !c.Expires.Before(time.Now()) || !c.HttpOnly || !c.Secure {
			t.Fatal("invalid cookie expiry")
		}
	}
	for _, proxy := range []string{"0.0.0.0/0", "::/0", "arbitrary"} {
		o := Defaults()
		o.TrustedProxies = []string{proxy}
		if _, err := New(o); err == nil {
			t.Fatal("unsafe proxy accepted")
		}
	}
}
