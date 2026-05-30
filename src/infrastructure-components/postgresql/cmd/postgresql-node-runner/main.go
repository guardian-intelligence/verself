package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const (
	postgresUser  = "postgres"
	postgresGroup = "postgres"

	dataParentDir   = "/var/lib/postgresql/16"
	dataDir         = "/var/lib/postgresql/16/verself"
	homeDir         = "/var/lib/postgresql"
	configParentDir = "/etc/postgresql"
	configDir       = "/etc/postgresql/verself"
	logDir          = "/var/log/postgresql"
	runDir          = "/var/run/postgresql"

	configPath = "/etc/postgresql/verself/postgresql.conf"
	hbaPath    = "/etc/postgresql/verself/pg_hba.conf"
	identPath  = "/etc/postgresql/verself/pg_ident.conf"

	openbaoRootTokenPath = "/etc/credstore/openbao/root-token"
	openbaoCACertPath    = "/etc/openbao/tls/cert.pem"
	openbaoAddr          = "https://127.0.0.1:8200"
	siteConfigMount      = "site-config"
)

type config struct {
	RuntimeRoot string
}

type ids struct {
	PostgresUID int
	PostgresGID int
	AdmGID      int
	RootGID     int
}

type roleSpec struct {
	Name        string
	Password    string
	Replication bool
	ConnLimit   int
}

type databaseSpec struct {
	Name  string
	Owner string
}

type peerMapping struct {
	SystemUser string
	PGUser     string
}

type publicationSpec struct {
	Database        string
	Name            string
	Owner           string
	TableOwner      string
	ReplicationRole string
	Tables          []string
}

var localRoles = []roleSpec{
	{Name: "billing"},
	{Name: "distribution_service"},
	{Name: "email_service"},
	{Name: "github_integration_service"},
	{Name: "governance_service"},
	{Name: "iam_service"},
	{Name: "notifications_service"},
	{Name: "object_storage_service"},
	{Name: "otelcol"},
	{Name: "profile_service"},
	{Name: "projects_service"},
	{Name: "sandbox_rental"},
	{Name: "source_code_hosting_service"},
	{Name: "spicedb"},
	{Name: "stalwart"},
	{Name: "temporal"},
}

var localDatabases = []databaseSpec{
	{Name: "billing", Owner: "billing"},
	{Name: "distribution_service", Owner: "distribution_service"},
	{Name: "email_service", Owner: "email_service"},
	{Name: "github_integration_service", Owner: "github_integration_service"},
	{Name: "governance_service", Owner: "governance_service"},
	{Name: "iam_service", Owner: "iam_service"},
	{Name: "notifications_service", Owner: "notifications_service"},
	{Name: "object_storage_service", Owner: "object_storage_service"},
	{Name: "profile", Owner: "profile_service"},
	{Name: "projects_service", Owner: "projects_service"},
	{Name: "sandbox_rental", Owner: "sandbox_rental"},
	{Name: "source_code_hosting", Owner: "source_code_hosting_service"},
	{Name: "spicedb", Owner: "spicedb"},
	{Name: "stalwart", Owner: "stalwart"},
	{Name: "temporal", Owner: "temporal"},
	{Name: "temporal_visibility", Owner: "temporal"},
}

var identMappings = []peerMapping{
	{SystemUser: "postgres", PGUser: "postgres"},
	{SystemUser: "billing", PGUser: "billing"},
	{SystemUser: "distribution_service", PGUser: "distribution_service"},
	{SystemUser: "email_service", PGUser: "email_service"},
	{SystemUser: "github_integration_service", PGUser: "github_integration_service"},
	{SystemUser: "governance_service", PGUser: "billing"},
	{SystemUser: "governance_service", PGUser: "governance_service"},
	{SystemUser: "governance_service", PGUser: "iam_service"},
	{SystemUser: "governance_service", PGUser: "sandbox_rental"},
	{SystemUser: "iam_service", PGUser: "iam_service"},
	{SystemUser: "notifications_service", PGUser: "notifications_service"},
	{SystemUser: "object_storage_admin", PGUser: "object_storage_service"},
	{SystemUser: "object_storage_service", PGUser: "object_storage_service"},
	{SystemUser: "otelcol", PGUser: "otelcol"},
	{SystemUser: "profile_service", PGUser: "profile_service"},
	{SystemUser: "projects_service", PGUser: "projects_service"},
	{SystemUser: "sandbox_rental", PGUser: "sandbox_rental"},
	{SystemUser: "source_code_hosting_service", PGUser: "source_code_hosting_service"},
	{SystemUser: "spicedb", PGUser: "spicedb"},
	{SystemUser: "stalwart", PGUser: "stalwart"},
	{SystemUser: "temporal_server", PGUser: "temporal"},
}

var logicalPublications = []publicationSpec{
	{
		Database:        "sandbox_rental",
		Name:            "electric_publication_default",
		Owner:           "electric",
		TableOwner:      "sandbox_rental",
		ReplicationRole: "electric",
		Tables: []string{
			"execution_logs",
			"executions",
			"runner_provider_repositories",
		},
	},
	{
		Database:        "notifications_service",
		Name:            "electric_publication_notifications",
		Owner:           "electric_notifications",
		TableOwner:      "notifications_service",
		ReplicationRole: "electric_notifications",
		Tables: []string{
			"notification_inbox_state",
		},
	},
	{
		Database:        "iam_service",
		Name:            "electric_publication_iam",
		Owner:           "electric_iam",
		TableOwner:      "iam_service",
		ReplicationRole: "electric_iam",
		Tables: []string{
			"iam_web_auth_clients",
			"iam_web_auth_accounts",
			"iam_web_auth_account_sessions",
			"iam_web_device_login_requests",
		},
	},
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "postgresql-node-runner: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	var cfg config
	fs := flag.NewFlagSet("postgresql-node-runner", flag.ContinueOnError)
	fs.StringVar(&cfg.RuntimeRoot, "runtime-root", "", "PostgreSQL runtime root from the resolved artifact.")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected positional args: %s", strings.Join(fs.Args(), " "))
	}
	if strings.TrimSpace(cfg.RuntimeRoot) == "" {
		return errors.New("--runtime-root is required")
	}
	if err := ensureGroupsAndUsers(ctx); err != nil {
		return err
	}
	ids, err := lookupIDs()
	if err != nil {
		return err
	}
	if err := convergeDirectories(ids); err != nil {
		return err
	}
	adminPassword, err := siteSecret(ctx, "postgresql_admin_password")
	if err != nil {
		return err
	}
	if err := convergeConfig(cfg, ids); err != nil {
		return err
	}
	if err := initDatabase(ctx, cfg, ids, adminPassword); err != nil {
		return err
	}
	return runServer(ctx, cfg, ids, adminPassword)
}

func ensureGroupsAndUsers(ctx context.Context) error {
	if err := ensureGroup(ctx, postgresGroup); err != nil {
		return err
	}
	if err := ensureUser(ctx, postgresUser, postgresGroup, homeDir); err != nil {
		return err
	}
	return nil
}

func ensureGroup(ctx context.Context, name string) error {
	if err := runCommand(ctx, "getent", "group", name); err == nil {
		return nil
	}
	return runCommand(ctx, "groupadd", "--system", name)
}

func ensureUser(ctx context.Context, name, group, home string) error {
	if err := runCommand(ctx, "id", "-u", name); err == nil {
		return nil
	}
	return runCommand(ctx, "useradd", "--system", "--gid", group, "--home-dir", home, "--shell", "/usr/sbin/nologin", "--no-create-home", name)
}

func lookupIDs() (ids, error) {
	postgresUID, err := lookupUserID(postgresUser)
	if err != nil {
		return ids{}, err
	}
	postgresGID, err := lookupGroupID(postgresGroup)
	if err != nil {
		return ids{}, err
	}
	admGID, err := lookupGroupID("adm")
	if err != nil {
		return ids{}, err
	}
	rootGID, err := lookupGroupID("root")
	if err != nil {
		return ids{}, err
	}
	return ids{PostgresUID: postgresUID, PostgresGID: postgresGID, AdmGID: admGID, RootGID: rootGID}, nil
}

func lookupUserID(name string) (int, error) {
	u, err := user.Lookup(name)
	if err != nil {
		return 0, fmt.Errorf("lookup user %s: %w", name, err)
	}
	id, err := strconv.Atoi(u.Uid)
	if err != nil {
		return 0, fmt.Errorf("parse uid for %s: %w", name, err)
	}
	return id, nil
}

func lookupGroupID(name string) (int, error) {
	g, err := user.LookupGroup(name)
	if err != nil {
		return 0, fmt.Errorf("lookup group %s: %w", name, err)
	}
	id, err := strconv.Atoi(g.Gid)
	if err != nil {
		return 0, fmt.Errorf("parse gid for %s: %w", name, err)
	}
	return id, nil
}

func convergeDirectories(ids ids) error {
	for _, dir := range []struct {
		Path string
		UID  int
		GID  int
		Mode os.FileMode
	}{
		{Path: homeDir, UID: ids.PostgresUID, GID: ids.PostgresGID, Mode: 0750},
		{Path: dataParentDir, UID: ids.PostgresUID, GID: ids.PostgresGID, Mode: 0750},
		{Path: dataDir, UID: ids.PostgresUID, GID: ids.PostgresGID, Mode: 0700},
		{Path: configParentDir, UID: 0, GID: ids.RootGID, Mode: 0755},
		{Path: configDir, UID: ids.PostgresUID, GID: ids.PostgresGID, Mode: 0700},
		{Path: logDir, UID: ids.PostgresUID, GID: ids.AdmGID, Mode: 02755},
		{Path: runDir, UID: ids.PostgresUID, GID: ids.PostgresGID, Mode: 0755},
	} {
		if err := ensureDir(dir.Path, dir.UID, dir.GID, dir.Mode); err != nil {
			return err
		}
	}
	return nil
}

func ensureDir(path string, uid, gid int, mode os.FileMode) error {
	if err := os.MkdirAll(path, mode); err != nil {
		return fmt.Errorf("mkdir %s: %w", path, err)
	}
	if err := os.Chown(path, uid, gid); err != nil {
		return fmt.Errorf("chown %s: %w", path, err)
	}
	if err := os.Chmod(path, mode); err != nil {
		return fmt.Errorf("chmod %s: %w", path, err)
	}
	return nil
}

func siteSecret(ctx context.Context, name string) (string, error) {
	tokenBody, err := os.ReadFile(openbaoRootTokenPath)
	if err != nil {
		return "", fmt.Errorf("read OpenBao root token: %w", err)
	}
	certBody, err := os.ReadFile(openbaoCACertPath)
	if err != nil {
		return "", fmt.Errorf("read OpenBao CA cert: %w", err)
	}
	pool := x509.NewCertPool()
	if ok := pool.AppendCertsFromPEM(certBody); !ok {
		return "", fmt.Errorf("parse OpenBao CA cert %s", openbaoCACertPath)
	}
	client := &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12},
		},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, openbaoAddr+"/v1/"+siteConfigMount+"/data/config/"+name, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("X-Vault-Token", strings.TrimSpace(string(tokenBody)))
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("read OpenBao site secret %s: %w", name, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("read OpenBao site secret %s: status %d", name, resp.StatusCode)
	}
	var payload struct {
		Data struct {
			Data struct {
				Value string `json:"value"`
			} `json:"data"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", fmt.Errorf("decode OpenBao site secret %s: %w", name, err)
	}
	value := strings.TrimSpace(payload.Data.Data.Value)
	if value == "" {
		return "", fmt.Errorf("OpenBao site secret %s is empty", name)
	}
	return value, nil
}

func convergeConfig(cfg config, ids ids) error {
	libDir := filepath.Join(cfg.RuntimeRoot, "usr/lib/postgresql/16/lib")
	if err := writeFile(configPath, []byte(postgresqlConf(libDir)), 0600, ids.PostgresUID, ids.PostgresGID); err != nil {
		return err
	}
	if err := writeFile(hbaPath, []byte(pgHBAConf()), 0600, ids.PostgresUID, ids.PostgresGID); err != nil {
		return err
	}
	return writeFile(identPath, []byte(pgIdentConf()), 0600, ids.PostgresUID, ids.PostgresGID)
}

func postgresqlConf(libDir string) string {
	return fmt.Sprintf(`# PostgreSQL configuration.
listen_addresses = '127.0.0.1'
port = 5432
max_connections = 300
superuser_reserved_connections = 10

data_directory = '%s'
hba_file = '%s'
ident_file = '%s'
dynamic_library_path = '%s'

logging_collector = on
log_directory = '%s'
log_filename = 'postgresql-%%Y-%%m-%%d.log'
log_file_mode = 0640
log_rotation_age = 1d
log_rotation_size = 100MB
log_min_messages = warning
log_line_prefix = '%%m [%%p] %%q%%u@%%d '

password_encryption = scram-sha-256

shared_buffers = 256MB
work_mem = 4MB
maintenance_work_mem = 64MB
effective_cache_size = 512MB

wal_level = logical
track_commit_timestamp = on
max_wal_size = 1GB
min_wal_size = 80MB

unix_socket_directories = '%s'
`, dataDir, hbaPath, identPath, libDir, logDir, runDir)
}

func pgHBAConf() string {
	return `# PostgreSQL host-based authentication.
# TYPE  DATABASE  USER  ADDRESS      METHOD
local   all       all                peer map=verself_services
host    all       all   127.0.0.1/32 scram-sha-256
host    all       all   ::1/128      scram-sha-256
`
}

func pgIdentConf() string {
	var b strings.Builder
	b.WriteString("# PostgreSQL user name maps.\n# MAPNAME                 SYSTEM-USERNAME          PG-USERNAME\n\n")
	for _, mapping := range identMappings {
		b.WriteString(fmt.Sprintf("verself_services      %-24s %s\n", mapping.SystemUser, mapping.PGUser))
	}
	return b.String()
}

func initDatabase(ctx context.Context, cfg config, ids ids, adminPassword string) error {
	if _, err := os.Stat(filepath.Join(dataDir, "PG_VERSION")); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("stat PostgreSQL data directory: %w", err)
	}
	pwFile, err := os.CreateTemp(runDir, ".pg-initdb-pw-*")
	if err != nil {
		return fmt.Errorf("create initdb password file: %w", err)
	}
	pwPath := pwFile.Name()
	defer func() { _ = os.Remove(pwPath) }()
	if _, err := io.WriteString(pwFile, adminPassword+"\n"); err != nil {
		_ = pwFile.Close()
		return fmt.Errorf("write initdb password file: %w", err)
	}
	if err := pwFile.Close(); err != nil {
		return fmt.Errorf("close initdb password file: %w", err)
	}
	if err := os.Chown(pwPath, ids.PostgresUID, ids.PostgresGID); err != nil {
		return fmt.Errorf("chown initdb password file: %w", err)
	}
	if err := os.Chmod(pwPath, 0600); err != nil {
		return fmt.Errorf("chmod initdb password file: %w", err)
	}
	return runAsPostgres(ctx, cfg, ids, initdbPath(cfg), "--pgdata="+dataDir, "--auth=scram-sha-256", "--encoding=UTF8", "--locale=C.UTF-8", "-L", filepath.Join(cfg.RuntimeRoot, "usr/share/postgresql/16"), "--pwfile="+pwPath)
}

func runServer(ctx context.Context, cfg config, ids ids, adminPassword string) error {
	cmd := exec.Command(postgresPath(cfg), "-D", dataDir, "-c", "config_file="+configPath)
	cmd.Env = postgresEnv(cfg)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
		Credential: &syscall.Credential{
			Uid: uint32(ids.PostgresUID),
			Gid: uint32(ids.PostgresGID),
		},
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start postgres: %w", err)
	}
	waitCh := make(chan error, 1)
	go func() {
		waitCh <- cmd.Wait()
	}()
	if err := waitForPostgres(ctx, cfg, ids, waitCh, 45*time.Second); err != nil {
		stopProcessGroup(cmd.Process.Pid, waitCh)
		return err
	}
	if err := convergeCatalog(ctx, cfg, ids, adminPassword); err != nil {
		stopProcessGroup(cmd.Process.Pid, waitCh)
		return err
	}
	publicationErrCh := startPublicationReconciler(ctx, cfg, ids)
	select {
	case err := <-waitCh:
		return err
	case err := <-publicationErrCh:
		stopProcessGroup(cmd.Process.Pid, waitCh)
		return err
	case <-ctx.Done():
		stopProcessGroup(cmd.Process.Pid, waitCh)
		return ctx.Err()
	}
}

func waitForPostgres(ctx context.Context, cfg config, ids ids, waitCh <-chan error, timeout time.Duration) error {
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	var lastErr error
	for {
		select {
		case err := <-waitCh:
			if err == nil {
				return errors.New("postgres exited before readiness")
			}
			return fmt.Errorf("postgres exited before readiness: %w", err)
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			if lastErr != nil {
				return fmt.Errorf("postgres did not become ready within %s: %w", timeout, lastErr)
			}
			return fmt.Errorf("postgres did not become ready within %s", timeout)
		case <-ticker.C:
			if err := psqlExec(ctx, cfg, ids, "postgres", "SELECT 1;"); err == nil {
				return nil
			} else {
				lastErr = err
			}
		}
	}
}

func stopProcessGroup(pid int, waitCh <-chan error) {
	if err := syscall.Kill(-pid, 0); err != nil {
		return
	}
	_ = syscall.Kill(-pid, syscall.SIGTERM)
	select {
	case <-waitCh:
	case <-time.After(30 * time.Second):
		_ = syscall.Kill(-pid, syscall.SIGKILL)
		select {
		case <-waitCh:
		case <-time.After(5 * time.Second):
		}
	}
}

func convergeCatalog(ctx context.Context, cfg config, ids ids, adminPassword string) error {
	if err := psqlExec(ctx, cfg, ids, "postgres", "ALTER ROLE postgres WITH PASSWORD "+quoteLiteral(adminPassword)+";"); err != nil {
		return err
	}
	roles := append([]roleSpec{}, localRoles...)
	zitadelPassword, err := siteSecret(ctx, "zitadel_db_password")
	if err != nil {
		return err
	}
	roles = append(roles, roleSpec{Name: "zitadel", Password: zitadelPassword, ConnLimit: 15})
	for _, role := range electricRoles() {
		password, err := readSecretFile(role.Password)
		if err != nil {
			return err
		}
		roles = append(roles, roleSpec{Name: role.Name, Password: password, Replication: true, ConnLimit: role.ConnLimit})
	}
	for _, role := range roles {
		if err := ensureRole(ctx, cfg, ids, role); err != nil {
			return err
		}
	}
	if err := psqlExec(ctx, cfg, ids, "postgres", "GRANT pg_monitor TO otelcol;"); err != nil {
		return err
	}
	databases := append([]databaseSpec{}, localDatabases...)
	databases = append(databases, databaseSpec{Name: "zitadel", Owner: "zitadel"})
	for _, db := range databases {
		if err := ensureDatabase(ctx, cfg, ids, db); err != nil {
			return err
		}
	}
	return psqlExec(ctx, cfg, ids, "postgres", "SELECT pg_reload_conf();")
}

func startPublicationReconciler(ctx context.Context, cfg config, ids ids) <-chan error {
	errCh := make(chan error, 1)
	go func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for {
			complete, err := convergeLogicalPublications(ctx, cfg, ids)
			if err != nil {
				errCh <- err
				return
			}
			if complete {
				return
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
	return errCh
}

func convergeLogicalPublications(ctx context.Context, cfg config, ids ids) (bool, error) {
	complete := true
	for _, publication := range logicalPublications {
		ready, err := publicationTablesReady(ctx, cfg, ids, publication)
		if err != nil {
			return false, err
		}
		if !ready {
			complete = false
			continue
		}
		if err := ensureLogicalPublication(ctx, cfg, ids, publication); err != nil {
			return false, err
		}
	}
	return complete, nil
}

func publicationTablesReady(ctx context.Context, cfg config, ids ids, publication publicationSpec) (bool, error) {
	for _, table := range publication.Tables {
		query := "SELECT to_regclass(" + quoteLiteral(qualifiedRegclass("public", table)) + ") IS NOT NULL;"
		out, err := psqlQuery(ctx, cfg, ids, publication.Database, query)
		if err != nil {
			return false, fmt.Errorf("check publication table %s.%s: %w", publication.Database, table, err)
		}
		if strings.TrimSpace(out) != "t" {
			return false, nil
		}
	}
	return true, nil
}

func ensureLogicalPublication(ctx context.Context, cfg config, ids ids, publication publicationSpec) error {
	if err := psqlExec(ctx, cfg, ids, "postgres", "GRANT CONNECT ON DATABASE "+quoteIdent(publication.Database)+" TO "+quoteIdent(publication.ReplicationRole)+";"); err != nil {
		return fmt.Errorf("grant replication database access %s: %w", publication.Database, err)
	}
	if err := psqlExec(ctx, cfg, ids, publication.Database, "GRANT USAGE ON SCHEMA public TO "+quoteIdent(publication.ReplicationRole)+";"); err != nil {
		return fmt.Errorf("grant replication schema access %s: %w", publication.Database, err)
	}
	for _, table := range publication.Tables {
		qualified := qualifiedTable("public", table)
		if err := psqlExec(ctx, cfg, ids, publication.Database, "ALTER TABLE "+qualified+" OWNER TO "+quoteIdent(publication.TableOwner)+";"); err != nil {
			return fmt.Errorf("set table owner %s.%s: %w", publication.Database, table, err)
		}
		if err := psqlExec(ctx, cfg, ids, publication.Database, "ALTER TABLE "+qualified+" REPLICA IDENTITY FULL;"); err != nil {
			return fmt.Errorf("set replica identity %s.%s: %w", publication.Database, table, err)
		}
		if err := psqlExec(ctx, cfg, ids, publication.Database, "GRANT SELECT ON TABLE "+qualified+" TO "+quoteIdent(publication.ReplicationRole)+";"); err != nil {
			return fmt.Errorf("grant replication table access %s.%s: %w", publication.Database, table, err)
		}
	}
	exists, err := publicationExists(ctx, cfg, ids, publication.Database, publication.Name)
	if err != nil {
		return err
	}
	tableList := publicationTableList(publication.Tables)
	if exists {
		if err := psqlExec(ctx, cfg, ids, publication.Database, "ALTER PUBLICATION "+quoteIdent(publication.Name)+" SET TABLE "+tableList+";"); err != nil {
			return fmt.Errorf("update publication %s.%s: %w", publication.Database, publication.Name, err)
		}
	} else {
		if err := psqlExec(ctx, cfg, ids, publication.Database, "CREATE PUBLICATION "+quoteIdent(publication.Name)+" FOR TABLE "+tableList+";"); err != nil {
			return fmt.Errorf("create publication %s.%s: %w", publication.Database, publication.Name, err)
		}
	}
	if err := psqlExec(ctx, cfg, ids, publication.Database, "ALTER PUBLICATION "+quoteIdent(publication.Name)+" OWNER TO "+quoteIdent(publication.Owner)+";"); err != nil {
		return fmt.Errorf("set publication owner %s.%s: %w", publication.Database, publication.Name, err)
	}
	return nil
}

func publicationExists(ctx context.Context, cfg config, ids ids, database, name string) (bool, error) {
	out, err := psqlQuery(ctx, cfg, ids, database, "SELECT 1 FROM pg_publication WHERE pubname = "+quoteLiteral(name)+";")
	if err != nil {
		return false, fmt.Errorf("check publication %s.%s: %w", database, name, err)
	}
	return strings.TrimSpace(out) == "1", nil
}

func publicationTableList(tables []string) string {
	qualified := make([]string, 0, len(tables))
	for _, table := range tables {
		qualified = append(qualified, qualifiedTable("public", table))
	}
	return strings.Join(qualified, ", ")
}

type electricRole struct {
	Name      string
	Password  string
	ConnLimit int
}

func electricRoles() []electricRole {
	return []electricRole{
		{Name: "electric", Password: "/etc/credstore/electric/pg-password", ConnLimit: 25},
		{Name: "electric_notifications", Password: "/etc/credstore/electric-notifications/pg-password", ConnLimit: 15},
		{Name: "electric_iam", Password: "/etc/credstore/electric-iam/pg-password", ConnLimit: 15},
	}
}

func readSecretFile(path string) (string, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", path, err)
	}
	value := strings.TrimSpace(string(body))
	if value == "" {
		return "", fmt.Errorf("%s is empty", path)
	}
	return value, nil
}

func ensureRole(ctx context.Context, cfg config, ids ids, role roleSpec) error {
	exists, err := roleExists(ctx, cfg, ids, role.Name)
	if err != nil {
		return err
	}
	if !exists {
		if err := psqlExec(ctx, cfg, ids, "postgres", "CREATE ROLE "+quoteIdent(role.Name)+";"); err != nil {
			return err
		}
	}
	clauses := []string{"LOGIN"}
	if role.Replication {
		clauses = append(clauses, "REPLICATION")
	} else {
		clauses = append(clauses, "NOREPLICATION")
	}
	if role.Password != "" {
		clauses = append(clauses, "PASSWORD "+quoteLiteral(role.Password))
	} else {
		clauses = append(clauses, "PASSWORD NULL")
	}
	connLimit := role.ConnLimit
	if connLimit == 0 {
		connLimit = -1
	}
	clauses = append(clauses, "CONNECTION LIMIT "+strconv.Itoa(connLimit))
	return psqlExec(ctx, cfg, ids, "postgres", "ALTER ROLE "+quoteIdent(role.Name)+" WITH "+strings.Join(clauses, " ")+";")
}

func roleExists(ctx context.Context, cfg config, ids ids, name string) (bool, error) {
	out, err := psqlQuery(ctx, cfg, ids, "postgres", "SELECT 1 FROM pg_roles WHERE rolname = "+quoteLiteral(name)+";")
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(out) == "1", nil
}

func ensureDatabase(ctx context.Context, cfg config, ids ids, db databaseSpec) error {
	exists, err := databaseExists(ctx, cfg, ids, db.Name)
	if err != nil {
		return err
	}
	if !exists {
		if err := psqlExec(ctx, cfg, ids, "postgres", "CREATE DATABASE "+quoteIdent(db.Name)+" OWNER "+quoteIdent(db.Owner)+";"); err != nil {
			return err
		}
	}
	return psqlExec(ctx, cfg, ids, "postgres", "ALTER DATABASE "+quoteIdent(db.Name)+" OWNER TO "+quoteIdent(db.Owner)+";")
}

func databaseExists(ctx context.Context, cfg config, ids ids, name string) (bool, error) {
	out, err := psqlQuery(ctx, cfg, ids, "postgres", "SELECT 1 FROM pg_database WHERE datname = "+quoteLiteral(name)+";")
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(out) == "1", nil
}

func psqlExec(ctx context.Context, cfg config, ids ids, db, sql string) error {
	_, err := runPSQL(ctx, cfg, ids, db, sql, false)
	return err
}

func psqlQuery(ctx context.Context, cfg config, ids ids, db, sql string) (string, error) {
	return runPSQL(ctx, cfg, ids, db, sql, true)
}

func runPSQL(ctx context.Context, cfg config, ids ids, db, sql string, query bool) (string, error) {
	args := []string{"-v", "ON_ERROR_STOP=1", "--no-psqlrc", "-h", runDir, "-U", "postgres", "-d", db, "-f", "-"}
	if query {
		args = append([]string{"-Atq"}, args...)
	}
	cmd := exec.CommandContext(ctx, psqlPath(cfg), args...)
	cmd.Env = postgresEnv(cfg)
	cmd.Stdin = strings.NewReader(sql + "\n")
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Credential: &syscall.Credential{Uid: uint32(ids.PostgresUID), Gid: uint32(ids.PostgresGID)},
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg != "" {
			return "", fmt.Errorf("psql %s: %w: %s", db, err, msg)
		}
		return "", fmt.Errorf("psql %s: %w", db, err)
	}
	return stdout.String(), nil
}

func runAsPostgres(ctx context.Context, cfg config, ids ids, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = postgresEnv(cfg)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Credential: &syscall.Credential{Uid: uint32(ids.PostgresUID), Gid: uint32(ids.PostgresGID)},
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg != "" {
			return fmt.Errorf("%s: %w: %s", filepath.Base(name), err, msg)
		}
		return fmt.Errorf("%s: %w", filepath.Base(name), err)
	}
	return nil
}

func runCommand(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	var stderr bytes.Buffer
	cmd.Stdout = io.Discard
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg != "" {
			return fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, msg)
		}
		return fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err)
	}
	return nil
}

func postgresEnv(cfg config) []string {
	return append(os.Environ(),
		"HOME="+homeDir,
		"LD_LIBRARY_PATH="+filepath.Join(cfg.RuntimeRoot, "usr/lib/x86_64-linux-gnu")+":"+filepath.Join(cfg.RuntimeRoot, "usr/lib/postgresql/16/lib"),
	)
}

func postgresPath(cfg config) string {
	return filepath.Join(cfg.RuntimeRoot, "usr/lib/postgresql/16/bin/postgres")
}

func initdbPath(cfg config) string {
	return filepath.Join(cfg.RuntimeRoot, "usr/lib/postgresql/16/bin/initdb")
}

func psqlPath(cfg config) string {
	return filepath.Join(cfg.RuntimeRoot, "usr/lib/postgresql/16/bin/psql")
}

func writeFile(path string, body []byte, mode os.FileMode, uid, gid int) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("mkdir %s: %w", filepath.Dir(path), err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, body, mode); err != nil {
		return fmt.Errorf("write %s: %w", tmp, err)
	}
	if err := os.Chown(tmp, uid, gid); err != nil {
		return fmt.Errorf("chown %s: %w", tmp, err)
	}
	if err := os.Chmod(tmp, mode); err != nil {
		return fmt.Errorf("chmod %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("install %s: %w", path, err)
	}
	return nil
}

func quoteIdent(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `""`) + `"`
}

func qualifiedTable(schema, table string) string {
	return quoteIdent(schema) + "." + quoteIdent(table)
}

func qualifiedRegclass(schema, table string) string {
	return schema + "." + quoteIdent(table)
}

func quoteLiteral(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}
