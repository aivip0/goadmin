package router

import (
	"context"
	"log/slog"
	"time"

	"github.com/gin-gonic/gin"
	"goadminkit/backend/api"
	"goadminkit/backend/internal/core/middleware"
	"goadminkit/backend/internal/core/response"
	"goadminkit/backend/internal/core/security"
)

type Readiness interface{ Ready(context.Context) error }

func New(db Readiness, log *slog.Logger, timeout time.Duration, guards ...*security.Guard) *gin.Engine {
	r := gin.New()
	_ = r.SetTrustedProxies(nil)
	r.HandleMethodNotAllowed = true
	r.Use(middleware.RequestLog(log), middleware.Recovery(log))
	if len(guards) > 0 {
		r.Use(guards[0].Protect())
		r.GET("/api/v1/auth/csrf", guards[0].IssueCSRF)
	}
	r.GET("/api/v1/health/live", func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		response.Success(c, gin.H{"status": "ok"})
	})
	ready := func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		ctx, cancel := context.WithTimeout(c.Request.Context(), timeout)
		defer cancel()
		if err := db.Ready(ctx); err != nil {
			response.Error(c, 503, 50301, "Service not ready")
			return
		}
		response.Success(c, gin.H{"status": "ok", "database": "connected", "migration": "current", "checked_at": time.Now().UTC().Format(time.RFC3339Nano)})
	}
	r.GET("/api/v1/health", ready)
	r.GET("/api/v1/health/ready", ready)
	r.GET("/api/v1/openapi.json", func(c *gin.Context) { c.Data(200, "application/json; charset=utf-8", api.OpenAPI) })
	r.NoRoute(func(c *gin.Context) { response.Error(c, 404, 40400, "Resource not found") })
	r.NoMethod(func(c *gin.Context) { response.Error(c, 405, 40500, "Method not allowed") })
	return r
}
