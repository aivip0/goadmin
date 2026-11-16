package logs

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
	service  *Service
	identity *auth.Service
	rights   *iam.Service
}

func RegisterRoutes(r *gin.Engine, service *Service, identity *auth.Service, rights *iam.Service) {
	h := &Handler{service, identity, rights}
	for _, entry := range []struct{ path, kind, permission string }{
		{"audit-logs", "audit", "audit:view"},
		{"login-logs", "login", "login_logs:view"},
	} {
		kind, permission := entry.kind, entry.permission
		path := "/api/v1/admin/" + entry.path
		r.GET(path, h.require(permission, func(c *gin.Context, ctx context.Context) { h.list(c, ctx, kind) }))
		r.GET(path+"/:id", h.require(permission, func(c *gin.Context, ctx context.Context) { h.detail(c, ctx, kind) }))
	}
}

func (h *Handler) require(permission string, next func(*gin.Context, context.Context)) gin.HandlerFunc {
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
		allowed, err := h.rights.Has(ctx, user.ID, permission)
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

func (h *Handler) list(c *gin.Context, ctx context.Context, kind string) {
	q := c.Request.URL.Query()
	for key, values := range q {
		if len(values) != 1 || (key != "page" && key != "page_size" && key != "from" && key != "to" && key != "user_id" && key != "action" && key != "outcome") {
			response.Reject(c, response.BadRequest)
			return
		}
	}
	if (kind == "audit" && q.Has("outcome")) || (kind == "login" && q.Has("action")) {
		response.Reject(c, response.BadRequest)
		return
	}
	page, size := 1, 20
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
	today := time.Now().UTC().Truncate(24 * time.Hour)
	f := Filter{Page: page, PageSize: size, From: today.AddDate(0, 0, -29), To: today.AddDate(0, 0, 1), UserID: q.Get("user_id"), Action: q.Get("action"), Outcome: q.Get("outcome")}
	for _, field := range []struct {
		key    string
		target *time.Time
	}{{"from", &f.From}, {"to", &f.To}} {
		if q.Has(field.key) {
			v, e := time.Parse("2006-01-02", q.Get(field.key))
			if e != nil || v.Format("2006-01-02") != q.Get(field.key) {
				response.Reject(c, response.BadRequest)
				return
			}
			*field.target = v
			if field.key == "to" {
				*field.target = v.AddDate(0, 0, 1)
			}
		}
	}
	result, err := h.service.List(ctx, kind, f)
	if errors.Is(err, ErrInvalid) {
		response.Reject(c, response.BadRequest)
		return
	}
	if err != nil {
		response.Reject(c, response.Unavailable)
		return
	}
	response.Success(c, result)
}

func (h *Handler) detail(c *gin.Context, ctx context.Context, kind string) {
	item, err := h.service.Get(ctx, kind, c.Param("id"))
	switch {
	case errors.Is(err, ErrInvalid):
		response.Reject(c, response.BadRequest)
	case errors.Is(err, ErrNotFound):
		response.Reject(c, response.NotFound)
	case err != nil:
		response.Reject(c, response.Unavailable)
	default:
		response.Success(c, item)
	}
}
