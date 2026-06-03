package sitebootstrap

import (
	"context"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"
)

var postgresIdentifierRE = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

func waitRemotePostgresReady(ctx context.Context, target inventoryTarget) error {
	deadline := time.Now().Add(90 * time.Second)
	var last error
	for {
		last = runRemotePSQL(ctx, target, "SELECT 1;\n")
		if last == nil {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("PostgreSQL did not become ready: %w", last)
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("PostgreSQL readiness: %w: %v", ctx.Err(), last)
		case <-time.After(time.Second):
		}
	}
}

func reconcilePostgres(ctx context.Context, target inventoryTarget, catalog postgresCatalog, runtimeValues map[string]string) error {
	if err := validatePostgresCatalog(catalog, runtimeValues); err != nil {
		return err
	}
	if err := installRemotePostgresIdent(ctx, target, catalog); err != nil {
		return err
	}
	return runRemotePSQL(ctx, target, postgresReconcileSQL(catalog, runtimeValues))
}

func validatePostgresCatalog(catalog postgresCatalog, runtimeValues map[string]string) error {
	for _, db := range catalog.ServiceDatabases {
		if !postgresIdentifierRE.MatchString(db.Name) {
			return fmt.Errorf("PostgreSQL database name %q is not a safe identifier", db.Name)
		}
		if !postgresIdentifierRE.MatchString(db.Owner) {
			return fmt.Errorf("PostgreSQL owner role %q is not a safe identifier", db.Owner)
		}
	}
	for _, role := range catalog.ReplicationRoles {
		if !postgresIdentifierRE.MatchString(role.Name) {
			return fmt.Errorf("PostgreSQL replication role %q is not a safe identifier", role.Name)
		}
		if strings.TrimSpace(runtimeValues[role.OpenBaoSecret]) == "" {
			return fmt.Errorf("PostgreSQL replication role %s requires generated OpenBao secret %s", role.Name, role.OpenBaoSecret)
		}
	}
	for _, mapping := range catalog.PeerMappings {
		if !postgresIdentifierRE.MatchString(mapping.SystemUser) {
			return fmt.Errorf("PostgreSQL peer system user %q is not a safe identifier", mapping.SystemUser)
		}
		if !postgresIdentifierRE.MatchString(mapping.PGUser) {
			return fmt.Errorf("PostgreSQL peer user %q is not a safe identifier", mapping.PGUser)
		}
	}
	return nil
}

func installRemotePostgresIdent(ctx context.Context, target inventoryTarget, catalog postgresCatalog) error {
	var b strings.Builder
	for _, mapping := range catalog.PeerMappings {
		b.WriteString("verself_services      ")
		b.WriteString(mapping.SystemUser)
		b.WriteString("            ")
		b.WriteString(mapping.PGUser)
		b.WriteByte('\n')
	}
	path, cleanup, err := localTempFile("verself-pg-ident-*", b.String(), 0o600)
	if err != nil {
		return err
	}
	defer cleanup()
	remote, err := remoteTempSecretPath("pg-ident")
	if err != nil {
		return err
	}
	if err := copyLocalFileToRemote(ctx, target, path, remote); err != nil {
		return err
	}
	command := "set -euo pipefail; tmp=" + shellQuote(remote) + "; trap 'rm -f \"$tmp\"' EXIT; sudo -n install -D -o postgres -g postgres -m 0600 \"$tmp\" /etc/postgresql/verself/pg_ident.conf"
	if output, err := sshCommand(ctx, target, command).CombinedOutput(); err != nil {
		return fmt.Errorf("install PostgreSQL pg_ident.conf: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func runRemotePSQL(ctx context.Context, target inventoryTarget, sql string) error {
	if strings.TrimSpace(sql) == "" {
		return errors.New("PostgreSQL SQL is required")
	}
	path, cleanup, err := localTempFile("verself-pg-reconcile-*.sql", sql, 0o600)
	if err != nil {
		return err
	}
	defer cleanup()
	remote, err := remoteTempSecretPath("postgres-reconcile.sql")
	if err != nil {
		return err
	}
	if err := copyLocalFileToRemote(ctx, target, path, remote); err != nil {
		return err
	}
	command := strings.Join([]string{
		"set -euo pipefail",
		"upload=" + shellQuote(remote),
		"sql=$(mktemp /tmp/verself-pg-reconcile.XXXXXX.sql)",
		"trap 'rm -f \"$upload\"; sudo -n rm -f \"$sql\"' EXIT",
		"sudo -n install -o postgres -g postgres -m 0600 \"$upload\" \"$sql\"",
		"psql=$(sudo -n find /var/lib/nomad/alloc -path '*/postgresql-runtime/opt/verself/postgresql/usr/lib/postgresql/16/bin/psql' -type f -printf '%T@ %p\\n' | sort -nr | awk 'NR==1 { $1=\"\"; sub(/^ /, \"\"); print; exit }')",
		"test -n \"$psql\"",
		"runtime=${psql%/usr/lib/postgresql/16/bin/psql}",
		"lib=\"$runtime/usr/lib/x86_64-linux-gnu:$runtime/usr/lib/postgresql/16/lib\"",
		"sudo -n -u postgres env LD_LIBRARY_PATH=\"$lib\" \"$psql\" -v ON_ERROR_STOP=1 -f \"$sql\" postgres",
	}, "; ")
	if output, err := sshCommand(ctx, target, command).CombinedOutput(); err != nil {
		return fmt.Errorf("run PostgreSQL reconcile SQL: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func postgresReconcileSQL(catalog postgresCatalog, runtimeValues map[string]string) string {
	var b strings.Builder
	b.WriteString("SELECT pg_reload_conf();\n")
	for _, db := range catalog.ServiceDatabases {
		b.WriteString("DO $$ BEGIN\n")
		b.WriteString("  IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = ")
		b.WriteString(sqlLiteral(db.Owner))
		b.WriteString(") THEN\n")
		b.WriteString("    EXECUTE 'CREATE ROLE ")
		b.WriteString(pgQuoteIdent(db.Owner))
		b.WriteString(" LOGIN';\n")
		b.WriteString("  END IF;\n")
		b.WriteString("END $$;\n")
	}
	for _, role := range catalog.ReplicationRoles {
		b.WriteString("DO $$ BEGIN\n")
		b.WriteString("  IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = ")
		b.WriteString(sqlLiteral(role.Name))
		b.WriteString(") THEN\n")
		b.WriteString("    EXECUTE format('CREATE ROLE %I LOGIN REPLICATION CONNECTION LIMIT ")
		b.WriteString(fmt.Sprint(role.ConnectionLimit))
		b.WriteString(" PASSWORD %L', ")
		b.WriteString(sqlLiteral(role.Name))
		b.WriteString(", ")
		b.WriteString(sqlLiteral(runtimeValues[role.OpenBaoSecret]))
		b.WriteString(");\n")
		b.WriteString("  ELSE\n")
		b.WriteString("    EXECUTE format('ALTER ROLE %I WITH LOGIN REPLICATION CONNECTION LIMIT ")
		b.WriteString(fmt.Sprint(role.ConnectionLimit))
		b.WriteString(" PASSWORD %L', ")
		b.WriteString(sqlLiteral(role.Name))
		b.WriteString(", ")
		b.WriteString(sqlLiteral(runtimeValues[role.OpenBaoSecret]))
		b.WriteString(");\n")
		b.WriteString("  END IF;\n")
		b.WriteString("END $$;\n")
	}
	for _, db := range catalog.ServiceDatabases {
		b.WriteString("SELECT 'CREATE DATABASE ")
		b.WriteString(pgQuoteIdent(db.Name))
		b.WriteString(" OWNER ")
		b.WriteString(pgQuoteIdent(db.Owner))
		b.WriteString("' WHERE NOT EXISTS (SELECT 1 FROM pg_database WHERE datname = ")
		b.WriteString(sqlLiteral(db.Name))
		b.WriteString(")\\gexec\n")
	}
	return b.String()
}

func localTempFile(pattern, body string, mode os.FileMode) (string, func(), error) {
	file, err := os.CreateTemp("", pattern)
	if err != nil {
		return "", nil, fmt.Errorf("create temporary file: %w", err)
	}
	path := file.Name()
	cleanup := func() { _ = os.Remove(path) }
	if _, err := file.WriteString(body); err != nil {
		_ = file.Close()
		cleanup()
		return "", nil, fmt.Errorf("write temporary file: %w", err)
	}
	if err := file.Close(); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("close temporary file: %w", err)
	}
	if err := os.Chmod(path, mode); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("chmod temporary file: %w", err)
	}
	return path, cleanup, nil
}

func pgQuoteIdent(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `""`) + `"`
}

func sqlLiteral(value string) string {
	return `'` + strings.ReplaceAll(value, `'`, `''`) + `'`
}
