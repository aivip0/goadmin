package notifications

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/gin-gonic/gin"
	"goadminkit/backend/internal/core/audit"
	"goadminkit/backend/internal/core/response"
	"goadminkit/backend/internal/core/security"
	"goadminkit/backend/internal/modules/auth"
	"goadminkit/backend/internal/modules/iam"
	"io"
	"net/http"
	"strconv"
	"time"
)

type Handler struct {
	service  *Service
	identity *auth.Service
	rights   *iam.Service
}

func RegisterRoutes(r *gin.Engine, s *Service, a *auth.Service, rights *iam.Service) {
	h := &Handler{s, a, rights}
	r.GET("/api/v1/admin/notifications", h.require("notifications:view", h.list))
	r.POST("/api/v1/admin/notifications", h.require("notifications:create", h.create))
	r.POST("/api/v1/admin/notifications/:id/read", h.require("notifications:view", h.read))
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
		ok, err := h.rights.Has(ctx, u.ID, permission)
		if err != nil {
			response.Reject(c, response.Unavailable)
			return
		}
		if !ok {
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
	case errors.Is(err, ErrRecipient):
		response.Reject(c, response.Unprocessable)
	case errors.Is(err, ErrForbidden):
		response.Reject(c, response.Forbidden)
	default:
		response.Reject(c, response.Unavailable)
	}
}
func (h *Handler) list(c *gin.Context, ctx context.Context, user string) {
	page, size := 1, 20
	var err error
	for key, values := range c.Request.URL.Query() {
		if len(values) != 1 || (key != "page" && key != "page_size") {
			response.Reject(c, response.BadRequest)
			return
		}
	}
	if c.Query("page") != "" {
		page, err = strconv.Atoi(c.Query("page"))
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
	v, err := h.service.List(ctx, user, page, size)
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
	decoder := json.NewDecoder(http.MaxBytesReader(c.Writer, c.Request.Body, 8192))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&in) != nil || decoder.Decode(&struct{}{}) != io.EOF {
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
func (h *Handler) read(c *gin.Context, ctx context.Context, user string) {
	v, err := h.service.Read(ctx, user, c.Param("id"))
	if err != nil {
		reject(c, err)
		return
	}
	response.Success(c, v)
}
