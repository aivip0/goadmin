package media

import (
	"context"
	"net/http"
	"time"
)

type writeDeadlineKey struct{}

// Connection deadlines, unlike handler contexts, constrain actual network I/O.
func DeadlineHandler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		read, write := deadlineBudget(r.URL.Path, r.Method)
		controller := http.NewResponseController(w)
		if controller.SetReadDeadline(time.Now().Add(read)) != nil || controller.SetWriteDeadline(time.Now().Add(write)) != nil {
			http.Error(w, "service unavailable", http.StatusServiceUnavailable)
			return
		}
		ctx := context.WithValue(r.Context(), writeDeadlineKey{}, controller)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func deadlineBudget(path, method string) (time.Duration, time.Duration) {
	if path == "/api/v1/admin/media" && method == http.MethodPost {
		return 65 * time.Second, 75 * time.Second
	}
	const prefix = "/api/v1/admin/media/"
	if len(path) == len(prefix)+44 && path[:len(prefix)] == prefix && method == http.MethodGet {
		item := path[len(prefix):]
		if item[36:] == "/content" && uuidName.MatchString(item[:36]) {
			return 10 * time.Second, 95 * time.Second
		}
	}
	return 10 * time.Second, 15 * time.Second
}

func writeDeadline(ctx context.Context, d time.Duration) error {
	controller, ok := ctx.Value(writeDeadlineKey{}).(*http.ResponseController)
	if !ok {
		return ErrNotReady
	}
	return controller.SetWriteDeadline(time.Now().Add(d))
}
