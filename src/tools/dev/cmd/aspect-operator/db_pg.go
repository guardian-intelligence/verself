package main

import (
	"errors"
	"flag"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strconv"

	"github.com/jackc/pgx/v5"
	opch "github.com/verself/operator-runtime/clickhouse"
	oppg "github.com/verself/operator-runtime/postgres"
	opruntime "github.com/verself/operator-runtime/runtime"
)

type dbPGOptions struct {
	dbRuntimeOptions
	secretsFile string
	user        string
	remotePort  int
}

func cmdDBPG(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("db pg: missing subcommand (try `list`, `query`, or `shell`)")
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "list":
		return cmdDBPGList(rest)
	case "query":
		return cmdDBPGQuery(rest)
	case "shell":
		return cmdDBPGShell(rest)
	default:
		return fmt.Errorf("db pg: unknown subcommand: %s", sub)
	}
}

func cmdDBPGList(args []string) error {
	fs := flagSet("db pg list")
	opts := addDBPGFlags(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := opts.validate(); err != nil {
		return err
	}
	return runDBPG("db.pg.list", opts, "postgres", false, func(rt *opruntime.Runtime, _ *opch.Client, conn *pgx.Conn) error {
		table, tag, err := oppg.QueryTable(rt.Ctx, conn, `
SELECT
  d.datname AS name,
  pg_catalog.pg_get_userbyid(d.datdba) AS owner,
  pg_catalog.pg_encoding_to_char(d.encoding) AS encoding,
  d.datcollate AS collate,
  d.datctype AS ctype,
  d.datconnlimit AS connlimit
FROM pg_catalog.pg_database d
WHERE NOT d.datistemplate
ORDER BY d.datname`)
		if err != nil {
			return err
		}
		if len(table.Headers) == 0 {
			if tag != "" {
				fmt.Fprintln(os.Stdout, tag)
			}
			return nil
		}
		return opruntime.PrintTable(os.Stdout, table)
	})
}

func cmdDBPGQuery(args []string) error {
	fs := flagSet("db pg query")
	opts := addDBPGFlags(fs)
	dbName := fs.String("db", "", "Database name")
	query := fs.String("query", "", "SQL to execute")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := opts.validate(); err != nil {
		return err
	}
	if *dbName == "" {
		return errors.New("db pg query: --db is required")
	}
	if *query == "" {
		return errors.New("db pg query: --query is required")
	}
	return runDBPG("db.pg.query", opts, *dbName, false, func(rt *opruntime.Runtime, _ *opch.Client, conn *pgx.Conn) error {
		table, tag, err := oppg.QueryTable(rt.Ctx, conn, *query)
		if err != nil {
			return err
		}
		if len(table.Headers) == 0 {
			if tag != "" {
				fmt.Fprintln(os.Stdout, tag)
			}
			return nil
		}
		return opruntime.PrintTable(os.Stdout, table)
	})
}

func cmdDBPGShell(args []string) error {
	fs := flagSet("db pg shell")
	opts := addDBPGFlags(fs)
	dbName := fs.String("db", "", "Database name")
	psqlPath := fs.String("psql-path", envOr("PSQL_PATH", "psql"), "Local psql binary")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := opts.validate(); err != nil {
		return err
	}
	if *dbName == "" {
		return errors.New("db pg shell: --db is required")
	}
	return runDBRuntime("db.pg.shell", opts.dbRuntimeOptions, true, func(rt *opruntime.Runtime, _ *opch.Client) error {
		password, err := opruntime.DecryptSOPSValue(rt.Ctx, postgresSecretsPath(rt, opts), "postgresql_admin_password")
		if err != nil {
			return err
		}
		forward, err := rt.SSH.Forward(rt.Ctx, "postgres", net.JoinHostPort("127.0.0.1", strconv.Itoa(opts.remotePort)))
		if err != nil {
			return err
		}
		defer func() { _ = forward.Close() }()
		host, port, err := splitHostPort(forward.ListenAddr)
		if err != nil {
			return err
		}
		cmd := exec.CommandContext(rt.Ctx, *psqlPath,
			"--host", host,
			"--port", port,
			"--username", opts.user,
			"--dbname", *dbName,
		)
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.Env = append(os.Environ(), "PGPASSWORD="+password)
		if err := cmd.Run(); err != nil {
			var ee *exec.ExitError
			if errors.As(err, &ee) {
				return exitError{code: ee.ExitCode()}
			}
			return fmt.Errorf("psql: %w", err)
		}
		return nil
	})
}

func addDBPGFlags(fs *flag.FlagSet) *dbPGOptions {
	opts := &dbPGOptions{}
	addDBRuntimeFlags(&opts.dbRuntimeOptions)
	fs.StringVar(&opts.site, "site", opts.site, "Deploy site")
	fs.StringVar(&opts.repoRoot, "repo-root", "", "verself-sh checkout root (defaults to cwd)")
	fs.StringVar(&opts.secretsFile, "secrets-file", os.Getenv("SOPS_SECRETS_FILE"), "SOPS secrets file")
	fs.StringVar(&opts.user, "user", envOr("PG_USER", oppg.DefaultUser), "PostgreSQL user")
	fs.IntVar(&opts.remotePort, "remote-port", envIntOr("PG_PORT", oppg.DefaultPort), "Remote PostgreSQL port on the worker loopback")
	return opts
}

func (opts *dbPGOptions) validate() error {
	if opts.remotePort <= 0 || opts.remotePort > 65535 {
		return fmt.Errorf("db pg: --remote-port must be between 1 and 65535 (got %d)", opts.remotePort)
	}
	return nil
}

func runDBPG(command string, opts *dbPGOptions, dbName string, interactive bool, fn func(*opruntime.Runtime, *opch.Client, *pgx.Conn) error) error {
	return runDBRuntime(command, opts.dbRuntimeOptions, interactive, func(rt *opruntime.Runtime, ch *opch.Client) error {
		conn, err := oppg.OpenOverSSH(rt.Ctx, rt, oppg.Config{
			Database:     dbName,
			User:         opts.user,
			RemotePort:   opts.remotePort,
			PasswordPath: postgresSecretsPath(rt, opts),
		})
		if err != nil {
			return fmt.Errorf("db pg %s: %w", dbName, err)
		}
		defer func() { _ = conn.Close(rt.Ctx) }()
		return fn(rt, ch, conn)
	})
}

func postgresSecretsPath(rt *opruntime.Runtime, opts *dbPGOptions) string {
	if opts.secretsFile != "" {
		return opts.secretsFile
	}
	return opruntime.HostConfigurationSecretsPath(rt.RepoRoot, rt.Site)
}

func splitHostPort(addr string) (string, string, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "", "", fmt.Errorf("parse listen address %q: %w", addr, err)
	}
	return host, port, nil
}
