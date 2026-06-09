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

type replicationConfigFile struct {
	Roles        []replicationRole    `json:"roles"`
	Publications []logicalPublication `json:"publications"`
}

type replicationRole struct {
	Name            string `json:"name"`
	ConnectionLimit int    `json:"connection_limit"`
	PasswordEnv     string `json:"password_env"`
}

type logicalPublication struct {
	Database         string   `json:"database"`
	Publication      string   `json:"publication"`
	PublicationOwner string   `json:"publication_owner"`
	TableOwner       string   `json:"table_owner"`
	ReplicationRole  string   `json:"replication_role"`
	Tables           []string `json:"tables"`
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
	replication, err := loadReplicationConfig(cfg.ConfigPath)
	if err != nil {
		return err
	}
	conn, err := connect(ctx, cfg.DSN, cfg.WaitDuration)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close(context.Background()) }()
	for _, role := range replication.Roles {
		password := os.Getenv(role.PasswordEnv)
		if password == "" {
			return fmt.Errorf("replication role %s password env %s is required", role.Name, role.PasswordEnv)
		}
		if err := convergeReplicationRole(ctx, conn, role, password); err != nil {
			return err
		}
		fmt.Printf("postgresql replication role %s converged\n", role.Name)
	}
	for _, publication := range replication.Publications {
		if err := convergeLogicalPublication(ctx, cfg.DSN, cfg.WaitDuration, publication); err != nil {
			return err
		}
		fmt.Printf("postgresql logical publication %s.%s converged\n", publication.Database, publication.Publication)
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

func loadReplicationConfig(path string) (replicationConfigFile, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return replicationConfigFile{}, fmt.Errorf("read replication config %s: %w", path, err)
	}
	var file replicationConfigFile
	if err := json.Unmarshal(body, &file); err != nil {
		return replicationConfigFile{}, fmt.Errorf("decode replication config %s: %w", path, err)
	}
	seen := map[string]bool{}
	for index, role := range file.Roles {
		prefix := fmt.Sprintf("roles[%d]", index)
		if !postgresIdentifierRE.MatchString(role.Name) {
			return replicationConfigFile{}, fmt.Errorf("%s.name must match %s", prefix, postgresIdentifierRE.String())
		}
		if role.ConnectionLimit <= 0 {
			return replicationConfigFile{}, fmt.Errorf("%s.connection_limit must be positive", prefix)
		}
		if !envNameRE.MatchString(role.PasswordEnv) {
			return replicationConfigFile{}, fmt.Errorf("%s.password_env must match %s", prefix, envNameRE.String())
		}
		if seen[role.Name] {
			return replicationConfigFile{}, fmt.Errorf("duplicate replication role %s", role.Name)
		}
		seen[role.Name] = true
	}
	seenPublications := map[string]bool{}
	for index, publication := range file.Publications {
		prefix := fmt.Sprintf("publications[%d]", index)
		if !postgresIdentifierRE.MatchString(publication.Database) {
			return replicationConfigFile{}, fmt.Errorf("%s.database must match %s", prefix, postgresIdentifierRE.String())
		}
		if !postgresIdentifierRE.MatchString(publication.Publication) {
			return replicationConfigFile{}, fmt.Errorf("%s.publication must match %s", prefix, postgresIdentifierRE.String())
		}
		if !postgresIdentifierRE.MatchString(publication.PublicationOwner) {
			return replicationConfigFile{}, fmt.Errorf("%s.publication_owner must match %s", prefix, postgresIdentifierRE.String())
		}
		if !postgresIdentifierRE.MatchString(publication.TableOwner) {
			return replicationConfigFile{}, fmt.Errorf("%s.table_owner must match %s", prefix, postgresIdentifierRE.String())
		}
		if !postgresIdentifierRE.MatchString(publication.ReplicationRole) {
			return replicationConfigFile{}, fmt.Errorf("%s.replication_role must match %s", prefix, postgresIdentifierRE.String())
		}
		if len(publication.Tables) == 0 {
			return replicationConfigFile{}, fmt.Errorf("%s.tables must not be empty", prefix)
		}
		for tableIndex, table := range publication.Tables {
			if !postgresIdentifierRE.MatchString(table) {
				return replicationConfigFile{}, fmt.Errorf("%s.tables[%d] must match %s", prefix, tableIndex, postgresIdentifierRE.String())
			}
		}
		key := publication.Database + "\x00" + publication.Publication
		if seenPublications[key] {
			return replicationConfigFile{}, fmt.Errorf("duplicate logical publication %s.%s", publication.Database, publication.Publication)
		}
		seenPublications[key] = true
	}
	return file, nil
}

func loadReplicationRoles(path string) ([]replicationRole, error) {
	file, err := loadReplicationConfig(path)
	if err != nil {
		return nil, err
	}
	return file.Roles, nil
}

func connect(ctx context.Context, dsn string, wait time.Duration) (*pgx.Conn, error) {
	cfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse postgres DSN: %w", err)
	}
	return connectConfig(ctx, cfg, wait)
}

func connectDatabase(ctx context.Context, dsn, database string, wait time.Duration) (*pgx.Conn, error) {
	cfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse postgres DSN: %w", err)
	}
	cfg.Database = database
	return connectConfig(ctx, cfg, wait)
}

func connectConfig(ctx context.Context, cfg *pgx.ConnConfig, wait time.Duration) (*pgx.Conn, error) {
	deadline := time.Now().Add(wait)
	var last error
	for {
		conn, err := pgx.ConnectConfig(ctx, cfg.Copy())
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
			return nil, fmt.Errorf("connect to postgres database %s: %w", cfg.Database, last)
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

func convergeLogicalPublication(ctx context.Context, dsn string, wait time.Duration, publication logicalPublication) error {
	conn, err := connectDatabase(ctx, dsn, publication.Database, wait)
	if err != nil {
		return fmt.Errorf("connect logical publication %s.%s: %w", publication.Database, publication.Publication, err)
	}
	defer func() { _ = conn.Close(context.Background()) }()
	tx, err := conn.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin logical publication %s.%s transaction: %w", publication.Database, publication.Publication, err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()

	databaseIdent := pgx.Identifier{publication.Database}.Sanitize()
	publicationIdent := pgx.Identifier{publication.Publication}.Sanitize()
	tableOwnerIdent := pgx.Identifier{publication.TableOwner}.Sanitize()
	publicationOwnerIdent := pgx.Identifier{publication.PublicationOwner}.Sanitize()
	replicationRoleIdent := pgx.Identifier{publication.ReplicationRole}.Sanitize()
	tableIdents := make([]string, 0, len(publication.Tables))
	for _, table := range publication.Tables {
		var exists bool
		if err := tx.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace WHERE n.nspname = 'public' AND c.relname = $1 AND c.relkind IN ('r', 'p'))", table).Scan(&exists); err != nil {
			return fmt.Errorf("check published table %s.%s: %w", publication.Database, table, err)
		}
		if !exists {
			return fmt.Errorf("published table %s.%s does not exist", publication.Database, table)
		}
		tableIdent := pgx.Identifier{"public", table}.Sanitize()
		tableIdents = append(tableIdents, tableIdent)
		if _, err := tx.Exec(ctx, fmt.Sprintf("ALTER TABLE %s OWNER TO %s", tableIdent, tableOwnerIdent)); err != nil {
			return fmt.Errorf("set published table owner %s.%s: %w", publication.Database, table, err)
		}
		if _, err := tx.Exec(ctx, fmt.Sprintf("ALTER TABLE %s REPLICA IDENTITY FULL", tableIdent)); err != nil {
			return fmt.Errorf("set published table replica identity %s.%s: %w", publication.Database, table, err)
		}
		if _, err := tx.Exec(ctx, fmt.Sprintf("GRANT SELECT ON TABLE %s TO %s", tableIdent, replicationRoleIdent)); err != nil {
			return fmt.Errorf("grant published table read %s.%s to %s: %w", publication.Database, table, publication.ReplicationRole, err)
		}
	}
	if _, err := tx.Exec(ctx, fmt.Sprintf("GRANT CONNECT ON DATABASE %s TO %s", databaseIdent, replicationRoleIdent)); err != nil {
		return fmt.Errorf("grant database connect %s to %s: %w", publication.Database, publication.ReplicationRole, err)
	}
	if _, err := tx.Exec(ctx, fmt.Sprintf("GRANT USAGE ON SCHEMA public TO %s", replicationRoleIdent)); err != nil {
		return fmt.Errorf("grant public schema usage %s to %s: %w", publication.Database, publication.ReplicationRole, err)
	}
	var exists bool
	if err := tx.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM pg_publication WHERE pubname = $1)", publication.Publication).Scan(&exists); err != nil {
		return fmt.Errorf("check logical publication %s.%s: %w", publication.Database, publication.Publication, err)
	}
	tableList := strings.Join(tableIdents, ", ")
	if exists {
		if _, err := tx.Exec(ctx, fmt.Sprintf("ALTER PUBLICATION %s SET TABLE %s", publicationIdent, tableList)); err != nil {
			return fmt.Errorf("set logical publication tables %s.%s: %w", publication.Database, publication.Publication, err)
		}
	} else if _, err := tx.Exec(ctx, fmt.Sprintf("CREATE PUBLICATION %s FOR TABLE %s", publicationIdent, tableList)); err != nil {
		return fmt.Errorf("create logical publication %s.%s: %w", publication.Database, publication.Publication, err)
	}
	if _, err := tx.Exec(ctx, fmt.Sprintf("ALTER PUBLICATION %s OWNER TO %s", publicationIdent, publicationOwnerIdent)); err != nil {
		return fmt.Errorf("set logical publication owner %s.%s: %w", publication.Database, publication.Publication, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit logical publication %s.%s: %w", publication.Database, publication.Publication, err)
	}
	return nil
}
