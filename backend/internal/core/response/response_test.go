package response

import (
	"github.com/gin-gonic/gin"
	"net/http/httptest"
	"testing"
)

func TestPublicErrorCatalog(t *testing.T) {
	for _, tt := range []struct {
		kind   Kind
		status int
	}{
		{BadRequest, 400}, {Unauthenticated, 401}, {Forbidden, 403}, {NotFound, 404}, {Conflict, 409},
		{Unprocessable, 422}, {TooManyRequests, 429}, {Internal, 500}, {Unavailable, 503}, {CSRFInvalid, 403}, {Kind(999), 500},
	} {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		Reject(c, tt.kind)
		if w.Code != tt.status {
			t.Fatalf("kind %d returned %d", tt.kind, w.Code)
		}
	}
}
