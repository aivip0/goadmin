package iam

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
	auditlog "goadminkit/backend/internal/core/audit"
	"goadminkit/backend/internal/core/response"
	"goadminkit/backend/internal/core/security"
	"goadminkit/backend/internal/modules/auth"
)

type Handler struct {
	service *Service
	auth    *auth.Service
}

func RegisterRoutes(r *gin.Engine, service *Service, identity *auth.Service) {
	h := &Handler{service, identity}
	base := "/api/v1/admin"
	r.GET(base+"/permissions/effective", h.require("", h.effective))
	r.GET(base+"/permissions", h.require("permissions:view", h.permissions))
	r.GET(base+"/permissions/grouped", h.require("permissions:view", h.grouped))
	r.GET(base+"/users", h.require("users:view", h.users))
	r.POST(base+"/users", h.require("users:create", h.createUser))
	r.GET(base+"/users/:id", h.require("users:view", h.user))
	r.PUT(base+"/users/:id", h.require("users:update", h.updateUser))
	r.PUT(base+"/users/:id/password", h.require("users:update", h.password))
	r.DELETE(base+"/users/:id", h.require("users:delete", h.deleteUser))
	r.POST(base+"/users/:id/enable", h.require("users:update", h.enable))
	r.POST(base+"/users/:id/disable", h.require("users:update", h.disable))
	r.PUT(base+"/users/:id/roles", h.require("users:assign_roles", h.userRoles))
	r.GET(base+"/roles", h.require("roles:view", h.roles))
	r.POST(base+"/roles", h.require("roles:create", h.createRole))
	r.GET(base+"/roles/:id", h.require("roles:view", h.role))
	r.PUT(base+"/roles/:id", h.require("roles:update", h.updateRole))
	r.DELETE(base+"/roles/:id", h.require("roles:delete", h.deleteRole))
	r.PUT(base+"/roles/:id/permissions", h.require("roles:assign_permissions", h.rolePermissions))
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

type action func(*gin.Context, context.Context, string)

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
			allowed, err := h.service.Has(ctx, user.ID, permission)
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
func decode(c *gin.Context, dst any) error {
	if c.ContentType() != "application/json" {
		return ErrInvalid
	}
	d := json.NewDecoder(http.MaxBytesReader(c.Writer, c.Request.Body, 16384))
	d.DisallowUnknownFields()
	if d.Decode(dst) != nil || d.Decode(&struct{}{}) != io.EOF {
		return ErrInvalid
	}
	return nil
}
func (h *Handler) effective(c *gin.Context, ctx context.Context, actor string) {
	list, super, err := h.service.Effective(ctx, actor)
	if err != nil {
		reject(c, err)
		return
	}
	response.Success(c, gin.H{"permissions": list, "is_super_admin": super})
}
func (h *Handler) permissions(c *gin.Context, ctx context.Context, _ string) {
	list, err := h.service.Permissions(ctx)
	if err != nil {
		reject(c, err)
		return
	}
	response.Success(c, list)
}
func (h *Handler) grouped(c *gin.Context, ctx context.Context, _ string) {
	list, err := h.service.Permissions(ctx)
	if err != nil {
		reject(c, err)
		return
	}
	groups := map[string][]Permission{}
	for _, p := range list {
		key := strings.SplitN(p.Code, ":", 2)[0]
		groups[key] = append(groups[key], p)
	}
	response.Success(c, groups)
}
func (h *Handler) users(c *gin.Context, ctx context.Context, _ string) {
	page, size := 1, 20
	var err error
	if text := c.Query("page"); text != "" {
		page, err = strconv.Atoi(text)
		if err != nil {
			reject(c, ErrInvalid)
			return
		}
	}
	if text := c.Query("page_size"); text != "" {
		size, err = strconv.Atoi(text)
		if err != nil {
			reject(c, ErrInvalid)
			return
		}
	}
	search, status := strings.TrimSpace(c.Query("username")), c.Query("status")
	if page < 1 || page > 100000 || size < 1 || size > 100 || len(search) > 64 || (status != "" && status != "active" && status != "disabled") {
		reject(c, ErrInvalid)
		return
	}
	list, err := h.service.Users(ctx, search, status, page, size)
	if err != nil {
		reject(c, err)
		return
	}
	response.Success(c, list)
}
func (h *Handler) user(c *gin.Context, ctx context.Context, _ string) {
	result, err := h.service.User(ctx, c.Param("id"))
	if err != nil {
		reject(c, err)
		return
	}
	response.Success(c, result)
}
func (h *Handler) createUser(c *gin.Context, ctx context.Context, actor string) {
	var in UserInput
	if err := decode(c, &in); err != nil {
		reject(c, err)
		return
	}
	u, err := h.service.CreateUser(ctx, actor, in)
	if err != nil {
		reject(c, err)
		return
	}
	response.Success(c, u)
}
func (h *Handler) updateUser(c *gin.Context, ctx context.Context, actor string) {
	var in UserInput
	if err := decode(c, &in); err != nil {
		reject(c, err)
		return
	}
	u, err := h.service.UpdateUser(ctx, actor, c.Param("id"), in)
	if err != nil {
		reject(c, err)
		return
	}
	response.Success(c, u)
}
func (h *Handler) password(c *gin.Context, ctx context.Context, actor string) {
	var in struct {
		Password string `json:"password"`
	}
	if err := decode(c, &in); err != nil {
		reject(c, err)
		return
	}
	if err := h.service.ChangePassword(ctx, actor, c.Param("id"), in.Password); err != nil {
		reject(c, err)
		return
	}
	response.Success(c, nil)
}
func (h *Handler) deleteUser(c *gin.Context, ctx context.Context, actor string) {
	if err := h.service.DeleteUser(ctx, actor, c.Param("id")); err != nil {
		reject(c, err)
		return
	}
	response.Success(c, nil)
}
func (h *Handler) enable(c *gin.Context, ctx context.Context, actor string) {
	h.status(c, ctx, actor, "active")
}
func (h *Handler) disable(c *gin.Context, ctx context.Context, actor string) {
	h.status(c, ctx, actor, "disabled")
}
func (h *Handler) status(c *gin.Context, ctx context.Context, actor, status string) {
	if err := h.service.SetUserStatus(ctx, actor, c.Param("id"), status); err != nil {
		reject(c, err)
		return
	}
	response.Success(c, nil)
}
func (h *Handler) userRoles(c *gin.Context, ctx context.Context, actor string) {
	var in struct {
		RoleIDs []string `json:"role_ids"`
	}
	if err := decode(c, &in); err != nil {
		reject(c, err)
		return
	}
	if err := h.service.SetUserRoles(ctx, actor, c.Param("id"), in.RoleIDs); err != nil {
		reject(c, err)
		return
	}
	response.Success(c, nil)
}
func (h *Handler) roles(c *gin.Context, ctx context.Context, _ string) {
	list, err := h.service.Roles(ctx)
	if err != nil {
		reject(c, err)
		return
	}
	response.Success(c, list)
}
func (h *Handler) role(c *gin.Context, ctx context.Context, _ string) {
	item, err := h.service.Role(ctx, c.Param("id"))
	if err != nil {
		reject(c, err)
		return
	}
	response.Success(c, item)
}
func (h *Handler) createRole(c *gin.Context, ctx context.Context, actor string) {
	var in RoleInput
	if err := decode(c, &in); err != nil {
		reject(c, err)
		return
	}
	item, err := h.service.CreateRole(ctx, actor, in)
	if err != nil {
		reject(c, err)
		return
	}
	response.Success(c, item)
}
func (h *Handler) updateRole(c *gin.Context, ctx context.Context, actor string) {
	var in RoleInput
	if err := decode(c, &in); err != nil {
		reject(c, err)
		return
	}
	item, err := h.service.UpdateRole(ctx, actor, c.Param("id"), in)
	if err != nil {
		reject(c, err)
		return
	}
	response.Success(c, item)
}
func (h *Handler) deleteRole(c *gin.Context, ctx context.Context, actor string) {
	if err := h.service.DeleteRole(ctx, actor, c.Param("id")); err != nil {
		reject(c, err)
		return
	}
	response.Success(c, nil)
}
func (h *Handler) rolePermissions(c *gin.Context, ctx context.Context, actor string) {
	var in struct {
		PermissionIDs []string `json:"permission_ids"`
	}
	if err := decode(c, &in); err != nil {
		reject(c, err)
		return
	}
	if err := h.service.SetRolePermissions(ctx, actor, c.Param("id"), in.PermissionIDs); err != nil {
		reject(c, err)
		return
	}
	response.Success(c, nil)
}
