package profile

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
)

type Handler struct {
	service *Service
	guard   *security.Guard
}

func RegisterRoutes(r *gin.Engine, s *Service, g *security.Guard) {
	h := &Handler{s, g}
	r.GET("/api/v1/admin/profile", h.get)
	r.PUT("/api/v1/admin/profile", h.update)
	r.PUT("/api/v1/admin/profile/password", h.password)
	r.GET("/api/v1/admin/profile/sessions", h.sessions)
	r.DELETE("/api/v1/admin/profile/sessions/:id", h.revoke)
	r.POST("/api/v1/admin/profile/sessions/revoke-others", h.others)
}
func request(c *gin.Context) (context.Context, context.CancelFunc, string) {
	c.Header("Cache-Control", "no-store")
	ctx, cancel := context.WithTimeout(audit.WithRequest(c.Request.Context(), c.Request.Method, c.FullPath(), c.ClientIP(), c.Request.UserAgent()), 4*time.Second)
	cookies := c.Request.CookiesNamed(security.SessionCookie)
	if len(cookies) != 1 {
		return ctx, cancel, ""
	}
	return ctx, cancel, cookies[0].Value
}
func decode(c *gin.Context, v any) bool {
	if c.ContentType() != "application/json" {
		return false
	}
	d := json.NewDecoder(http.MaxBytesReader(c.Writer, c.Request.Body, 8192))
	d.DisallowUnknownFields()
	return d.Decode(v) == nil && d.Decode(&struct{}{}) == io.EOF
}
func (h *Handler) finish(c *gin.Context, data any, err error) {
	if err == nil {
		response.Success(c, data)
		return
	}
	var limit *auth.Limited
	switch {
	case errors.Is(err, auth.ErrUnauthorized):
		h.guard.ClearSession(c.Writer)
		response.Reject(c, response.Unauthenticated)
	case errors.Is(err, ErrInvalid):
		response.Reject(c, response.BadRequest)
	case errors.Is(err, ErrPassword):
		response.Reject(c, response.Unprocessable)
	case errors.Is(err, ErrConflict):
		response.Reject(c, response.Conflict)
	case errors.Is(err, ErrNotFound):
		response.Reject(c, response.NotFound)
	case errors.As(err, &limit):
		c.Header("Retry-After", "60")
		response.Reject(c, response.TooManyRequests)
	default:
		response.Reject(c, response.Unavailable)
	}
}
func (h *Handler) get(c *gin.Context) {
	ctx, cancel, t := request(c)
	defer cancel()
	u, err := h.service.Get(ctx, t)
	h.finish(c, gin.H{"user": u}, err)
}
func (h *Handler) update(c *gin.Context) {
	ctx, cancel, t := request(c)
	defer cancel()
	var in Input
	if !decode(c, &in) {
		h.finish(c, nil, ErrInvalid)
		return
	}
	u, err := h.service.Update(ctx, t, in)
	h.finish(c, gin.H{"user": u}, err)
}
func (h *Handler) password(c *gin.Context) {
	ctx, cancel, t := request(c)
	defer cancel()
	var in PasswordInput
	if !decode(c, &in) {
		h.finish(c, nil, ErrInvalid)
		return
	}
	err := h.service.Password(ctx, t, in)
	if err == nil {
		h.guard.ClearSession(c.Writer)
	}
	h.finish(c, nil, err)
}
func (h *Handler) sessions(c *gin.Context) {
	ctx, cancel, t := request(c)
	defer cancel()
	p, e := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, e2 := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if e != nil || e2 != nil {
		h.finish(c, nil, ErrInvalid)
		return
	}
	out, err := h.service.Sessions(ctx, t, p, size)
	h.finish(c, out, err)
}
func (h *Handler) revoke(c *gin.Context) {
	ctx, cancel, t := request(c)
	defer cancel()
	current, err := h.service.Revoke(ctx, t, c.Param("id"), false)
	if err == nil && current {
		h.guard.ClearSession(c.Writer)
	}
	h.finish(c, gin.H{"current_revoked": current}, err)
}
func (h *Handler) others(c *gin.Context) {
	ctx, cancel, t := request(c)
	defer cancel()
	_, err := h.service.Revoke(ctx, t, "", true)
	h.finish(c, nil, err)
}
