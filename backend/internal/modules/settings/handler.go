package settings

import (
	"context"
	"encoding/json"
	"errors"
	"io"
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

func RegisterRoutes(r *gin.Engine, s *Service, a *auth.Service, rights *iam.Service) {
	h := &Handler{s, a, rights}
	r.GET("/api/v1/admin/settings", h.require("settings:view", h.get))
	r.PUT("/api/v1/admin/settings", h.require("settings:update", h.put))
}
func (h *Handler) require(permission string, next func(*gin.Context, context.Context, string)) gin.HandlerFunc {
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
func (h *Handler) get(c *gin.Context, ctx context.Context, _ string) {
	value, err := h.service.Get(ctx)
	if err != nil {
		response.Reject(c, response.Unavailable)
		return
	}
	response.Success(c, value)
}
func (h *Handler) put(c *gin.Context, ctx context.Context, actor string) {
	var payload struct {
		Settings map[string]json.RawMessage `json:"settings"`
	}
	decoder := json.NewDecoder(io.LimitReader(c.Request.Body, 32769))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&payload); err != nil {
		response.Reject(c, response.BadRequest)
		return
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		response.Reject(c, response.BadRequest)
		return
	}
	if err := h.service.Update(ctx, actor, payload.Settings); err != nil {
		switch {
		case errors.Is(err, ErrInvalid):
			response.Reject(c, response.Unprocessable)
		case errors.Is(err, ErrForbidden):
			response.Reject(c, response.Forbidden)
		default:
			response.Reject(c, response.Unavailable)
		}
		return
	}
	h.get(c, ctx, actor)
}
