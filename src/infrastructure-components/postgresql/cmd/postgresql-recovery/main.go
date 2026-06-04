package main

import (
	"bufio"
	"compress/gzip"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

const (
	component          = "postgresql"
	mechanism          = "pg_dumpall_logical"
	backupFileName     = "postgresql.sql.gz"
	manifestFileName   = "manifest.json"
	evidencePrefix     = "verify-"
	defaultCacheRoot   = "/var/lib/verself/recovery-cache"
	defaultVerifyRoot  = "/var/lib/verself/recovery-verify/postgresql"
	defaultRuntimeRoot = "/opt/verself/postgresql"
	postgresMajor      = "16"
	restorePort        = "55432"
	restoreUser        = "postgres"
)

var safeSegmentPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]*$`)

type config struct {
	action      string
	site        string
	cacheRoot   string
	verifyRoot  string
	runtimeRoot string
	point       string
	keepVerify  bool
}

type pgTools struct {
	binDir        string
	shareDir      string
	ldLibraryPath string
}

type backupManifest struct {
	Site                     string   `json:"site"`
	Component                string   `json:"component"`
	Mechanism                string   `json:"mechanism"`
	CacheKey                 string   `json:"cache_key"`
	Point                    string   `json:"point"`
	StartedAt                string   `json:"started_at"`
	FinishedAt               string   `json:"finished_at"`
	PostgresVersion          string   `json:"postgres_version"`
	PostgresSystemIdentifier string   `json:"postgres_system_identifier"`
	DatabaseNames            []string `json:"database_names"`
	RoleCount                int      `json:"role_count"`
	SHA256                   string   `json:"sha256"`
	SizeBytes                int64    `json:"size_bytes"`
}

type verifyEvidence struct {
	Site                  string   `json:"site"`
	Component             string   `json:"component"`
	Mechanism             string   `json:"mechanism"`
	Point                 string   `json:"point"`
	BackupCacheKey        string   `json:"backup_cache_key"`
	StartedAt             string   `json:"started_at"`
	FinishedAt            string   `json:"finished_at"`
	Result                string   `json:"result"`
	BackupSHA256          string   `json:"backup_sha256"`
	BackupSizeBytes       int64    `json:"backup_size_bytes"`
	ExpectedRoleCount     int      `json:"expected_role_count"`
	RestoredRoleCount     int      `json:"restored_role_count"`
	ExpectedDatabaseNames []string `json:"expected_database_names"`
	RestoredDatabaseNames []string `json:"restored_database_names"`
	VerifyDirectory       string   `json:"verify_directory,omitempty"`
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "postgresql-recovery: "+err.Error())
		os.Exit(1)
	}
}

func run(args []string) error {
	cfg := config{}
	fs := flag.NewFlagSet("postgresql-recovery", flag.ContinueOnError)
	fs.StringVar(&cfg.action, "action", "", "Recovery action: backup, verify, or exercise.")
	fs.StringVar(&cfg.site, "site", "", "Site name.")
	fs.StringVar(&cfg.cacheRoot, "cache-root", defaultCacheRoot, "Recovery cache root.")
	fs.StringVar(&cfg.verifyRoot, "verify-root", defaultVerifyRoot, "Temporary restore verification root.")
	fs.StringVar(&cfg.runtimeRoot, "runtime-root", defaultRuntimeRoot, "PostgreSQL runtime root.")
	fs.StringVar(&cfg.point, "point", "", "Recovery point for verify. Use latest to select the newest point.")
	fs.BoolVar(&cfg.keepVerify, "keep-verify", false, "Keep temporary restore data after verification.")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := cfg.validate(); err != nil {
		return err
	}
	var err error
	if cfg.cacheRoot, err = filepath.Abs(cfg.cacheRoot); err != nil {
		return fmt.Errorf("resolve --cache-root: %w", err)
	}
	if cfg.verifyRoot, err = filepath.Abs(cfg.verifyRoot); err != nil {
		return fmt.Errorf("resolve --verify-root: %w", err)
	}
	if cfg.runtimeRoot, err = filepath.Abs(cfg.runtimeRoot); err != nil {
		return fmt.Errorf("resolve --runtime-root: %w", err)
	}
	tools := pgToolsFromRuntime(cfg.runtimeRoot)
	ctx := context.Background()
	switch cfg.action {
	case "backup":
		manifest, err := backup(ctx, cfg, tools)
		if err != nil {
			return err
		}
		printSummary(map[string]any{
			"action":         "backup",
			"cache_key":      manifest.CacheKey,
			"point":          manifest.Point,
			"sha256":         manifest.SHA256,
			"size_bytes":     manifest.SizeBytes,
			"database_count": len(manifest.DatabaseNames),
		})
	case "verify":
		evidence, err := verify(ctx, cfg, tools)
		if err != nil {
			return err
		}
		printSummary(map[string]any{
			"action":                  "verify",
			"point":                   evidence.Point,
			"result":                  evidence.Result,
			"restored_database_count": len(evidence.RestoredDatabaseNames),
			"restored_role_count":     evidence.RestoredRoleCount,
		})
	case "exercise":
		manifest, err := backup(ctx, cfg, tools)
		if err != nil {
			return err
		}
		cfg.point = manifest.Point
		evidence, err := verify(ctx, cfg, tools)
		if err != nil {
			return err
		}
		printSummary(map[string]any{
			"action":                  "exercise",
			"cache_key":               manifest.CacheKey,
			"point":                   manifest.Point,
			"sha256":                  manifest.SHA256,
			"size_bytes":              manifest.SizeBytes,
			"source_database_count":   len(manifest.DatabaseNames),
			"restored_database_count": len(evidence.RestoredDatabaseNames),
			"restored_role_count":     evidence.RestoredRoleCount,
			"result":                  evidence.Result,
		})
	default:
		return fmt.Errorf("unsupported --action %q", cfg.action)
	}
	return nil
}

func (cfg config) validate() error {
	if strings.TrimSpace(cfg.action) == "" {
		return errors.New("--action is required")
	}
	if !safeSegment(cfg.site) {
		return errors.New("--site must be a non-empty safe path segment")
	}
	if strings.TrimSpace(cfg.cacheRoot) == "" {
		return errors.New("--cache-root is required")
	}
	if strings.TrimSpace(cfg.runtimeRoot) == "" {
		return errors.New("--runtime-root is required")
	}
	if strings.TrimSpace(cfg.verifyRoot) == "" {
		return errors.New("--verify-root is required")
	}
	if cfg.action == "verify" && strings.TrimSpace(cfg.point) == "" {
		return errors.New("--point is required for verify; use latest to select the newest point")
	}
	if cfg.point != "" && cfg.point != "latest" && !safeSegment(cfg.point) {
		return errors.New("--point must be latest or a safe path segment")
	}
	return nil
}

func pgToolsFromRuntime(root string) pgTools {
	return pgTools{
		binDir:        filepath.Join(root, "usr", "lib", "postgresql", postgresMajor, "bin"),
		shareDir:      filepath.Join(root, "usr", "share", "postgresql", postgresMajor),
		ldLibraryPath: strings.Join([]string{filepath.Join(root, "usr", "lib", "x86_64-linux-gnu"), filepath.Join(root, "usr", "lib", "postgresql", postgresMajor, "lib")}, ":"),
	}
}

func backup(ctx context.Context, cfg config, tools pgTools) (backupManifest, error) {
	started := time.Now().UTC()
	point, err := newPoint(started)
	if err != nil {
		return backupManifest{}, err
	}
	dir := backupDir(cfg.cacheRoot, cfg.site, point)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return backupManifest{}, fmt.Errorf("create backup directory: %w", err)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return backupManifest{}, fmt.Errorf("chmod backup directory: %w", err)
	}
	meta, err := sourceMetadata(ctx, tools)
	if err != nil {
		return backupManifest{}, err
	}
	finalPath := filepath.Join(dir, backupFileName)
	tmpPath := finalPath + ".tmp"
	if err := writeLogicalDump(ctx, tools, tmpPath); err != nil {
		_ = os.Remove(tmpPath)
		return backupManifest{}, err
	}
	if err := os.Rename(tmpPath, finalPath); err != nil {
		_ = os.Remove(tmpPath)
		return backupManifest{}, fmt.Errorf("commit backup file: %w", err)
	}
	if err := os.Chmod(finalPath, 0o600); err != nil {
		return backupManifest{}, fmt.Errorf("chmod backup file: %w", err)
	}
	sha, size, err := fileDigest(finalPath)
	if err != nil {
		return backupManifest{}, err
	}
	manifest := backupManifest{
		Site:                     cfg.site,
		Component:                component,
		Mechanism:                mechanism,
		CacheKey:                 recoveryKey(cfg.site, point, backupFileName),
		Point:                    point,
		StartedAt:                started.Format(time.RFC3339),
		FinishedAt:               time.Now().UTC().Format(time.RFC3339),
		PostgresVersion:          meta.version,
		PostgresSystemIdentifier: meta.systemIdentifier,
		DatabaseNames:            meta.databases,
		RoleCount:                meta.roleCount,
		SHA256:                   sha,
		SizeBytes:                size,
	}
	if err := writeJSONFile(filepath.Join(dir, manifestFileName), manifest, 0o600); err != nil {
		return backupManifest{}, err
	}
	return manifest, nil
}

type metadata struct {
	version          string
	systemIdentifier string
	databases        []string
	roleCount        int
}

func sourceMetadata(ctx context.Context, tools pgTools) (metadata, error) {
	versionSystem, err := psqlOutput(ctx, tools, "", "select current_setting('server_version'), system_identifier from pg_control_system();")
	if err != nil {
		return metadata{}, fmt.Errorf("query PostgreSQL system metadata: %w", err)
	}
	parts := strings.Split(strings.TrimSpace(versionSystem), "|")
	if len(parts) != 2 {
		return metadata{}, fmt.Errorf("unexpected PostgreSQL system metadata %q", strings.TrimSpace(versionSystem))
	}
	databaseOutput, err := psqlOutput(ctx, tools, "", "select datname from pg_database where datistemplate = false order by datname;")
	if err != nil {
		return metadata{}, fmt.Errorf("query PostgreSQL databases: %w", err)
	}
	roleOutput, err := psqlOutput(ctx, tools, "", "select count(*) from pg_roles;")
	if err != nil {
		return metadata{}, fmt.Errorf("query PostgreSQL roles: %w", err)
	}
	var roleCount int
	if _, err := fmt.Sscanf(strings.TrimSpace(roleOutput), "%d", &roleCount); err != nil {
		return metadata{}, fmt.Errorf("parse PostgreSQL role count %q: %w", strings.TrimSpace(roleOutput), err)
	}
	return metadata{
		version:          strings.TrimSpace(parts[0]),
		systemIdentifier: strings.TrimSpace(parts[1]),
		databases:        nonEmptyLines(databaseOutput),
		roleCount:        roleCount,
	}, nil
}

func writeLogicalDump(ctx context.Context, tools pgTools, path string) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("create backup file: %w", err)
	}
	closeFile := true
	defer func() {
		if closeFile {
			_ = file.Close()
		}
	}()
	gzipWriter := gzip.NewWriter(file)
	dump := exec.CommandContext(ctx, filepath.Join(tools.binDir, "pg_dumpall"), "--clean", "--if-exists", "--quote-all-identifiers")
	dump.Env = commandEnv(tools)
	dump.Stdout = gzipWriter
	var stderr strings.Builder
	dump.Stderr = &stderr
	if err := dump.Run(); err != nil {
		_ = gzipWriter.Close()
		return fmt.Errorf("pg_dumpall failed: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	if err := gzipWriter.Close(); err != nil {
		return fmt.Errorf("finish gzip backup: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close backup file: %w", err)
	}
	closeFile = false
	return nil
}

func verify(ctx context.Context, cfg config, tools pgTools) (verifyEvidence, error) {
	point, err := resolvePoint(cfg.cacheRoot, cfg.site, cfg.point)
	if err != nil {
		return verifyEvidence{}, err
	}
	dir := backupDir(cfg.cacheRoot, cfg.site, point)
	manifest, err := readManifest(filepath.Join(dir, manifestFileName))
	if err != nil {
		return verifyEvidence{}, err
	}
	if manifest.Site != cfg.site || manifest.Point != point {
		return verifyEvidence{}, fmt.Errorf("backup manifest identity site=%s point=%s does not match requested site=%s point=%s", manifest.Site, manifest.Point, cfg.site, point)
	}
	backupPath := filepath.Join(dir, backupFileName)
	sha, size, err := fileDigest(backupPath)
	if err != nil {
		return verifyEvidence{}, err
	}
	if sha != manifest.SHA256 {
		return verifyEvidence{}, fmt.Errorf("backup sha256=%s does not match manifest %s", sha, manifest.SHA256)
	}
	if size != manifest.SizeBytes {
		return verifyEvidence{}, fmt.Errorf("backup size=%d does not match manifest %d", size, manifest.SizeBytes)
	}
	started := time.Now().UTC()
	if err := os.MkdirAll(cfg.verifyRoot, 0o700); err != nil {
		return verifyEvidence{}, fmt.Errorf("create restore verification root: %w", err)
	}
	restoreDir, err := os.MkdirTemp(cfg.verifyRoot, point+"-")
	if err != nil {
		return verifyEvidence{}, fmt.Errorf("create restore verification directory: %w", err)
	}
	if err := os.Chmod(restoreDir, 0o700); err != nil {
		return verifyEvidence{}, fmt.Errorf("chmod restore verification directory: %w", err)
	}
	removeRestore := !cfg.keepVerify
	defer func() {
		if removeRestore {
			_ = os.RemoveAll(restoreDir)
		}
	}()
	restored, err := restoreAndInspect(ctx, tools, backupPath, restoreDir)
	if err != nil {
		writeFailureEvidence(dir, point, manifest, started, restoreDir, err)
		return verifyEvidence{}, err
	}
	if !sameStrings(manifest.DatabaseNames, restored.databases) {
		err := fmt.Errorf("restored database inventory %v does not match manifest %v", restored.databases, manifest.DatabaseNames)
		writeFailureEvidence(dir, point, manifest, started, restoreDir, err)
		return verifyEvidence{}, err
	}
	if manifest.RoleCount != restored.roleCount {
		err := fmt.Errorf("restored role count %d does not match manifest %d", restored.roleCount, manifest.RoleCount)
		writeFailureEvidence(dir, point, manifest, started, restoreDir, err)
		return verifyEvidence{}, err
	}
	evidence := verifyEvidence{
		Site:                  cfg.site,
		Component:             component,
		Mechanism:             mechanism,
		Point:                 point,
		BackupCacheKey:        manifest.CacheKey,
		StartedAt:             started.Format(time.RFC3339),
		FinishedAt:            time.Now().UTC().Format(time.RFC3339),
		Result:                "succeeded",
		BackupSHA256:          sha,
		BackupSizeBytes:       size,
		ExpectedRoleCount:     manifest.RoleCount,
		RestoredRoleCount:     restored.roleCount,
		ExpectedDatabaseNames: manifest.DatabaseNames,
		RestoredDatabaseNames: restored.databases,
	}
	if cfg.keepVerify {
		evidence.VerifyDirectory = restoreDir
		removeRestore = false
	}
	if err := writeJSONFile(filepath.Join(dir, evidencePrefix+timestampForFile(time.Now().UTC())+".json"), evidence, 0o600); err != nil {
		return verifyEvidence{}, err
	}
	return evidence, nil
}

type restoreInspection struct {
	databases []string
	roleCount int
}

func restoreAndInspect(ctx context.Context, tools pgTools, backupPath, restoreDir string) (restoreInspection, error) {
	pgdata := filepath.Join(restoreDir, "pgdata")
	socketDir := filepath.Join(restoreDir, "socket")
	if err := os.MkdirAll(socketDir, 0o700); err != nil {
		return restoreInspection{}, fmt.Errorf("create restore socket directory: %w", err)
	}
	initdb := exec.CommandContext(ctx, filepath.Join(tools.binDir, "initdb"),
		"--pgdata="+pgdata,
		"--auth-local=trust",
		"--auth-host=reject",
		"--encoding=UTF8",
		"--locale=C.UTF-8",
		"--username="+restoreUser,
		"-L", tools.shareDir,
	)
	initdb.Env = commandEnv(tools)
	if output, err := initdb.CombinedOutput(); err != nil {
		return restoreInspection{}, fmt.Errorf("initdb restore cluster: %w: %s", err, strings.TrimSpace(string(output)))
	}
	logFile, err := os.OpenFile(filepath.Join(restoreDir, "postgres.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return restoreInspection{}, fmt.Errorf("create restore postgres log: %w", err)
	}
	defer logFile.Close()
	postgres := exec.CommandContext(ctx, filepath.Join(tools.binDir, "postgres"),
		"-D", pgdata,
		"-c", "listen_addresses=",
		"-c", "unix_socket_directories="+socketDir,
		"-c", "port="+restorePort,
	)
	postgres.Env = commandEnv(tools)
	postgres.Stdout = logFile
	postgres.Stderr = logFile
	if err := postgres.Start(); err != nil {
		return restoreInspection{}, fmt.Errorf("start restore postgres: %w", err)
	}
	stopped := false
	defer func() {
		if !stopped && postgres.Process != nil {
			_ = postgres.Process.Signal(os.Interrupt)
			_, _ = postgres.Process.Wait()
		}
	}()
	if err := waitForPostgres(ctx, tools, socketDir); err != nil {
		return restoreInspection{}, err
	}
	if err := restoreDump(ctx, tools, socketDir, backupPath); err != nil {
		return restoreInspection{}, err
	}
	restoredOutput, err := psqlOutput(ctx, tools, socketDir, "select datname from pg_database where datistemplate = false order by datname;")
	if err != nil {
		return restoreInspection{}, fmt.Errorf("query restored databases: %w", err)
	}
	roleOutput, err := psqlOutput(ctx, tools, socketDir, "select count(*) from pg_roles;")
	if err != nil {
		return restoreInspection{}, fmt.Errorf("query restored roles: %w", err)
	}
	var roleCount int
	if _, err := fmt.Sscanf(strings.TrimSpace(roleOutput), "%d", &roleCount); err != nil {
		return restoreInspection{}, fmt.Errorf("parse restored role count %q: %w", strings.TrimSpace(roleOutput), err)
	}
	if err := stopPostgres(ctx, tools, pgdata); err != nil {
		return restoreInspection{}, err
	}
	stopped = true
	if err := postgres.Wait(); err != nil {
		return restoreInspection{}, fmt.Errorf("restore postgres exited with error: %w", err)
	}
	return restoreInspection{databases: nonEmptyLines(restoredOutput), roleCount: roleCount}, nil
}

func waitForPostgres(ctx context.Context, tools pgTools, socketDir string) error {
	deadline := time.Now().Add(30 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		_, err := psqlOutput(ctx, tools, socketDir, "select 1;")
		if err == nil {
			return nil
		}
		lastErr = err
		time.Sleep(250 * time.Millisecond)
	}
	return fmt.Errorf("restore postgres did not become ready: %w", lastErr)
}

func restoreDump(ctx context.Context, tools pgTools, socketDir, backupPath string) error {
	file, err := os.Open(backupPath)
	if err != nil {
		return fmt.Errorf("open backup for restore: %w", err)
	}
	defer file.Close()
	gzipReader, err := gzip.NewReader(file)
	if err != nil {
		return fmt.Errorf("open gzip backup: %w", err)
	}
	defer gzipReader.Close()
	restoreReader, filterDone := filterDumpForRestore(gzipReader, restoreUser)
	psql := exec.CommandContext(ctx, filepath.Join(tools.binDir, "psql"), "-h", socketDir, "-p", restorePort, "-d", "postgres", "-U", restoreUser, "-v", "ON_ERROR_STOP=1", "-f", "-")
	psql.Env = commandEnv(tools)
	psql.Stdin = restoreReader
	var stderr strings.Builder
	psql.Stderr = &stderr
	if err := psql.Run(); err != nil {
		_ = restoreReader.Close()
		<-filterDone
		return fmt.Errorf("restore logical dump: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	if err := <-filterDone; err != nil {
		return fmt.Errorf("filter logical dump for restore: %w", err)
	}
	return nil
}

func filterDumpForRestore(source io.Reader, currentUser string) (*io.PipeReader, <-chan error) {
	reader, writer := io.Pipe()
	done := make(chan error, 1)
	go func() {
		err := copyDumpForRestore(writer, source, currentUser)
		if err != nil {
			_ = writer.CloseWithError(err)
		} else {
			_ = writer.Close()
		}
		done <- err
	}()
	return reader, done
}

func copyDumpForRestore(dest io.Writer, source io.Reader, currentUser string) error {
	reader := bufio.NewReader(source)
	for {
		line, err := reader.ReadString('\n')
		if line != "" && !skipCurrentUserBootstrapLine(line, currentUser) {
			if _, writeErr := io.WriteString(dest, line); writeErr != nil {
				return writeErr
			}
		}
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

func skipCurrentUserBootstrapLine(line, currentUser string) bool {
	trimmed := strings.TrimSpace(line)
	quoted := `"` + strings.ReplaceAll(currentUser, `"`, `""`) + `"`
	for _, action := range []string{"DROP ROLE IF EXISTS ", "CREATE ROLE "} {
		if trimmed == action+quoted+";" || trimmed == action+currentUser+";" {
			return true
		}
	}
	return false
}

func stopPostgres(ctx context.Context, tools pgTools, pgdata string) error {
	stop := exec.CommandContext(ctx, filepath.Join(tools.binDir, "pg_ctl"), "-D", pgdata, "-m", "fast", "stop")
	stop.Env = commandEnv(tools)
	if output, err := stop.CombinedOutput(); err != nil {
		return fmt.Errorf("stop restore postgres: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func psqlOutput(ctx context.Context, tools pgTools, socketDir, query string) (string, error) {
	args := []string{"-A", "-t", "-q", "-d", "postgres", "-c", query}
	if socketDir != "" {
		args = append([]string{"-h", socketDir, "-p", restorePort, "-U", restoreUser}, args...)
	}
	cmd := exec.CommandContext(ctx, filepath.Join(tools.binDir, "psql"), args...)
	cmd.Env = commandEnv(tools)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("psql: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return string(output), nil
}

func commandEnv(tools pgTools) []string {
	env := os.Environ()
	env = append(env, "LD_LIBRARY_PATH="+tools.ldLibraryPath)
	return env
}

func backupDir(root, site, point string) string {
	return filepath.Join(root, "dr", "v1", "sites", site, "components", component, "mechanisms", mechanism, "points", point)
}

func pointsDir(root, site string) string {
	return filepath.Join(root, "dr", "v1", "sites", site, "components", component, "mechanisms", mechanism, "points")
}

func recoveryKey(site, point, name string) string {
	return strings.Join([]string{"dr", "v1", "sites", site, "components", component, "mechanisms", mechanism, "points", point, name}, "/")
}

func resolvePoint(root, site, point string) (string, error) {
	if point != "latest" {
		return point, nil
	}
	entries, err := os.ReadDir(pointsDir(root, site))
	if err != nil {
		return "", fmt.Errorf("read recovery points: %w", err)
	}
	var points []string
	for _, entry := range entries {
		if entry.IsDir() && safeSegment(entry.Name()) {
			points = append(points, entry.Name())
		}
	}
	if len(points) == 0 {
		return "", errors.New("no recovery points found")
	}
	sort.Strings(points)
	return points[len(points)-1], nil
}

func newPoint(now time.Time) (string, error) {
	var raw [6]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("generate recovery point randomness: %w", err)
	}
	return timestampForFile(now) + "-" + hex.EncodeToString(raw[:]), nil
}

func timestampForFile(t time.Time) string {
	return t.UTC().Format("20060102T150405Z")
}

func safeSegment(value string) bool {
	return safeSegmentPattern.MatchString(value)
}

func fileDigest(path string) (string, int64, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", 0, fmt.Errorf("open %s for digest: %w", path, err)
	}
	defer file.Close()
	hash := sha256.New()
	size, err := io.Copy(hash, file)
	if err != nil {
		return "", 0, fmt.Errorf("hash %s: %w", path, err)
	}
	return hex.EncodeToString(hash.Sum(nil)), size, nil
}

func readManifest(path string) (backupManifest, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return backupManifest{}, fmt.Errorf("read backup manifest: %w", err)
	}
	var manifest backupManifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return backupManifest{}, fmt.Errorf("decode backup manifest: %w", err)
	}
	if manifest.Site == "" || manifest.Component != component || manifest.Mechanism != mechanism || manifest.Point == "" || manifest.SHA256 == "" {
		return backupManifest{}, errors.New("backup manifest is missing required recovery identity")
	}
	return manifest, nil
}

func writeJSONFile(path string, value any, mode os.FileMode) error {
	file, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary file for %s: %w", path, err)
	}
	tmp := file.Name()
	committed := false
	defer func() {
		if !committed {
			_ = os.Remove(tmp)
		}
	}()
	if err := file.Chmod(mode); err != nil {
		_ = file.Close()
		return fmt.Errorf("chmod %s: %w", tmp, err)
	}
	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(value); err != nil {
		_ = file.Close()
		return fmt.Errorf("encode %s: %w", tmp, err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("commit %s: %w", path, err)
	}
	committed = true
	if err := os.Chmod(path, mode); err != nil {
		return fmt.Errorf("chmod %s: %w", path, err)
	}
	return nil
}

func writeFailureEvidence(dir, point string, manifest backupManifest, started time.Time, restoreDir string, cause error) {
	evidence := map[string]any{
		"site":                    manifest.Site,
		"component":               component,
		"mechanism":               mechanism,
		"point":                   point,
		"backup_cache_key":        manifest.CacheKey,
		"started_at":              started.Format(time.RFC3339),
		"finished_at":             time.Now().UTC().Format(time.RFC3339),
		"result":                  "failed",
		"reason":                  cause.Error(),
		"expected_role_count":     manifest.RoleCount,
		"expected_database_names": manifest.DatabaseNames,
		"verify_directory":        restoreDir,
	}
	_ = writeJSONFile(filepath.Join(dir, evidencePrefix+timestampForFile(time.Now().UTC())+".json"), evidence, 0o600)
}

func nonEmptyLines(raw string) []string {
	scanner := bufio.NewScanner(strings.NewReader(raw))
	var lines []string
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

func sameStrings(a, b []string) bool {
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

func printSummary(value map[string]any) {
	raw, err := json.Marshal(value)
	if err != nil {
		fmt.Printf("{\"result\":\"summary_marshal_failed\"}\n")
		return
	}
	fmt.Println(string(raw))
}
