package articles

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
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

func RegisterRoutes(r *gin.Engine, service *Service, identity *auth.Service, rights *iam.Service) {
	h := &Handler{service, identity, rights}
	r.GET("/api/v1/admin/articles", h.require("articles:view", h.list))
	r.POST("/api/v1/admin/articles", h.require("articles:create", h.create))
	r.GET("/api/v1/admin/articles/:id", h.require("articles:view", h.get))
	r.PUT("/api/v1/admin/articles/:id", h.require("articles:update", h.update))
	r.DELETE("/api/v1/admin/articles/:id", h.require("articles:delete", h.delete))
	r.POST("/api/v1/admin/articles/:id/publish", h.require("articles:publish", h.publish))
	r.POST("/api/v1/admin/articles/:id/unpublish", h.require("articles:publish", h.unpublish))
	r.GET("/api/v1/admin/article-categories", h.require("article_categories:view", h.categories))
	r.POST("/api/v1/admin/article-categories", h.require("article_categories:create", h.createCategory))
	r.PUT("/api/v1/admin/article-categories/:id", h.require("article_categories:update", h.updateCategory))
	r.DELETE("/api/v1/admin/article-categories/:id", h.require("article_categories:delete", h.deleteCategory))
}
func (h *Handler) require(permission string, next action) gin.HandlerFunc {
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
	case errors.Is(err, ErrConflict):
		response.Reject(c, response.Conflict)
	case errors.Is(err, ErrTransition):
		response.Reject(c, response.Unprocessable)
	case errors.Is(err, ErrForbidden):
		response.Reject(c, response.Forbidden)
	default:
		response.Reject(c, response.Unavailable)
	}
}
func decode(c *gin.Context, dst any) error {
	if c.ContentType() != "application/json" {
		return ErrInvalid
	}
	d := json.NewDecoder(http.MaxBytesReader(c.Writer, c.Request.Body, 131072))
	d.DisallowUnknownFields()
	if d.Decode(dst) != nil || d.Decode(&struct{}{}) != io.EOF {
		return ErrInvalid
	}
	return nil
}
func paging(c *gin.Context) (int, int, error) {
	page, size := 1, 20
	if v := c.Query("page"); v != "" {
		n, e := strconv.Atoi(v)
		if e != nil {
			return 0, 0, ErrInvalid
		}
		page = n
	}
	if v := c.Query("page_size"); v != "" {
		n, e := strconv.Atoi(v)
		if e != nil {
			return 0, 0, ErrInvalid
		}
		size = n
	}
	if page < 1 || page > 1000 || size < 1 || size > 50 {
		return 0, 0, ErrInvalid
	}
	return page, size, nil
}
func (h *Handler) list(c *gin.Context, ctx context.Context, _ string) {
	page, size, err := paging(c)
	if err != nil {
		reject(c, err)
		return
	}
	f := Filter{Page: page, PageSize: size, Query: strings.TrimSpace(c.Query("q")), Status: c.Query("status"), CategoryID: c.Query("category_id")}
	result, err := h.service.List(ctx, f)
	if err != nil {
		reject(c, err)
		return
	}
	response.Success(c, result)
}
func (h *Handler) get(c *gin.Context, ctx context.Context, _ string) {
	v, err := h.service.Get(ctx, c.Param("id"))
	if err != nil {
		reject(c, err)
		return
	}
	response.Success(c, v)
}
func (h *Handler) create(c *gin.Context, ctx context.Context, actor string) {
	var in ArticleInput
	if err := decode(c, &in); err != nil {
		reject(c, err)
		return
	}
	v, err := h.service.Create(ctx, actor, in)
	if err != nil {
		reject(c, err)
		return
	}
	response.Success(c, v)
}
func (h *Handler) update(c *gin.Context, ctx context.Context, actor string) {
	var in ArticleInput
	if err := decode(c, &in); err != nil {
		reject(c, err)
		return
	}
	v, err := h.service.Update(ctx, actor, c.Param("id"), in)
	if err != nil {
		reject(c, err)
		return
	}
	response.Success(c, v)
}
func (h *Handler) delete(c *gin.Context, ctx context.Context, actor string) {
	if err := h.service.Delete(ctx, actor, c.Param("id")); err != nil {
		reject(c, err)
		return
	}
	response.Success(c, gin.H{"deleted": true})
}
func (h *Handler) publish(c *gin.Context, ctx context.Context, actor string) {
	h.transition(c, ctx, actor, "publish")
}
func (h *Handler) unpublish(c *gin.Context, ctx context.Context, actor string) {
	h.transition(c, ctx, actor, "unpublish")
}
func (h *Handler) transition(c *gin.Context, ctx context.Context, actor, kind string) {
	v, err := h.service.Transition(ctx, actor, c.Param("id"), kind)
	if err != nil {
		reject(c, err)
		return
	}
	response.Success(c, v)
}
func (h *Handler) categories(c *gin.Context, ctx context.Context, _ string) {
	page, size, err := paging(c)
	if err != nil {
		reject(c, err)
		return
	}
	v, err := h.service.Categories(ctx, page, size)
	if err != nil {
		reject(c, err)
		return
	}
	response.Success(c, v)
}
func (h *Handler) createCategory(c *gin.Context, ctx context.Context, actor string) {
	var in CategoryInput
	if err := decode(c, &in); err != nil {
		reject(c, err)
		return
	}
	v, err := h.service.CreateCategory(ctx, actor, in)
	if err != nil {
		reject(c, err)
		return
	}
	response.Success(c, v)
}
func (h *Handler) updateCategory(c *gin.Context, ctx context.Context, actor string) {
	var in CategoryInput
	if err := decode(c, &in); err != nil {
		reject(c, err)
		return
	}
	v, err := h.service.UpdateCategory(ctx, actor, c.Param("id"), in)
	if err != nil {
		reject(c, err)
		return
	}
	response.Success(c, v)
}
func (h *Handler) deleteCategory(c *gin.Context, ctx context.Context, actor string) {
	if err := h.service.DeleteCategory(ctx, actor, c.Param("id")); err != nil {
		reject(c, err)
		return
	}
	response.Success(c, gin.H{"deleted": true})
}
