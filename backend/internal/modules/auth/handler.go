package auth

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"goadminkit/backend/internal/core/response"
	"goadminkit/backend/internal/core/security"
)

type Handler struct {
	service *Service
	guard   *security.Guard
}

func RegisterRoutes(r *gin.Engine, s *Service, g *security.Guard) {
	h := &Handler{s, g}
	r.POST("/api/v1/auth/login", h.login)
	r.GET("/api/v1/auth/me", h.me)
	r.POST("/api/v1/auth/logout", h.logout)
	r.POST("/api/v1/auth/logout-all", h.logoutAll)
}
func token(c *gin.Context) string {
	cookies := c.Request.CookiesNamed(security.SessionCookie)
	if len(cookies) != 1 {
		return ""
	}
	return cookies[0].Value
}
func reject(c *gin.Context, err error) {
	var limited *Limited
	switch {
	case errors.Is(err, ErrInvalid):
		response.Reject(c, response.BadRequest)
	case errors.Is(err, ErrCredentials):
		response.Error(c, 401, 40101, "Invalid username or password")
	case errors.Is(err, ErrUnauthorized):
		response.Reject(c, response.Unauthenticated)
	case errors.As(err, &limited):
		c.Header("Retry-After", strconv.Itoa(max(1, int(math.Ceil(limited.Retry.Seconds())))))
		response.Reject(c, response.TooManyRequests)
	default:
		response.Reject(c, response.Unavailable)
	}
}
func requestContext(c *gin.Context) (context.Context, context.CancelFunc) {
	c.Header("Cache-Control", "no-store")
	return context.WithTimeout(c.Request.Context(), 4*time.Second)
}
func (h *Handler) login(c *gin.Context) {
	ctx, cancel := requestContext(c)
	defer cancel()
	var input struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if c.ContentType() != "application/json" {
		reject(c, ErrInvalid)
		return
	}
	decoder := json.NewDecoder(http.MaxBytesReader(c.Writer, c.Request.Body, 8192))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&input) != nil || decoder.Decode(&struct{}{}) != io.EOF {
		reject(c, ErrInvalid)
		return
	}
	u, raw, err := h.service.LoginWithContext(ctx, input.Username, input.Password, c.ClientIP(), c.Request.UserAgent())
	if err != nil {
		reject(c, err)
		return
	}
	h.guard.SetSession(c.Writer, raw)
	response.Success(c, gin.H{"user": u})
}
func (h *Handler) me(c *gin.Context) {
	ctx, cancel := requestContext(c)
	defer cancel()
	u, err := h.service.Me(ctx, token(c))
	if err != nil {
		if errors.Is(err, ErrUnauthorized) {
			h.guard.ClearSession(c.Writer)
		}
		reject(c, err)
		return
	}
	response.Success(c, gin.H{"user": u})
}
func (h *Handler) logout(c *gin.Context) {
	ctx, cancel := requestContext(c)
	defer cancel()
	if err := h.service.Logout(ctx, token(c)); err != nil {
		reject(c, err)
		return
	}
	h.guard.ClearSession(c.Writer)
	response.Success(c, nil)
}
func (h *Handler) logoutAll(c *gin.Context) {
	ctx, cancel := requestContext(c)
	defer cancel()
	if err := h.service.LogoutAll(ctx, token(c)); err != nil {
		reject(c, err)
		return
	}
	h.guard.ClearSession(c.Writer)
	response.Success(c, nil)
}
