package migrations

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"strings"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	_ "github.com/lib/pq"
)

//go:embed *.up.sql
var Files embed.FS

func UpDSN(ctx context.Context, service string, dsn string) error {
	dsn = strings.TrimSpace(dsn)
	if dsn == "" {
		return errors.New("postgres dsn is required")
	}
	sourceDriver, err := iofs.New(Files, ".")
	if err != nil {
		return fmt.Errorf("%s: load migrations: %w", service, err)
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return fmt.Errorf("%s: open postgres: %w", service, err)
	}
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return fmt.Errorf("%s: ping postgres: %w", service, err)
	}
	databaseDriver, err := postgres.WithInstance(db, &postgres.Config{})
	if err != nil {
		_ = db.Close()
		return fmt.Errorf("%s: migration database driver: %w", service, err)
	}
	runner, err := migrate.NewWithInstance("iofs", sourceDriver, "postgres", databaseDriver)
	if err != nil {
		_ = db.Close()
		return fmt.Errorf("%s: create migration runner: %w", service, err)
	}
	err = runner.Up()
	sourceErr, databaseErr := runner.Close()
	if err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("%s: migrate up: %w", service, err)
	}
	if sourceErr != nil {
		return fmt.Errorf("%s: close migration source: %w", service, sourceErr)
	}
	if databaseErr != nil {
		return fmt.Errorf("%s: close migration database: %w", service, databaseErr)
	}
	fmt.Printf("%s migrations ok\n", service)
	return nil
}
