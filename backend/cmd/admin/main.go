package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"goadminkit/backend/internal/core/config"
	"goadminkit/backend/internal/core/database"
	"goadminkit/backend/internal/modules/auth"
	"goadminkit/backend/internal/modules/media"
	"goadminkit/backend/internal/modules/menus"
)

func run() error {
	if len(os.Args) != 2 || (os.Args[1] != "create" && os.Args[1] != "menus-init" && os.Args[1] != "media-bind" && os.Args[1] != "media-reconcile" && os.Args[1] != "media-retry") {
		return errors.New("usage: admin create|menus-init|media-bind|media-reconcile|media-retry; recovery confirmation must use environment variables, never arguments")
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	fmt.Printf("Administrator initialization target: host=%s port=%d database=%s user=%s\n", cfg.DBHost, cfg.DBPort, cfg.DBName, cfg.DBUser)
	if os.Getenv("ADMIN_INIT_DATABASE") != cfg.DBName {
		return errors.New("explicit ADMIN_INIT_DATABASE must equal POSTGRES_DB; this command never migrates the database")
	}
	input := auth.InitialAdmin{Username: os.Getenv("ADMIN_INIT_USERNAME"), Email: os.Getenv("ADMIN_INIT_EMAIL"), Password: os.Getenv("ADMIN_INIT_PASSWORD"), DisplayName: os.Getenv("ADMIN_INIT_DISPLAY_NAME")}
	os.Unsetenv("ADMIN_INIT_PASSWORD")
	budget := 15 * time.Second
	if os.Args[1] == "media-reconcile" || os.Args[1] == "media-retry" {
		budget = 45 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), budget)
	defer cancel()
	db, err := database.Open(ctx, cfg)
	if err != nil {
		return err
	}
	defer db.SQL.Close()
	if db.Ready(ctx) != nil {
		return errors.New("database schema is not current; obtain approval and back up before applying migrations separately")
	}
	if os.Args[1] == "media-bind" || os.Args[1] == "media-reconcile" || os.Args[1] == "media-retry" {
		if cfg.MediaStorageRoot == "" {
			return errors.New("MEDIA_STORAGE_ROOT must identify a private root")
		}
		store, e := media.OpenLocal(cfg.MediaStorageRoot)
		if e != nil {
			return errors.New("media root unavailable")
		}
		defer store.Close()
		store.SetMinFree(cfg.MediaMinFreeBytes)
		repo := media.NewRepository(db.SQL)
		if os.Args[1] == "media-bind" {
			if os.Getenv("MEDIA_BIND_CONFIRM_DATABASE") != cfg.DBName {
				return errors.New("MEDIA_BIND_CONFIRM_DATABASE must equal POSTGRES_DB")
			}
			actor, e := menus.NewRepository(db.SQL).BootstrapActor(ctx)
			if e != nil {
				return errors.New("active bootstrap administrator required")
			}
			if e = media.BindRoot(ctx, repo, store, actor); e != nil {
				return errors.New("media root binding failed; inspect marker and database without retrying automatically")
			}
			fmt.Println("Private media root bound to confirmed database.")
			return nil
		}
		actor, e := menus.NewRepository(db.SQL).BootstrapActor(ctx)
		if e != nil {
			return errors.New("active bootstrap administrator required")
		}
		if os.Args[1] == "media-retry" {
			id := os.Getenv("MEDIA_RETRY_FILE_ID")
			expected := os.Getenv("MEDIA_RETRY_EXPECTED_STATUS")
			if os.Getenv("MEDIA_RETRY_CONFIRM_DATABASE") != cfg.DBName || os.Getenv("MEDIA_RETRY_CONFIRM_ID") != id || id == "" {
				return errors.New("explicit MEDIA_RETRY_CONFIRM_DATABASE and MEDIA_RETRY_CONFIRM_ID must match target")
			}
			if e = media.ValidateMaintenanceRoot(ctx, repo, store); e != nil {
				return errors.New("media root binding or verification gate unavailable")
			}
			if e = repo.RetryOne(ctx, actor, id, expected); e != nil {
				return errors.New("exact-state media retry refused; inspect row and active claim")
			}
			fmt.Println("Audited media retry scheduled for confirmed record.")
			return nil
		}
		report, e := media.Reconcile(ctx, repo, store, actor)
		if e != nil {
			return errors.New("media reconcile failed; inspect private storage and database state")
		}
		fmt.Printf("Media reconcile: processed=%d failed=%d staging_cleaned=%d objects_scanned=%d orphans_isolated=%d\n", report.Processed, report.Failed, report.StagingCleaned, report.ObjectsScanned, report.OrphansIsolated)
		return nil
	}
	if os.Args[1] == "menus-init" {
		if os.Getenv("ADMIN_INIT_DATABASE") != cfg.DBName {
			return errors.New("explicit ADMIN_INIT_DATABASE must equal POSTGRES_DB")
		}
		svc := menus.NewService(db.SQL)
		if err := svc.InitializeBootstrap(ctx); err != nil {
			return errors.New("system menu initialization failed")
		}
		fmt.Println("Missing system menus initialized without changing existing menu settings.")
		return nil
	}
	s, err := auth.NewService(auth.NewRepository(db.SQL), cfg.Security.SessionTTL, cfg.SessionIdleTTL)
	if err != nil {
		return errors.New("authentication initialization failed")
	}
	u, err := s.Initialize(ctx, input)
	if errors.Is(err, auth.ErrConflict) || errors.Is(err, auth.ErrInvalid) {
		return err
	}
	if err != nil {
		return errors.New("administrator initialization failed; transaction rolled back")
	}
	if err = menus.NewService(db.SQL).Initialize(ctx, u.ID); err != nil {
		return errors.New("administrator created but system menu initialization failed; run admin menus-init")
	}
	fmt.Printf("Initial administrator created: id=%s username=%s\n", u.ID, u.Username)
	return nil
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
