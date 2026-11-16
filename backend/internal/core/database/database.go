package database

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"goadminkit/backend/internal/core/config"
	"goadminkit/backend/migrations"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type Database struct {
	ORM *gorm.DB
	SQL *sql.DB
}

func Open(ctx context.Context, cfg config.Config) (*Database, error) {
	orm, err := gorm.Open(postgres.Open(cfg.DatabaseURL()), &gorm.Config{DisableAutomaticPing: true, Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		return nil, errors.New("initialize PostgreSQL connection failed")
	}
	pool, err := orm.DB()
	if err != nil {
		return nil, errors.New("initialize connection pool failed")
	}
	pool.SetMaxOpenConns(cfg.MaxOpen)
	pool.SetMaxIdleConns(cfg.MaxIdle)
	pool.SetConnMaxLifetime(30 * time.Minute)
	pool.SetConnMaxIdleTime(5 * time.Minute)
	checkCtx, cancel := context.WithTimeout(ctx, cfg.ConnectTimeout)
	defer cancel()
	if err := pool.PingContext(checkCtx); err != nil {
		_ = pool.Close()
		return nil, errors.New("PostgreSQL connection failed; check database availability and configuration")
	}
	return &Database{ORM: orm, SQL: pool}, nil
}

// Ready compares the authoritative golang-migrate state with this binary's embedded migrations.
func (d *Database) Ready(ctx context.Context) error {
	if err := d.SQL.PingContext(ctx); err != nil {
		return err
	}
	latest, err := migrations.LatestVersion()
	if err != nil {
		return errors.New("embedded migration source is invalid")
	}
	var version uint
	if err := d.ORM.WithContext(ctx).Raw("SELECT schema_version FROM app_metadata WHERE component = ?", "core").Scan(&version).Error; err != nil {
		return err
	}
	if version != latest {
		return errors.New("schema metadata version mismatch")
	}
	var state struct {
		Version uint
		Dirty   bool
	}
	result := d.ORM.WithContext(ctx).Raw("SELECT version, dirty FROM schema_migrations").Scan(&state)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 || state.Version != latest || state.Dirty {
		return errors.New("migration state is not current")
	}
	return nil
}
