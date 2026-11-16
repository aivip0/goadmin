package media

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
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

func RegisterRoutes(r *gin.Engine, s *Service, a *auth.Service, rights *iam.Service) {
	h := &Handler{s, a, rights}
	r.POST("/api/v1/admin/media", h.require("media:upload", h.upload))
	r.GET("/api/v1/admin/media", h.require("media:view", h.list))
	r.GET("/api/v1/admin/media/:id", h.require("media:view", h.get))
	r.GET("/api/v1/admin/media/:id/content", h.require("media:view", h.content))
	r.DELETE("/api/v1/admin/media/:id", h.require("media:delete", h.delete))
}

func reject(c *gin.Context, err error) {
	switch {
	case errors.Is(err, ErrInvalid):
		response.Reject(c, response.BadRequest)
	case errors.Is(err, ErrTooLarge):
		response.Reject(c, response.PayloadTooLarge)
	case errors.Is(err, ErrUnsupported):
		response.Reject(c, response.UnsupportedMedia)
	case errors.Is(err, ErrForbidden):
		response.Reject(c, response.Forbidden)
	case errors.Is(err, ErrNotFound):
		response.Reject(c, response.NotFound)
	case errors.Is(err, ErrConflict), errors.Is(err, ErrExists):
		response.Reject(c, response.Conflict)
	default:
		response.Reject(c, response.Unavailable)
	}
}
func (h *Handler) require(permission string, next func(*gin.Context, context.Context, string)) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		c.Header("X-Content-Type-Options", "nosniff")
		budget := 4 * time.Second
		if c.Request.Method == "POST" {
			budget = 60 * time.Second
		}
		if strings.HasSuffix(c.Request.URL.Path, "/content") {
			budget = 90 * time.Second
		}
		ctx, cancel := context.WithTimeout(c.Request.Context(), budget)
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
		next(c, ctx, u.ID)
	}
}
func (h *Handler) upload(c *gin.Context, ctx context.Context, actor string) {
	if c.ContentType() != "multipart/form-data" || c.Request.ContentLength > MaxMultipartSize {
		if c.Request.ContentLength > MaxMultipartSize {
			reject(c, ErrTooLarge)
		} else {
			reject(c, ErrInvalid)
		}
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, MaxMultipartSize)
	r, err := c.Request.MultipartReader()
	if err != nil {
		reject(c, ErrInvalid)
		return
	}
	f, err := h.service.Upload(ctx, actor, r)
	var maxErr *http.MaxBytesError
	if errors.As(err, &maxErr) {
		reject(c, ErrTooLarge)
		return
	}
	if err != nil {
		reject(c, err)
		return
	}
	response.Success(c, f)
}
func parseList(c *gin.Context) (int, int, string, string, error) {
	q := c.Request.URL.Query()
	for key, values := range q {
		if len(values) != 1 || (key != "page" && key != "page_size" && key != "type" && key != "q") {
			return 0, 0, "", "", ErrInvalid
		}
	}
	page, size := 1, 25
	var err error
	if q.Has("page") {
		page, err = strconv.Atoi(q.Get("page"))
		if err != nil {
			return 0, 0, "", "", ErrInvalid
		}
	}
	if q.Has("page_size") {
		size, err = strconv.Atoi(q.Get("page_size"))
		if err != nil {
			return 0, 0, "", "", ErrInvalid
		}
	}
	return page, size, q.Get("type"), q.Get("q"), nil
}
func (h *Handler) list(c *gin.Context, ctx context.Context, _ string) {
	page, size, kind, q, err := parseList(c)
	if err != nil {
		reject(c, err)
		return
	}
	items, total, err := h.service.List(ctx, page, size, kind, q)
	if err != nil {
		reject(c, err)
		return
	}
	response.Success(c, gin.H{"items": items, "total": total, "page": page, "page_size": size})
}
func (h *Handler) get(c *gin.Context, ctx context.Context, _ string) {
	f, err := h.service.Get(ctx, c.Param("id"))
	if err != nil {
		reject(c, err)
		return
	}
	response.Success(c, f)
}
func (h *Handler) content(c *gin.Context, ctx context.Context, _ string) {
	if c.GetHeader("Range") != "" {
		response.Reject(c, response.RangeNotSatisfiable)
		return
	}
	q := c.Request.URL.Query()
	if len(q) > 1 || (len(q) == 1 && (len(q["download"]) != 1 || q.Get("download") != "1")) {
		reject(c, ErrInvalid)
		return
	}
	prepare, cancel := context.WithTimeout(ctx, 40*time.Second)
	f, stream, err := h.service.Content(prepare, c.Param("id"))
	cancel()
	if err != nil {
		reject(c, err)
		return
	}
	defer stream.Close()
	inline := strings.HasPrefix(f.MIME, "image/") && !q.Has("download")
	c.Header("Content-Type", f.MIME)
	c.Header("Content-Length", strconv.FormatInt(f.Size, 10))
	c.Header("Content-Disposition", contentDisposition(f.OriginalName, inline))
	c.Header("Content-Security-Policy", "default-src 'none'; sandbox")
	c.Status(200)
	buf := make([]byte, 32<<10)
	for {
		if ctx.Err() != nil {
			return
		}
		n, e := stream.Read(buf)
		if n > 0 {
			if writeDeadline(c.Request.Context(), 15*time.Second) != nil {
				return
			}
			if _, writeErr := c.Writer.Write(buf[:n]); writeErr != nil {
				return
			}
		}
		if e == io.EOF {
			return
		}
		if e != nil {
			return
		}
	}
}
func (h *Handler) delete(c *gin.Context, ctx context.Context, actor string) {
	if err := h.service.Delete(ctx, actor, c.Param("id")); err != nil {
		reject(c, err)
		return
	}
	response.Success(c, nil)
}
