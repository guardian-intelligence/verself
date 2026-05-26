package migrations

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"sort"
	"strings"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	_ "github.com/lib/pq"
)

//go:embed *.sql
var Files embed.FS

func Run(ctx context.Context, dsn string, service string) error {
	return UpDSN(ctx, service, dsn)
}

func UpDSN(ctx context.Context, service string, dsn string) error {
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return fmt.Errorf("%s: open migration database: %w", service, err)
	}
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return fmt.Errorf("%s: ping migration database: %w", service, err)
	}
	sourceDriver, err := iofs.New(Files, ".")
	if err != nil {
		_ = db.Close()
		return fmt.Errorf("%s: migration source: %w", service, err)
	}
	databaseDriver, err := postgres.WithInstance(db, &postgres.Config{})
	if err != nil {
		_ = db.Close()
		return fmt.Errorf("%s: migration database driver: %w", service, err)
	}
	runner, err := migrate.NewWithInstance("iofs", sourceDriver, "postgres", databaseDriver)
	if err != nil {
		_ = db.Close()
		return fmt.Errorf("%s: migration runner: %w", service, err)
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
	return nil
}

func RunCLI(ctx context.Context, args []string, service string) error {
	if len(args) != 1 || args[0] != "up" {
		return fmt.Errorf("usage: %s migrate up", service)
	}
	dsn := getenv("VERSELF_PG_DSN")
	if dsn == "" {
		return fmt.Errorf("VERSELF_PG_DSN is required")
	}
	return Run(ctx, dsn, service)
}

func Fingerprint() string {
	names := make([]string, 0)
	if err := fs.WalkDir(Files, ".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Type().IsRegular() && strings.HasSuffix(path, ".up.sql") {
			names = append(names, path)
		}
		return nil
	}); err != nil {
		panic(err)
	}
	sort.Strings(names)
	h := sha256.New()
	for _, name := range names {
		raw, err := Files.ReadFile(name)
		if err != nil {
			panic(err)
		}
		_, _ = h.Write([]byte(name))
		_, _ = h.Write([]byte{0})
		_, _ = h.Write(raw)
		_, _ = h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

func getenv(key string) string {
	return strings.TrimSpace(os.Getenv(key))
}
