package pgmigrate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

type Config struct {
	Service string
	FS      fs.FS
	Dir     string
}

type migration struct {
	Version  string
	Filename string
	SQL      string
	Checksum string
}

type appliedMigration struct {
	Filename string
	Checksum string
}

var migrationFilePattern = regexp.MustCompile(`^([0-9]+)_.+\.up\.sql$`)

func RunCLI(ctx context.Context, args []string, cfg Config) error {
	if len(args) != 1 || args[0] != "up" {
		return errors.New("usage: migrate up")
	}
	return Up(ctx, cfg)
}

func Up(ctx context.Context, cfg Config) error {
	if cfg.Service == "" {
		return errors.New("migration service name is required")
	}
	if cfg.FS == nil {
		return errors.New("migration filesystem is required")
	}
	if cfg.Dir == "" {
		cfg.Dir = "."
	}
	migrations, err := loadMigrations(cfg)
	if err != nil {
		return err
	}
	if len(migrations) == 0 {
		return fmt.Errorf("%s: no *.up.sql migrations found", cfg.Service)
	}

	conn, err := pgx.Connect(ctx, migrationDSN(cfg))
	if err != nil {
		return fmt.Errorf("%s: connect postgres: %w", cfg.Service, err)
	}
	defer func() { _ = conn.Close(context.Background()) }()

	lockKey := "verself.pg_migrate." + cfg.Service
	if _, err := conn.Exec(ctx, "SELECT pg_advisory_lock(hashtextextended($1, 0))", lockKey); err != nil {
		return fmt.Errorf("%s: acquire migration lock: %w", cfg.Service, err)
	}
	defer unlock(conn, lockKey)

	if err := ensureMigrationTable(ctx, conn, cfg.Service); err != nil {
		return err
	}
	applied, err := readApplied(ctx, conn, cfg.Service)
	if err != nil {
		return err
	}

	appliedCount := 0
	skippedCount := 0
	for _, m := range migrations {
		if existing, ok := applied[m.Version]; ok {
			if existing.Filename != m.Filename || existing.Checksum != m.Checksum {
				return fmt.Errorf("%s: applied migration %s checksum drift: recorded %s %s, current %s %s", cfg.Service, m.Version, existing.Filename, existing.Checksum, m.Filename, m.Checksum)
			}
			skippedCount++
			continue
		}
		if err := applyMigration(ctx, conn, cfg.Service, m); err != nil {
			return err
		}
		appliedCount++
		fmt.Fprintf(os.Stdout, "applied %s\n", m.Filename)
	}
	fmt.Fprintf(os.Stdout, "%s migrations ok: applied=%d skipped=%d\n", cfg.Service, appliedCount, skippedCount)
	return nil
}

func migrationDSN(cfg Config) string {
	return strings.TrimSpace(os.Getenv("VERSELF_PG_DSN"))
}

func loadMigrations(cfg Config) ([]migration, error) {
	entries, err := fs.ReadDir(cfg.FS, cfg.Dir)
	if err != nil {
		return nil, fmt.Errorf("%s: read migrations: %w", cfg.Service, err)
	}
	out := make([]migration, 0, len(entries))
	seenVersions := map[string]string{}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		match := migrationFilePattern.FindStringSubmatch(name)
		if match == nil {
			continue
		}
		version := match[1]
		if previous := seenVersions[version]; previous != "" {
			return nil, fmt.Errorf("%s: duplicate migration version %s in %s and %s", cfg.Service, version, previous, name)
		}
		seenVersions[version] = name
		body, err := fs.ReadFile(cfg.FS, pathJoin(cfg.Dir, name))
		if err != nil {
			return nil, fmt.Errorf("%s: read migration %s: %w", cfg.Service, name, err)
		}
		sum := sha256.Sum256(body)
		out = append(out, migration{
			Version:  version,
			Filename: name,
			SQL:      string(body),
			Checksum: hex.EncodeToString(sum[:]),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if len(out[i].Version) != len(out[j].Version) {
			return len(out[i].Version) < len(out[j].Version)
		}
		return out[i].Version < out[j].Version
	})
	return out, nil
}

func pathJoin(dir, name string) string {
	if dir == "." || dir == "" {
		return name
	}
	return strings.TrimRight(dir, "/") + "/" + name
}

func ensureMigrationTable(ctx context.Context, conn *pgx.Conn, service string) error {
	_, err := conn.Exec(ctx, `
CREATE TABLE IF NOT EXISTS verself_schema_migrations (
  version text PRIMARY KEY,
  filename text NOT NULL UNIQUE,
  checksum text NOT NULL CHECK (checksum ~ '^[a-f0-9]{64}$'),
  applied_at timestamptz NOT NULL DEFAULT now()
)`)
	if err != nil {
		return fmt.Errorf("%s: ensure migration table: %w", service, err)
	}
	return nil
}

func readApplied(ctx context.Context, conn *pgx.Conn, service string) (map[string]appliedMigration, error) {
	rows, err := conn.Query(ctx, `SELECT version, filename, checksum FROM verself_schema_migrations`)
	if err != nil {
		return nil, fmt.Errorf("%s: read migration table: %w", service, err)
	}
	defer rows.Close()

	out := map[string]appliedMigration{}
	for rows.Next() {
		var version string
		var applied appliedMigration
		if err := rows.Scan(&version, &applied.Filename, &applied.Checksum); err != nil {
			return nil, fmt.Errorf("%s: scan migration table: %w", service, err)
		}
		out[version] = applied
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("%s: read migration rows: %w", service, err)
	}
	return out, nil
}

func applyMigration(ctx context.Context, conn *pgx.Conn, service string, m migration) error {
	if _, err := conn.Exec(ctx, "BEGIN"); err != nil {
		return fmt.Errorf("%s: begin migration %s: %w", service, m.Filename, err)
	}
	committed := false
	defer func() {
		if !committed {
			_, _ = conn.Exec(context.Background(), "ROLLBACK")
		}
	}()

	// pgx's extended protocol rejects multi-statement SQL; migration files are
	// deliberately sent through the simple protocol while the transaction is open.
	if _, err := conn.PgConn().Exec(ctx, m.SQL).ReadAll(); err != nil {
		return fmt.Errorf("%s: apply migration %s: %w", service, m.Filename, err)
	}
	if _, err := conn.Exec(ctx, `
INSERT INTO verself_schema_migrations (version, filename, checksum)
VALUES ($1, $2, $3)`, m.Version, m.Filename, m.Checksum); err != nil {
		return fmt.Errorf("%s: record migration %s: %w", service, m.Filename, err)
	}
	if _, err := conn.Exec(ctx, "COMMIT"); err != nil {
		return fmt.Errorf("%s: commit migration %s: %w", service, m.Filename, err)
	}
	committed = true
	return nil
}

func unlock(conn *pgx.Conn, key string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, _ = conn.Exec(ctx, "SELECT pg_advisory_unlock(hashtextextended($1, 0))", key)
}
