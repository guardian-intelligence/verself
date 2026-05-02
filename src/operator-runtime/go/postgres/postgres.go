package postgres

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"

	opruntime "github.com/verself/operator-runtime/runtime"
)

const (
	DefaultPort = 5432
	DefaultUser = "postgres"
)

type Config struct {
	Database     string
	User         string
	Password     string
	RemotePort   int
	PasswordPath string
	PasswordKey  string
}

func OpenOverSSH(ctx context.Context, rt *opruntime.Runtime, cfg Config) (*pgx.Conn, error) {
	if rt == nil || rt.SSH == nil {
		return nil, fmt.Errorf("postgres: operator runtime with SSH is required")
	}
	if cfg.Database == "" {
		return nil, fmt.Errorf("postgres: Database is required")
	}
	if cfg.User == "" {
		cfg.User = DefaultUser
	}
	if cfg.RemotePort == 0 {
		cfg.RemotePort = DefaultPort
	}
	if cfg.RemotePort <= 0 || cfg.RemotePort > 65535 {
		return nil, fmt.Errorf("postgres: invalid remote port %d", cfg.RemotePort)
	}
	password := cfg.Password
	if password == "" {
		key := cfg.PasswordKey
		if key == "" {
			key = "postgresql_admin_password"
		}
		path := cfg.PasswordPath
		if path == "" {
			path = opruntime.SecretsPath(rt.RepoRoot)
		}
		var err error
		password, err = opruntime.DecryptSOPSValue(ctx, path, key)
		if err != nil {
			return nil, err
		}
	}
	u := url.URL{
		Scheme:   "postgres",
		User:     url.UserPassword(cfg.User, password),
		Host:     net.JoinHostPort("127.0.0.1", strconv.Itoa(cfg.RemotePort)),
		Path:     "/" + cfg.Database,
		RawQuery: "sslmode=disable",
	}
	pgxCfg, err := pgx.ParseConfig(u.String())
	if err != nil {
		return nil, fmt.Errorf("postgres: parse config: %w", err)
	}
	pgxCfg.Config.DialFunc = rt.SSH.DialContext
	conn, err := pgx.ConnectConfig(ctx, pgxCfg)
	if err != nil {
		return nil, fmt.Errorf("postgres: connect: %w", err)
	}
	if err := conn.Ping(ctx); err != nil {
		_ = conn.Close(context.Background())
		return nil, fmt.Errorf("postgres: ping: %w", err)
	}
	return conn, nil
}

func QueryTable(ctx context.Context, conn *pgx.Conn, query string) (opruntime.Table, string, error) {
	return QueryTableArgs(ctx, conn, query)
}

func QueryTableArgs(ctx context.Context, conn *pgx.Conn, query string, args ...any) (opruntime.Table, string, error) {
	rows, err := conn.Query(ctx, query, args...)
	if err != nil {
		return opruntime.Table{}, "", err
	}
	defer rows.Close()
	fields := rows.FieldDescriptions()
	headers := make([]string, len(fields))
	for i, field := range fields {
		headers[i] = field.Name
	}
	out := opruntime.Table{Headers: headers}
	for rows.Next() {
		rowValues, err := rows.Values()
		if err != nil {
			return opruntime.Table{}, "", err
		}
		row := make([]string, len(rowValues))
		for i, value := range rowValues {
			row[i] = opruntime.FormatValue(value)
		}
		out.Rows = append(out.Rows, row)
	}
	if err := rows.Err(); err != nil {
		return opruntime.Table{}, "", err
	}
	return out, rows.CommandTag().String(), nil
}

func FormatTime(t time.Time) string {
	return t.Format(time.RFC3339Nano)
}
