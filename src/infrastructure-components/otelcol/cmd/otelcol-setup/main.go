package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

const (
	otelUser      = "otelcol"
	otelGroup     = "otelcol"
	otelHome      = "/var/lib/otelcol"
	otelConfigDir = "/etc/otelcol"

	clickhouseCAPath       = "/etc/clickhouse-server/tls/server-ca.pem"
	otelClickHouseCAPath   = "/etc/otelcol/clickhouse-server-ca.pem"
	clickhouseClient       = "/opt/verself/profile/bin/clickhouse-client"
	clickhouseOperatorUser = "clickhouse_operator"

	postgresDataDir     = "/var/lib/postgresql/16/verself"
	postgresIdentPath   = "/etc/postgresql/verself/pg_ident.conf"
	postgresqlClient    = "/opt/verself/postgresql/usr/lib/postgresql/16/bin/psql"
	postgresqlLibDir    = "/opt/verself/postgresql/usr/lib/x86_64-linux-gnu"
	postgresPeerMapLine = "verself_services      otelcol            otelcol"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "otelcol-setup: "+err.Error())
		os.Exit(1)
	}
}

func run() error {
	if os.Geteuid() != 0 {
		return errors.New("must run as root")
	}
	if err := ensureGroup(otelGroup); err != nil {
		return err
	}
	if err := ensureUser(otelUser, otelGroup, []string{"spire_workload", "adm", "systemd-journal"}); err != nil {
		return err
	}
	if err := ensureDirectories(); err != nil {
		return err
	}
	if err := copyFile(clickhouseCAPath, otelClickHouseCAPath, "root", otelGroup, 0o640); err != nil {
		return err
	}
	if err := ensureClickHouseUser(); err != nil {
		return err
	}
	if err := ensurePostgresUser(); err != nil {
		return err
	}
	return nil
}

func ensureGroup(name string) error {
	if err := command("getent", "group", name).Run(); err == nil {
		return nil
	}
	return runCommand("groupadd", "--system", name)
}

func ensureUser(name, group string, groups []string) error {
	if err := command("id", "-u", name).Run(); err != nil {
		args := []string{
			"--system",
			"--gid", group,
			"--groups", strings.Join(groups, ","),
			"--home-dir", otelHome,
			"--shell", "/usr/sbin/nologin",
			"--no-create-home",
			name,
		}
		if err := runCommand("useradd", args...); err != nil {
			return err
		}
	}
	return runCommand("usermod",
		"--gid", group,
		"--groups", strings.Join(groups, ","),
		"--home", otelHome,
		"--shell", "/usr/sbin/nologin",
		name,
	)
}

func ensureDirectories() error {
	uid, gid, err := uidGID(otelUser, otelGroup)
	if err != nil {
		return err
	}
	rootUID, otelGID, err := uidGID("root", otelGroup)
	if err != nil {
		return err
	}
	dirs := []struct {
		path string
		uid  int
		gid  int
		mode os.FileMode
	}{
		{otelConfigDir, rootUID, otelGID, 0o750},
		{otelHome, uid, gid, 0o750},
		{filepath.Join(otelHome, "storage"), uid, gid, 0o750},
		{filepath.Join(otelHome, "clickhouse-spiffe"), uid, gid, 0o700},
	}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir.path, dir.mode); err != nil {
			return fmt.Errorf("mkdir %s: %w", dir.path, err)
		}
		if err := os.Chown(dir.path, dir.uid, dir.gid); err != nil {
			return fmt.Errorf("chown %s: %w", dir.path, err)
		}
		if err := os.Chmod(dir.path, dir.mode); err != nil {
			return fmt.Errorf("chmod %s: %w", dir.path, err)
		}
	}
	return nil
}

func ensureClickHouseUser() error {
	sql := `
CREATE USER IF NOT EXISTS otelcol IDENTIFIED WITH ssl_certificate SAN 'URI:spiffe://spiffe.verself.sh/svc/otelcol' HOST LOCAL;
ALTER USER otelcol IDENTIFIED WITH ssl_certificate SAN 'URI:spiffe://spiffe.verself.sh/svc/otelcol' HOST LOCAL;
GRANT INSERT ON default.* TO otelcol;
GRANT SELECT ON default.otel_logs TO otelcol;
GRANT SELECT (Timestamp, SpanAttributes, SpanName, TraceId, SpanId, ServiceName) ON default.otel_traces TO otelcol;
`
	cmd := command("sudo",
		"-u", clickhouseOperatorUser,
		clickhouseClient,
		"--config-file", "/etc/clickhouse-client/operator.xml",
		"--user", clickhouseOperatorUser,
		"--multiquery",
	)
	cmd.Stdin = strings.NewReader(sql)
	return runPrepared(cmd)
}

func ensurePostgresUser() error {
	if err := ensurePostgresPeerMap(); err != nil {
		return err
	}
	sql := `
DO $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'otelcol') THEN
    CREATE ROLE otelcol LOGIN;
  END IF;
END $$;
GRANT pg_monitor TO otelcol;
`
	cmd := command("sudo",
		"-u", "postgres",
		postgresqlClient,
		"-h", "/var/run/postgresql",
		"-U", "postgres",
		"-d", "postgres",
		"-v", "ON_ERROR_STOP=1",
	)
	cmd.Env = append(os.Environ(), "LD_LIBRARY_PATH="+postgresqlLibDir)
	cmd.Stdin = strings.NewReader(sql)
	return runPrepared(cmd)
}

func ensurePostgresPeerMap() error {
	body, err := os.ReadFile(postgresIdentPath)
	if err != nil {
		return fmt.Errorf("read %s: %w", postgresIdentPath, err)
	}
	if hasFieldsLine(body, []string{"verself_services", "otelcol", "otelcol"}) {
		return nil
	}
	var next bytes.Buffer
	next.Write(bytes.TrimRight(body, "\n"))
	next.WriteByte('\n')
	next.WriteString(postgresPeerMapLine)
	next.WriteByte('\n')
	if err := atomicWrite(postgresIdentPath, next.Bytes(), "postgres", "postgres", 0o600); err != nil {
		return err
	}
	return signalPostgresReload()
}

func hasFieldsLine(body []byte, fields []string) bool {
	for _, line := range strings.Split(string(body), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if equalStrings(strings.Fields(trimmed), fields) {
			return true
		}
	}
	return false
}

func signalPostgresReload() error {
	body, err := os.ReadFile(filepath.Join(postgresDataDir, "postmaster.pid"))
	if err != nil {
		return fmt.Errorf("read postgres pid: %w", err)
	}
	line, _, _ := strings.Cut(string(body), "\n")
	pid, err := strconv.Atoi(strings.TrimSpace(line))
	if err != nil || pid <= 0 {
		return fmt.Errorf("parse postgres pid %q", strings.TrimSpace(line))
	}
	if err := syscall.Kill(pid, syscall.SIGHUP); err != nil {
		return fmt.Errorf("reload postgres pid %d: %w", pid, err)
	}
	return nil
}

func copyFile(src, dest, owner, group string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open %s: %w", src, err)
	}
	defer func() { _ = in.Close() }()
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, in); err != nil {
		return fmt.Errorf("read %s: %w", src, err)
	}
	return atomicWrite(dest, buf.Bytes(), owner, group, mode)
}

func atomicWrite(path string, body []byte, owner, group string, mode os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create temp for %s: %w", path, err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if _, err := tmp.Write(body); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write %s: %w", tmpName, err)
	}
	uid, gid, err := uidGID(owner, group)
	if err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chown(uid, gid); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("chown %s: %w", tmpName, err)
	}
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("chmod %s: %w", tmpName, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close %s: %w", tmpName, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("rename %s to %s: %w", tmpName, path, err)
	}
	return nil
}

func uidGID(owner, group string) (int, int, error) {
	u, err := user.Lookup(owner)
	if err != nil {
		return 0, 0, fmt.Errorf("lookup user %s: %w", owner, err)
	}
	g, err := user.LookupGroup(group)
	if err != nil {
		return 0, 0, fmt.Errorf("lookup group %s: %w", group, err)
	}
	uid, err := strconv.Atoi(u.Uid)
	if err != nil {
		return 0, 0, fmt.Errorf("parse uid for %s: %w", owner, err)
	}
	gid, err := strconv.Atoi(g.Gid)
	if err != nil {
		return 0, 0, fmt.Errorf("parse gid for %s: %w", group, err)
	}
	return uid, gid, nil
}

func command(name string, args ...string) *exec.Cmd {
	return exec.Command(name, args...)
}

func runCommand(name string, args ...string) error {
	return runPrepared(command(name, args...))
}

func runPrepared(cmd *exec.Cmd) error {
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %s: %w: %s", cmd.Path, strings.Join(cmd.Args[1:], " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
