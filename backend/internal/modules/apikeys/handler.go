package apikeys

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/gin-gonic/gin"
	"goadminkit/backend/internal/core/audit"
	"goadminkit/backend/internal/core/response"
	"goadminkit/backend/internal/core/security"
	"goadminkit/backend/internal/modules/auth"
	"goadminkit/backend/internal/modules/dashboard"
	"goadminkit/backend/internal/modules/iam"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type Handler struct {
	service   *Service
	identity  *auth.Service
	rights    *iam.Service
	dashboard *dashboard.Service
}

func RegisterRoutes(r *gin.Engine, s *Service, a *auth.Service, rights *iam.Service, d *dashboard.Service) {
	h := &Handler{s, a, rights, d}
	r.GET("/api/v1/admin/api-keys", h.session("api_keys:view", h.list))
	r.POST("/api/v1/admin/api-keys", h.session("api_keys:create", h.create))
	r.POST("/api/v1/admin/api-keys/:id/revoke", h.session("api_keys:revoke", h.revoke))
	r.GET("/api/v1/key/dashboard/summary", h.keySummary)
}
func (h *Handler) session(permission string, next func(*gin.Context, context.Context, string)) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		ctx, cancel := context.WithTimeout(c.Request.Context(), 4*time.Second)
		defer cancel()
		cookies := c.Request.CookiesNamed(security.SessionCookie)
		if len(cookies) != 1 {
			response.Reject(c, response.Unauthenticated)
			return
		}
		u, err := h.identity.Me(ctx, cookies[0].Value)
		if errors.Is(err, auth.ErrUnauthorized) {
			response.Reject(c, response.Unauthenticated)
			return
		}
		if err != nil {
			response.Reject(c, response.Unavailable)
			return
		}
		allowed, err := h.rights.Has(ctx, u.ID, permission)
		if err != nil {
			response.Reject(c, response.Unavailable)
			return
		}
		if !allowed {
			response.Reject(c, response.Forbidden)
			return
		}
		next(c, audit.WithRequest(ctx, c.Request.Method, c.FullPath(), c.ClientIP(), c.Request.UserAgent()), u.ID)
	}
}
func reject(c *gin.Context, err error) {
	switch {
	case errors.Is(err, ErrInvalid):
		response.Reject(c, response.BadRequest)
	case errors.Is(err, ErrNotFound):
		response.Reject(c, response.NotFound)
	case errors.Is(err, ErrForbidden):
		response.Reject(c, response.Forbidden)
	default:
		response.Reject(c, response.Unavailable)
	}
}
func (h *Handler) list(c *gin.Context, ctx context.Context, user string) {
	p, size := 1, 20
	var err error
	for key, v := range c.Request.URL.Query() {
		if len(v) != 1 || (key != "page" && key != "page_size") {
			response.Reject(c, response.BadRequest)
			return
		}
	}
	if c.Query("page") != "" {
		p, err = strconv.Atoi(c.Query("page"))
		if err != nil {
			reject(c, ErrInvalid)
			return
		}
	}
	if c.Query("page_size") != "" {
		size, err = strconv.Atoi(c.Query("page_size"))
		if err != nil {
			reject(c, ErrInvalid)
			return
		}
	}
	v, err := h.service.List(ctx, user, p, size)
	if err != nil {
		reject(c, err)
		return
	}
	response.Success(c, v)
}
func (h *Handler) create(c *gin.Context, ctx context.Context, user string) {
	if c.ContentType() != "application/json" {
		reject(c, ErrInvalid)
		return
	}
	var in Input
	dec := json.NewDecoder(http.MaxBytesReader(c.Writer, c.Request.Body, 4096))
	dec.DisallowUnknownFields()
	if dec.Decode(&in) != nil || dec.Decode(&struct{}{}) != io.EOF {
		reject(c, ErrInvalid)
		return
	}
	v, err := h.service.Create(ctx, user, in)
	if err != nil {
		reject(c, err)
		return
	}
	response.Success(c, v)
}
func (h *Handler) revoke(c *gin.Context, ctx context.Context, user string) {
	if err := h.service.Revoke(ctx, user, c.Param("id")); err != nil {
		reject(c, err)
		return
	}
	response.Success(c, gin.H{"revoked": true})
}
func (h *Handler) keySummary(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	if len(c.Request.CookiesNamed(security.SessionCookie)) != 0 || len(c.Request.Header.Values("Authorization")) != 1 {
		response.Reject(c, response.Unauthenticated)
		return
	}
	raw, ok := strings.CutPrefix(c.Request.Header.Get("Authorization"), "Bearer ")
	if !ok {
		response.Reject(c, response.Unauthenticated)
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 4*time.Second)
	defer cancel()
	owner, err := h.service.Resolve(ctx, raw)
	if errors.Is(err, ErrUnauthorized) {
		response.Reject(c, response.Unauthenticated)
		return
	}
	if err != nil {
		response.Reject(c, response.Unavailable)
		return
	}
	allowed, err := h.rights.Has(ctx, owner, "dashboard:view")
	if err != nil {
		response.Reject(c, response.Unavailable)
		return
	}
	if !allowed {
		response.Reject(c, response.Forbidden)
		return
	}
	value, err := h.dashboard.Summary(ctx)
	if err != nil {
		response.Reject(c, response.Unavailable)
		return
	}
	response.Success(c, value)
}
