package bootstrap

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"goadminkit/backend/internal/core/config"
	"goadminkit/backend/internal/core/database"
	"goadminkit/backend/internal/core/security"
	"goadminkit/backend/internal/modules/apikeys"
	"goadminkit/backend/internal/modules/articles"
	"goadminkit/backend/internal/modules/auth"
	"goadminkit/backend/internal/modules/dashboard"
	"goadminkit/backend/internal/modules/iam"
	"goadminkit/backend/internal/modules/jobs"
	"goadminkit/backend/internal/modules/logs"
	"goadminkit/backend/internal/modules/media"
	"goadminkit/backend/internal/modules/menus"
	"goadminkit/backend/internal/modules/notifications"
	"goadminkit/backend/internal/modules/plugins"
	"goadminkit/backend/internal/modules/profile"
	"goadminkit/backend/internal/modules/settings"
	"goadminkit/backend/internal/router"
)

func Run(ctx context.Context, cfg config.Config, log *slog.Logger) error {
	return run(ctx, cfg, log, nil)
}

// RunWithPluginExtensions starts the same application with trusted, build-time
// plugin providers supplied by the embedding application. It does not load code
// from the plugin package directory.
func RunWithPluginExtensions(ctx context.Context, cfg config.Config, log *slog.Logger, extensions ...plugins.Extension) error {
	return run(ctx, cfg, log, extensions)
}

func run(ctx context.Context, cfg config.Config, log *slog.Logger, extensions []plugins.Extension) error {
	gin.SetMode(gin.ReleaseMode)
	db, err := database.Open(ctx, cfg)
	if err != nil {
		return err
	}
	defer db.SQL.Close()
	guard, err := security.New(cfg.Security)
	if err != nil {
		return err
	}
	r := router.New(db, log, cfg.HealthTimeout, guard)
	authService, err := auth.NewService(auth.NewRepository(db.SQL), cfg.Security.SessionTTL, cfg.SessionIdleTTL)
	if err != nil {
		return errors.New("authentication initialization failed")
	}
	auth.RegisterRoutes(r, authService, guard)
	rights := iam.NewService(iam.NewRepository(db.SQL))
	iam.RegisterRoutes(r, rights, authService)
	menus.RegisterRoutes(r, menus.NewService(db.SQL), authService)
	dashboardService := dashboard.NewService(dashboard.NewRepository(db.SQL))
	dashboard.RegisterRoutes(r, dashboardService, authService, rights, db)
	apikeys.RegisterRoutes(r, apikeys.NewService(apikeys.NewRepository(db.SQL)), authService, rights, dashboardService)
	settings.RegisterRoutes(r, settings.NewService(settings.NewRepository(db.SQL), cfg.SettingsEncryptionKey), authService, rights)
	logs.RegisterRoutes(r, logs.NewService(logs.NewRepository(db.SQL)), authService, rights)
	profile.RegisterRoutes(r, profile.NewService(profile.NewRepository(db.SQL, cfg.SessionIdleTTL)), guard)
	articles.RegisterRoutes(r, articles.NewService(articles.NewRepository(db.SQL)), authService, rights)
	notifications.RegisterRoutes(r, notifications.NewService(notifications.NewRepository(db.SQL)), authService, rights)
	jobService := jobs.NewService(jobs.NewRepository(db.SQL))
	jobs.RegisterRoutes(r, jobService, authService, rights)
	pluginService, e := plugins.NewService(plugins.NewRepository(db.SQL), cfg.PluginPackageRoot, cfg.PluginTrustedKeys, cfg.PluginConfigKey)
	if e != nil {
		return errors.New("plugin system initialization failed")
	}
	for _, extension := range extensions {
		if e = pluginService.RegisterExtension(extension); e != nil {
			return errors.New("plugin extension registration failed")
		}
	}
	plugins.RegisterRoutes(r, pluginService, authService, rights)
	go func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				work, cancel := context.WithTimeout(ctx, 10*time.Second)
				if db.Ready(work) == nil {
					_, _ = jobService.RunOnce(work)
				}
				cancel()
			}
		}
	}()
	if cfg.MediaStorageRoot != "" {
		store, e := media.OpenLocal(cfg.MediaStorageRoot)
		if e != nil {
			return errors.New("private media root unavailable")
		}
		defer store.Close()
		store.SetMinFree(cfg.MediaMinFreeBytes)
		svc, e := media.NewService(ctx, media.NewRepository(db.SQL), store)
		if e != nil {
			return errors.New("media binding or verification unavailable")
		}
		defer svc.Close()
		media.RegisterRoutes(r, svc, authService, rights)
	}
	if err = r.SetTrustedProxies(cfg.Security.TrustedProxies); err != nil {
		return errors.New("invalid trusted proxy configuration")
	}
	r.RemoteIPHeaders = []string{"X-Forwarded-For"}
	srv := &http.Server{Addr: cfg.Address(), Handler: media.DeadlineHandler(r), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 75 * time.Second, WriteTimeout: 100 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 1 << 20}
	result := make(chan error, 1)
	go func() {
		log.Info("api listening", "address", cfg.Address(), "environment", cfg.Environment)
		result <- srv.ListenAndServe()
	}()
	select {
	case err := <-result:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			_ = srv.Close()
			return err
		}
		log.Info("api stopped")
		return nil
	}
}
