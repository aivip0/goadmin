package security

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"goadminkit/backend/internal/core/response"
)

const SessionCookie = "goadmin_session"
const CSRFCookie = "goadmin_csrf"

type Options struct {
	Secure         bool
	SameSite       http.SameSite
	SessionTTL     time.Duration
	CSRFTTL        time.Duration
	Secret         []byte
	Origins        []string
	TrustedProxies []string
}

func Defaults() Options {
	return Options{SameSite: http.SameSiteLaxMode, SessionTTL: 12 * time.Hour, CSRFTTL: time.Hour, Origins: []string{"http://127.0.0.1:3000", "http://localhost:3000"}}
}

func (o Options) Validate() error {
	if o.SessionTTL < time.Minute || o.SessionTTL > 30*24*time.Hour || o.CSRFTTL < time.Minute || o.CSRFTTL > 24*time.Hour {
		return errors.New("invalid security token lifetime")
	}
	if o.SameSite != http.SameSiteLaxMode && o.SameSite != http.SameSiteStrictMode {
		return errors.New("SameSite must be lax or strict")
	}
	if len(o.Secret) != 0 && len(o.Secret) < 32 {
		return errors.New("CSRF_SECRET must contain at least 32 bytes")
	}
	if len(o.Origins) == 0 {
		return errors.New("PUBLIC_ORIGINS must not be empty")
	}
	for _, origin := range o.Origins {
		u, err := url.Parse(origin)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || (o.Secure && u.Scheme != "https") {
			return errors.New("PUBLIC_ORIGINS requires exact HTTP origins; secure cookies require HTTPS")
		}
	}
	for _, proxy := range o.TrustedProxies {
		if net.ParseIP(proxy) == nil {
			_, network, err := net.ParseCIDR(proxy)
			if err != nil {
				return errors.New("TRUSTED_PROXIES requires explicit IPs or CIDRs")
			}
			ones, _ := network.Mask.Size()
			if ones == 0 {
				return errors.New("trusting every proxy is prohibited")
			}
		}
	}
	return nil
}

type Guard struct{ options Options }

func New(o Options) (*Guard, error) {
	if err := o.Validate(); err != nil {
		return nil, err
	}
	if len(o.Secret) == 0 {
		o.Secret = make([]byte, 32)
		if _, err := rand.Read(o.Secret); err != nil {
			return nil, errors.New("security initialization failed")
		}
	} else {
		o.Secret = append([]byte(nil), o.Secret...)
	}
	return &Guard{options: o}, nil
}

func (g *Guard) cookie(name, value string, ttl time.Duration) *http.Cookie {
	c := &http.Cookie{Name: name, Value: value, Path: "/", HttpOnly: true, Secure: g.options.Secure, SameSite: g.options.SameSite, MaxAge: int(ttl.Seconds()), Expires: time.Now().UTC().Add(ttl)}
	if ttl < 0 {
		c.MaxAge = -1
		c.Expires = time.Unix(1, 0).UTC()
	}
	return c
}

// Call only after the Auth service commits the token hash.
func (g *Guard) SetSession(w http.ResponseWriter, token string) {
	http.SetCookie(w, g.cookie(SessionCookie, token, g.options.SessionTTL))
	http.SetCookie(w, g.cookie(CSRFCookie, "", -time.Second))
}
func (g *Guard) ClearSession(w http.ResponseWriter) {
	http.SetCookie(w, g.cookie(SessionCookie, "", -time.Second))
	http.SetCookie(w, g.cookie(CSRFCookie, "", -time.Second))
}

func cookie(r *http.Request, name string) (string, bool) {
	var value string
	count := 0
	for _, c := range r.Cookies() {
		if c.Name == name {
			count++
			value = c.Value
		}
	}
	return value, count == 1
}

func sessionCookie(r *http.Request) (string, bool) {
	value, unique := cookie(r, SessionCookie)
	return value, unique || len(r.CookiesNamed(SessionCookie)) == 0
}

func (g *Guard) signature(payload, session string) string {
	mac := hmac.New(sha256.New, g.options.Secret)
	mac.Write([]byte(strconv.Itoa(len(session)) + ":" + session + ":" + payload))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func (g *Guard) originOK(r *http.Request, required bool) bool {
	if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
		return false
	}
	origin := r.Header.Get("Origin")
	if origin == "" {
		return !required
	}
	if len(r.Header.Values("Origin")) != 1 {
		return false
	}
	for _, allowed := range g.options.Origins {
		if origin == allowed {
			return true
		}
	}
	return false
}

func (g *Guard) IssueCSRF(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	if !g.originOK(c.Request, false) {
		response.Reject(c, response.CSRFInvalid)
		return
	}
	session, validSession := sessionCookie(c.Request)
	if !validSession {
		response.Reject(c, response.CSRFInvalid)
		return
	}
	var nonce [32]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		response.Reject(c, response.Internal)
		return
	}
	payload := strconv.FormatInt(time.Now().Unix(), 10) + "." + base64.RawURLEncoding.EncodeToString(nonce[:])
	token := payload + "." + g.signature(payload, session)
	http.SetCookie(c.Writer, g.cookie(CSRFCookie, token, g.options.CSRFTTL))
	response.Success(c, gin.H{"csrf_token": token, "expires_in": int(g.options.CSRFTTL.Seconds())})
}

func (g *Guard) valid(r *http.Request) bool {
	if !g.originOK(r, true) {
		return false
	}
	token := r.Header.Get("X-CSRF-Token")
	stored, ok := cookie(r, CSRFCookie)
	if !ok || len(token) > 256 || len(r.Header.Values("X-CSRF-Token")) != 1 || !hmac.Equal([]byte(token), []byte(stored)) {
		return false
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return false
	}
	issued, err := strconv.ParseInt(parts[0], 10, 64)
	now := time.Now().Unix()
	if err != nil || issued > now || now-issued >= int64(g.options.CSRFTTL.Seconds()) {
		return false
	}
	nonce, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || len(nonce) != 32 {
		return false
	}
	session, validSession := sessionCookie(r)
	return validSession && hmac.Equal([]byte(parts[2]), []byte(g.signature(parts[0]+"."+parts[1], session)))
}

func (g *Guard) Protect() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		method := c.Request.Method
		// Keep infrastructure method errors compatible; protect all business mutations, including unregistered routes.
		path := c.Request.URL.Path
		infrastructure := path == "/api/v1/health" || strings.HasPrefix(path, "/api/v1/health/") || path == "/api/v1/openapi.json"
		if strings.HasPrefix(path, "/api/v1/") && !infrastructure && method != "GET" && method != "HEAD" && method != "OPTIONS" && !g.valid(c.Request) {
			response.Reject(c, response.CSRFInvalid)
			return
		}
		c.Next()
	}
}
