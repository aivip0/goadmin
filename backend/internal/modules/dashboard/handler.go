package dashboard

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"goadminkit/backend/internal/core/response"
	"goadminkit/backend/internal/core/security"
	"goadminkit/backend/internal/modules/auth"
	"goadminkit/backend/internal/modules/iam"
)

type Handler struct {
	service   *Service
	identity  *auth.Service
	rights    *iam.Service
	readiness interface{ Ready(context.Context) error }
}

func RegisterRoutes(r *gin.Engine, s *Service, a *auth.Service, rights *iam.Service, readiness interface{ Ready(context.Context) error }) {
	h := &Handler{s, a, rights, readiness}
	r.GET("/api/v1/admin/dashboard/summary", h.require(h.summary))
	r.GET("/api/v1/admin/dashboard/activity", h.require(h.activity))
	r.GET("/api/v1/admin/dashboard/trends", h.require(h.trends))
}

func (h *Handler) require(next func(*gin.Context, context.Context)) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		ctx, cancel := context.WithTimeout(c.Request.Context(), 4*time.Second)
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
		allowed, err := h.rights.Has(ctx, user.ID, "dashboard:view")
		if err != nil {
			response.Reject(c, response.Unavailable)
			return
		}
		if !allowed {
			response.Reject(c, response.Forbidden)
			return
		}
		next(c, ctx)
	}
}

func (h *Handler) summary(c *gin.Context, ctx context.Context) {
	v, err := h.service.Summary(ctx)
	if err != nil {
		response.Reject(c, response.Unavailable)
		return
	}
	// This is a public-status category, never a database error or connection detail.
	v.Service = "ready"
	if err = h.readiness.Ready(ctx); err != nil {
		v.Service = "unavailable"
	}
	response.Success(c, v)
}

func (h *Handler) activity(c *gin.Context, ctx context.Context) {
	q := c.Request.URL.Query()
	if len(q) > 2 {
		response.Reject(c, response.BadRequest)
		return
	}
	for key := range q {
		if key != "page" && key != "page_size" || len(q[key]) != 1 {
			response.Reject(c, response.BadRequest)
			return
		}
	}
	page, size := 1, 10
	var err error
	if q.Has("page") {
		page, err = strconv.Atoi(q.Get("page"))
		if err != nil {
			response.Reject(c, response.BadRequest)
			return
		}
	}
	if q.Has("page_size") {
		size, err = strconv.Atoi(q.Get("page_size"))
		if err != nil {
			response.Reject(c, response.BadRequest)
			return
		}
	}
	v, err := h.service.Activity(ctx, page, size)
	if errors.Is(err, ErrInvalid) {
		response.Reject(c, response.BadRequest)
		return
	}
	if err != nil {
		response.Reject(c, response.Unavailable)
		return
	}
	response.Success(c, v)
}

func (h *Handler) trends(c *gin.Context, ctx context.Context) {
	q := c.Request.URL.Query()
	if len(q) != 1 || len(q["range"]) != 1 {
		response.Reject(c, response.BadRequest)
		return
	}
	v, err := h.service.Trends(ctx, q.Get("range"), time.Now())
	if errors.Is(err, ErrInvalid) {
		response.Reject(c, response.BadRequest)
		return
	}
	if err != nil {
		response.Reject(c, response.Unavailable)
		return
	}
	response.Success(c, v)
}
