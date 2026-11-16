package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/lib/pq"

	"goadminkit/backend/internal/core/config"
	"goadminkit/backend/migrations"
)

func main() {
	if len(os.Args) > 2 || (len(os.Args) == 2 && os.Args[1] != "up") {
		fmt.Fprintln(os.Stderr, "usage: migrate [up]; destructive rollbacks are intentionally not exposed")
		os.Exit(1)
	}
	cfg, err := config.Load()
	if err == nil {
		err = run(cfg, os.LookupEnv)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println("migrations current")
}

func run(cfg config.Config, lookup func(string) (string, bool)) error {
	latest, err := migrations.LatestVersion()
	if err != nil {
		return errors.New("embedded migrations unavailable")
	}
	// Require the exact destination before opening a production database.
	if cfg.Environment == "production" && !matches(lookup, "MIGRATION_CONFIRM_DATABASE", cfg.DBName) {
		return errors.New("production migration requires explicit target database confirmation")
	}
	current, err := inspect(cfg.DatabaseURL())
	if err != nil {
		return err
	}
	if err := authorize(current, latest, cfg.Environment, cfg.DBName, lookup); err != nil {
		return err
	}
	if current == latest {
		return nil
	}
	return migrations.Up(cfg.DatabaseURL())
}

func inspect(dsn string) (uint, error) {
	m, err := migrations.New(dsn)
	if err != nil {
		return 0, errors.New("cannot inspect migration state")
	}
	defer m.Close()
	current, dirty, err := m.Version()
	if errors.Is(err, migrate.ErrNilVersion) {
		current, err = 0, nil
	}
	if err != nil || dirty {
		return 0, errors.New("migration state is dirty or unavailable")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return 0, errors.New("cannot inspect core schema metadata")
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var exists bool
	if err := db.QueryRowContext(ctx, `SELECT to_regclass('app_metadata') IS NOT NULL`).Scan(&exists); err != nil {
		return 0, errors.New("cannot inspect core schema metadata")
	}
	if !exists {
		if current == 0 {
			return 0, nil
		}
		return 0, errors.New("core metadata missing for existing migration")
	}
	var core uint
	if err := db.QueryRowContext(ctx, `SELECT schema_version FROM app_metadata WHERE component='core'`).Scan(&core); err != nil || core != current {
		return 0, errors.New("core metadata and migration state do not match")
	}
	return current, nil
}

func matches(lookup func(string) (string, bool), key, value string) bool {
	v, ok := lookup(key)
	return ok && v == value
}

func authorize(current, latest uint, environment, database string, lookup func(string) (string, bool)) error {
	if current > latest {
		return errors.New("database schema is newer than this binary")
	}
	if current == latest {
		return nil
	}
	if current == 0 && environment != "production" {
		return nil // Preserve fresh local and isolated-test initialization.
	}
	from, okFrom := lookup("MIGRATION_CONFIRM_FROM_VERSION")
	to, okTo := lookup("MIGRATION_CONFIRM_TO_VERSION")
	if !matches(lookup, "MIGRATION_CONFIRM_DATABASE", database) || !okFrom || !okTo || from != strconv.FormatUint(uint64(current), 10) || to != strconv.FormatUint(uint64(latest), 10) {
		return errors.New("migration upgrade requires exact database, current and target version confirmations")
	}
	if current > 0 {
		ref, ok := lookup("MIGRATION_BACKUP_REFERENCE")
		if !ok || strings.TrimSpace(ref) == "" {
			return errors.New("existing database upgrade requires a verified backup reference")
		}
	}
	return nil
}
