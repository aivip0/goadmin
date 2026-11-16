package plugins

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"goadminkit/backend/internal/core/audit"
	"goadminkit/backend/internal/core/response"
	"goadminkit/backend/internal/core/security"
	"goadminkit/backend/internal/modules/auth"
	"goadminkit/backend/internal/modules/iam"
)

type Handler struct {
	service  *Service
	identity *auth.Service
	rights   *iam.Service
}
type action func(*gin.Context, context.Context, string)

func RegisterRoutes(r *gin.Engine, s *Service, a *auth.Service, rights *iam.Service) {
	h := &Handler{s, a, rights}
	base := "/api/v1/admin/plugins"
	r.GET(base, h.require("plugins:view", h.list))
	r.GET(base+"/packages", h.require("plugins:view", h.packages))
	r.GET(base+"/events", h.require("plugins:view", h.events))
	r.POST(base+"/install", h.require("plugins:install", h.install))
	r.GET(base+"/:id/config", h.require("plugins:view", h.getConfig))
	r.PUT(base+"/:id/config", h.require("plugins:configure", h.saveConfig))
	r.POST(base+"/:id/enable", h.require("plugins:enable", h.enable))
	r.POST(base+"/:id/disable", h.require("plugins:disable", h.disable))
	r.POST(base+"/:id/uninstall", h.require("plugins:uninstall", h.uninstall))
	r.POST(base+"/:id/upgrade", h.require("plugins:migrate", h.upgrade))
	r.GET(base+"/:id/routes/:key", h.route)
	r.POST(base+"/:id/routes/:key", h.route)
	r.PUT(base+"/:id/routes/:key", h.route)
	r.DELETE(base+"/:id/routes/:key", h.route)
}

func (h *Handler) require(permission string, next action) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
		defer cancel()
		cookies := c.Request.CookiesNamed(security.SessionCookie)
		if len(cookies) != 1 {
			response.Reject(c, response.Unauthenticated)
			return
		}
		user, err := h.identity.Me(ctx, cookies[0].Value)
		if errors.Is(err, auth.ErrUnauthorized) {
			response.Reject(c, response.Unauthenticated)
			return
		}
		if err != nil {
			response.Reject(c, response.Unavailable)
			return
		}
		allowed, err := h.rights.Has(ctx, user.ID, permission)
		if err != nil {
			response.Reject(c, response.Unavailable)
			return
		}
		if !allowed {
			response.Reject(c, response.Forbidden)
			return
		}
		next(c, audit.WithRequest(ctx, c.Request.Method, c.FullPath(), c.ClientIP(), c.Request.UserAgent()), user.ID)
	}
}

func (h *Handler) route(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()
	cookies := c.Request.CookiesNamed(security.SessionCookie)
	if len(cookies) != 1 {
		response.Reject(c, response.Unauthenticated)
		return
	}
	user, err := h.identity.Me(ctx, cookies[0].Value)
	if errors.Is(err, auth.ErrUnauthorized) {
		response.Reject(c, response.Unauthenticated)
		return
	}
	if err != nil {
		response.Reject(c, response.Unavailable)
		return
	}
	permission, err := h.service.RoutePermission(ctx, c.Param("id"), c.Param("key"))
	if err != nil {
		reject(c, err)
		return
	}
	allowed, err := h.rights.Has(ctx, user.ID, permission)
	if err != nil {
		response.Reject(c, response.Unavailable)
		return
	}
	if !allowed {
		response.Reject(c, response.Forbidden)
		return
	}
	var body json.RawMessage
	if c.Request.Method != "GET" {
		raw, e := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, 65536))
		if e != nil || !json.Valid(raw) {
			response.Reject(c, response.BadRequest)
			return
		}
		body = raw
	}
	value, err := h.service.Route(ctx, c.Param("id"), c.Param("key"), c.Request.Method, body)
	if err != nil {
		reject(c, err)
		return
	}
	response.Success(c, value)
}
func decode(c *gin.Context, v any) error {
	if c.ContentType() != "application/json" {
		return ErrInvalid
	}
	d := json.NewDecoder(http.MaxBytesReader(c.Writer, c.Request.Body, 65536))
	d.DisallowUnknownFields()
	if d.Decode(v) != nil || d.Decode(&struct{}{}) != io.EOF {
		return ErrInvalid
	}
	return nil
}
func reject(c *gin.Context, err error) {
	switch {
	case errors.Is(err, ErrInvalid):
		response.Reject(c, response.BadRequest)
	case errors.Is(err, ErrNotFound):
		response.Reject(c, response.NotFound)
	case errors.Is(err, ErrConflict):
		response.Reject(c, response.Conflict)
	case errors.Is(err, ErrUnavailable):
		response.Reject(c, response.Unavailable)
	default:
		response.Reject(c, response.Unavailable)
	}
}
func (h *Handler) list(c *gin.Context, ctx context.Context, _ string) {
	v, e := h.service.List(ctx)
	if e != nil {
		reject(c, e)
		return
	}
	response.Success(c, v)
}
func (h *Handler) packages(c *gin.Context, ctx context.Context, _ string) {
	v, e := h.service.Packages(ctx)
	if e != nil {
		reject(c, e)
		return
	}
	response.Success(c, v)
}
func (h *Handler) events(c *gin.Context, ctx context.Context, _ string) {
	limit := 50
	if c.Query("limit") != "" {
		n, e := strconv.Atoi(c.Query("limit"))
		if e != nil || n < 1 || n > 100 {
			response.Reject(c, response.BadRequest)
			return
		}
		limit = n
	}
	v, e := h.service.Events(ctx, c.Query("plugin_id"), limit)
	if e != nil {
		reject(c, e)
		return
	}
	response.Success(c, v)
}
func (h *Handler) install(c *gin.Context, ctx context.Context, actor string) {
	var in struct {
		ID      string `json:"id"`
		Version string `json:"version"`
	}
	if decode(c, &in) != nil {
		response.Reject(c, response.BadRequest)
		return
	}
	if e := h.service.Install(ctx, actor, in.ID, in.Version); e != nil {
		reject(c, e)
		return
	}
	response.Success(c, nil)
}
func (h *Handler) lifecycle(c *gin.Context, ctx context.Context, actor, op string) {
	if e := h.service.Lifecycle(ctx, actor, c.Param("id"), op); e != nil {
		reject(c, e)
		return
	}
	response.Success(c, nil)
}
func (h *Handler) enable(c *gin.Context, ctx context.Context, a string) {
	h.lifecycle(c, ctx, a, "enable")
}
func (h *Handler) disable(c *gin.Context, ctx context.Context, a string) {
	h.lifecycle(c, ctx, a, "disable")
}
func (h *Handler) uninstall(c *gin.Context, ctx context.Context, a string) {
	h.lifecycle(c, ctx, a, "uninstall")
}
func (h *Handler) upgrade(c *gin.Context, ctx context.Context, a string) {
	h.lifecycle(c, ctx, a, "upgrade")
}
func (h *Handler) getConfig(c *gin.Context, ctx context.Context, _ string) {
	v, e := h.service.Config(ctx, c.Param("id"))
	if e != nil {
		reject(c, e)
		return
	}
	response.Success(c, v)
}
func (h *Handler) saveConfig(c *gin.Context, ctx context.Context, actor string) {
	var in struct {
		Values map[string]json.RawMessage `json:"values"`
	}
	if decode(c, &in) != nil || in.Values == nil {
		response.Reject(c, response.BadRequest)
		return
	}
	if e := h.service.SaveConfig(ctx, actor, c.Param("id"), in.Values); e != nil {
		reject(c, e)
		return
	}
	response.Success(c, nil)
}
