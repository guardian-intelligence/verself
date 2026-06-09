package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

var (
	postgresIdentifierRE = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
	envNameRE            = regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)
)

type config struct {
	DSN          string
	ConfigPath   string
	WaitDuration time.Duration
}

type replicationRoleFile struct {
	Roles []replicationRole `json:"roles"`
}

type replicationRole struct {
	Name            string `json:"name"`
	ConnectionLimit int    `json:"connection_limit"`
	PasswordEnv     string `json:"password_env"`
}

func main() {
	if err := run(context.Background(), os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	cfg, err := parseConfig(args)
	if err != nil {
		return err
	}
	roles, err := loadReplicationRoles(cfg.ConfigPath)
	if err != nil {
		return err
	}
	conn, err := connect(ctx, cfg.DSN, cfg.WaitDuration)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close(context.Background()) }()
	for _, role := range roles {
		password := os.Getenv(role.PasswordEnv)
		if password == "" {
			return fmt.Errorf("replication role %s password env %s is required", role.Name, role.PasswordEnv)
		}
		if err := convergeReplicationRole(ctx, conn, role, password); err != nil {
			return err
		}
		fmt.Printf("postgresql replication role %s converged\n", role.Name)
	}
	return nil
}

func parseConfig(args []string) (config, error) {
	fs := flag.NewFlagSet("postgresql-bootstrap", flag.ContinueOnError)
	cfg := config{}
	fs.StringVar(&cfg.DSN, "dsn", "postgres://postgres@/postgres?host=/var/run/postgresql&sslmode=disable", "PostgreSQL admin DSN")
	fs.StringVar(&cfg.ConfigPath, "replication-roles", "", "JSON replication role convergence file")
	fs.DurationVar(&cfg.WaitDuration, "wait", 30*time.Second, "maximum time to wait for PostgreSQL readiness")
	if err := fs.Parse(args); err != nil {
		return config{}, err
	}
	if strings.TrimSpace(cfg.ConfigPath) == "" {
		return config{}, errors.New("--replication-roles is required")
	}
	if cfg.WaitDuration <= 0 {
		return config{}, errors.New("--wait must be positive")
	}
	return cfg, nil
}

func loadReplicationRoles(path string) ([]replicationRole, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read replication roles %s: %w", path, err)
	}
	var file replicationRoleFile
	if err := json.Unmarshal(body, &file); err != nil {
		return nil, fmt.Errorf("decode replication roles %s: %w", path, err)
	}
	seen := map[string]bool{}
	for index, role := range file.Roles {
		prefix := fmt.Sprintf("roles[%d]", index)
		if !postgresIdentifierRE.MatchString(role.Name) {
			return nil, fmt.Errorf("%s.name must match %s", prefix, postgresIdentifierRE.String())
		}
		if role.ConnectionLimit <= 0 {
			return nil, fmt.Errorf("%s.connection_limit must be positive", prefix)
		}
		if !envNameRE.MatchString(role.PasswordEnv) {
			return nil, fmt.Errorf("%s.password_env must match %s", prefix, envNameRE.String())
		}
		if seen[role.Name] {
			return nil, fmt.Errorf("duplicate replication role %s", role.Name)
		}
		seen[role.Name] = true
	}
	return file.Roles, nil
}

func connect(ctx context.Context, dsn string, wait time.Duration) (*pgx.Conn, error) {
	deadline := time.Now().Add(wait)
	var last error
	for {
		conn, err := pgx.Connect(ctx, dsn)
		if err == nil {
			if pingErr := conn.Ping(ctx); pingErr == nil {
				return conn, nil
			} else {
				last = pingErr
				_ = conn.Close(ctx)
			}
		} else {
			last = err
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("connect to postgres: %w", last)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
}

func convergeReplicationRole(ctx context.Context, conn *pgx.Conn, role replicationRole, password string) error {
	tx, err := conn.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin replication role %s transaction: %w", role.Name, err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	var quotedPassword string
	if err := tx.QueryRow(ctx, "SELECT quote_literal($1)", password).Scan(&quotedPassword); err != nil {
		return fmt.Errorf("quote replication role %s password: %w", role.Name, err)
	}
	ident := pgx.Identifier{role.Name}.Sanitize()
	var exists bool
	if err := tx.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = $1)", role.Name).Scan(&exists); err != nil {
		return fmt.Errorf("check replication role %s: %w", role.Name, err)
	}
	if !exists {
		if _, err := tx.Exec(ctx, fmt.Sprintf("CREATE ROLE %s", ident)); err != nil {
			return fmt.Errorf("create replication role %s: %w", role.Name, err)
		}
	}
	query := fmt.Sprintf(
		"ALTER ROLE %s WITH LOGIN REPLICATION CONNECTION LIMIT %d PASSWORD %s",
		ident,
		role.ConnectionLimit,
		quotedPassword,
	)
	if _, err := tx.Exec(ctx, query); err != nil {
		return fmt.Errorf("alter replication role %s: %w", role.Name, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit replication role %s: %w", role.Name, err)
	}
	return nil
}
