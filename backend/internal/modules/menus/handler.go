package menus

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	auditlog "goadminkit/backend/internal/core/audit"
	"goadminkit/backend/internal/core/response"
	"goadminkit/backend/internal/core/security"
	"goadminkit/backend/internal/modules/auth"
)

type Handler struct {
	service *Service
	auth    *auth.Service
}
type action func(*gin.Context, context.Context, string)

func RegisterRoutes(r *gin.Engine, service *Service, identity *auth.Service) {
	h := &Handler{service, identity}
	base := "/api/v1/admin/menus"
	r.GET(base+"/my", h.require("", h.my))
	r.GET(base+"/roles", h.require("menus:view", h.roles))
	r.GET(base+"/permissions", h.require("menus:view", h.permissions))
	r.GET(base+"/tree", h.require("menus:view", h.tree))
	r.GET(base, h.require("menus:view", h.list))
	r.POST(base, h.require("menus:create", h.create))
	r.PUT(base+"/reorder", h.require("menus:update", h.reorder))
	r.GET(base+"/:id", h.require("menus:view", h.get))
	r.PUT(base+"/:id", h.require("menus:update", h.update))
	r.DELETE(base+"/:id", h.require("menus:delete", h.delete))
	r.PUT(base+"/:id/roles", h.require("menus:assign_roles", h.assignRoles))
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
		user, err := h.auth.Me(ctx, cookies[0].Value)
		if err != nil {
			if errors.Is(err, auth.ErrUnauthorized) {
				response.Reject(c, response.Unauthenticated)
			} else {
				response.Reject(c, response.Unavailable)
			}
			return
		}
		if permission != "" {
			allowed, err := h.service.repo.Has(ctx, user.ID, permission)
			if err != nil {
				response.Reject(c, response.Unavailable)
				return
			}
			if !allowed {
				response.Reject(c, response.Forbidden)
				return
			}
		}
		next(c, auditlog.WithRequest(ctx, c.Request.Method, c.FullPath(), c.ClientIP(), c.Request.UserAgent()), user.ID)
	}
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
	case errors.Is(err, ErrForbidden):
		response.Reject(c, response.Forbidden)
	default:
		response.Reject(c, response.Unavailable)
	}
}
func (h *Handler) my(c *gin.Context, ctx context.Context, actor string) {
	v, e := h.service.My(ctx, actor)
	if e != nil {
		reject(c, e)
		return
	}
	response.Success(c, v)
}
func (h *Handler) list(c *gin.Context, ctx context.Context, _ string) {
	v, e := h.service.List(ctx)
	if e != nil {
		reject(c, e)
		return
	}
	response.Success(c, v)
}
func (h *Handler) tree(c *gin.Context, ctx context.Context, _ string) {
	v, e := h.service.Tree(ctx)
	if e != nil {
		reject(c, e)
		return
	}
	response.Success(c, v)
}
func (h *Handler) get(c *gin.Context, ctx context.Context, _ string) {
	v, e := h.service.Get(ctx, c.Param("id"))
	if e != nil {
		reject(c, e)
		return
	}
	response.Success(c, v)
}
func (h *Handler) create(c *gin.Context, ctx context.Context, actor string) {
	var in Input
	if e := decode(c, &in); e != nil {
		reject(c, e)
		return
	}
	v, e := h.service.Create(ctx, actor, in)
	if e != nil {
		reject(c, e)
		return
	}
	response.Success(c, v)
}
func (h *Handler) update(c *gin.Context, ctx context.Context, actor string) {
	var in Input
	if e := decode(c, &in); e != nil {
		reject(c, e)
		return
	}
	v, e := h.service.Update(ctx, actor, c.Param("id"), in)
	if e != nil {
		reject(c, e)
		return
	}
	response.Success(c, v)
}
func (h *Handler) delete(c *gin.Context, ctx context.Context, actor string) {
	if e := h.service.Delete(ctx, actor, c.Param("id")); e != nil {
		reject(c, e)
		return
	}
	response.Success(c, nil)
}
func (h *Handler) assignRoles(c *gin.Context, ctx context.Context, actor string) {
	var in struct {
		RoleIDs []string `json:"role_ids"`
	}
	if e := decode(c, &in); e != nil {
		reject(c, e)
		return
	}
	if e := h.service.SetRoles(ctx, actor, c.Param("id"), in.RoleIDs); e != nil {
		reject(c, e)
		return
	}
	response.Success(c, nil)
}
func (h *Handler) reorder(c *gin.Context, ctx context.Context, actor string) {
	var in struct {
		Items []ReorderItem `json:"items"`
	}
	if e := decode(c, &in); e != nil {
		reject(c, e)
		return
	}
	if e := h.service.Reorder(ctx, actor, in.Items); e != nil {
		reject(c, e)
		return
	}
	response.Success(c, nil)
}
func (h *Handler) roles(c *gin.Context, ctx context.Context, _ string) {
	v, e := h.service.Roles(ctx)
	if e != nil {
		reject(c, e)
		return
	}
	response.Success(c, v)
}

func (h *Handler) permissions(c *gin.Context, ctx context.Context, _ string) {
	v, e := h.service.Permissions(ctx)
	if e != nil {
		reject(c, e)
		return
	}
	response.Success(c, v)
}
