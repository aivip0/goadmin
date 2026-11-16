package middleware

import (
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"time"

	"github.com/gin-gonic/gin"
	"goadminkit/backend/internal/core/response"
)

func RequestLog(log *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		var bytes [16]byte
		_, _ = rand.Read(bytes[:])
		id := hex.EncodeToString(bytes[:])
		c.Header("X-Request-ID", id)
		c.Header("X-Content-Type-Options", "nosniff")
		start := time.Now()
		c.Next()
		// Do not log query strings, request bodies, cookies or authorization headers.
		log.Info("http request", "request_id", id, "method", c.Request.Method, "path", c.Request.URL.Path, "status", c.Writer.Status(), "duration_ms", time.Since(start).Milliseconds())
	}
}

func Recovery(log *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if recover() != nil {
				log.Error("http panic recovered")
				response.Error(c, 500, 50000, "Internal server error")
			}
		}()
		c.Next()
	}
}
