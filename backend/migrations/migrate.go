package migrations

import (
	"embed"
	"errors"
	"io/fs"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"
)

//go:embed *.sql
var files embed.FS

// Enumerate through golang-migrate's source driver, not a duplicated version constant.
func LatestVersion() (uint, error) {
	source, err := iofs.New(files, ".")
	if err != nil {
		return 0, err
	}
	defer source.Close()
	version, err := source.First()
	if err != nil {
		return 0, err
	}
	for {
		next, err := source.Next(version)
		if errors.Is(err, fs.ErrNotExist) {
			return version, nil
		}
		if err != nil {
			return 0, err
		}
		version = next
	}
}

func New(databaseURL string) (*migrate.Migrate, error) {
	source, err := iofs.New(files, ".")
	if err != nil {
		return nil, err
	}
	m, err := migrate.NewWithSourceInstance("iofs", source, databaseURL)
	if err != nil {
		_ = source.Close()
		return nil, err
	}
	return m, nil
}

func Up(databaseURL string) error {
	m, err := New(databaseURL)
	if err != nil {
		return errors.New("initialize migrations failed; check database configuration")
	}
	err = m.Up()
	sourceErr, databaseErr := m.Close()
	if err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return errors.New("migration failed; inspect schema version and migration SQL before retrying")
	}
	return errors.Join(sourceErr, databaseErr)
}
