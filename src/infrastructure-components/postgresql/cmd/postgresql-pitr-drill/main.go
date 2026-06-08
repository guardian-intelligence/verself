package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"math"
	"os"
	"os/exec"
	osuser "os/user"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	ch "github.com/ClickHouse/clickhouse-go/v2"
	chdriver "github.com/ClickHouse/clickhouse-go/v2/lib/driver"
)

const (
	archiveBoundaryAfterWrite = "after-write"
	archiveBoundaryNone       = "none"
	component                 = "postgresql"
	databaseName              = "verself_pitr_drill"
	defaultArchiveTimeout     = time.Second
	defaultCacheRoot          = "/var/lib/verself/recovery-cache"
	defaultClickHouseXML      = "/etc/clickhouse-client/operator.xml"
	defaultDrillRoot          = "/var/lib/verself/recovery-drill/postgresql"
	defaultRuntimeRoot        = "/opt/verself/postgresql-recovery"
	defaultWriterInterval     = 100 * time.Millisecond
	defaultWriterSeconds      = 8
	evidenceTable             = "verself.recovery_events"
	mechanism                 = "pgbackrest_pitr_drill"
	pointTimestampLayout      = "20060102T150405Z"
	postgresMajor             = "16"
	recoverySchemaVersion     = 1
	restoreUser               = "postgres"
)

var safeSegmentPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

type options struct {
	site                 string
	drills               int
	runtimeRoot          string
	drillRoot            string
	cacheRoot            string
	writerSeconds        int
	writerInterval       time.Duration
	archiveTimeout       time.Duration
	archiveBoundary      string
	sourcePortBase       int
	keepScratch          bool
	recordClickHouse     bool
	clickHouseConfigPath string
}

type pgTools struct {
	binDir        string
	shareDir      string
	pgBackRest    string
	ldLibraryPath string
}

type credential struct {
	uid uint32
	gid uint32
}

type runner struct {
	opts      options
	tools     pgTools
	postgres  credential
	clickConn chdriver.Conn
}

type drillReport struct {
	SchemaVersion             int       `json:"schema_version"`
	Site                      string    `json:"site"`
	Component                 string    `json:"component"`
	Mechanism                 string    `json:"mechanism"`
	Point                     string    `json:"point"`
	Stanza                    string    `json:"stanza"`
	Status                    string    `json:"status"`
	Error                     string    `json:"error,omitempty"`
	StartedAt                 time.Time `json:"started_at"`
	FinishedAt                time.Time `json:"finished_at"`
	FailureInjectedAt         time.Time `json:"failure_injected_at"`
	RestoredReadyAt           time.Time `json:"restored_ready_at"`
	BackupDurationMS          uint32    `json:"backup_duration_ms"`
	RestoreDurationMS         uint32    `json:"restore_duration_ms"`
	RecoveryStartupDurationMS uint32    `json:"recovery_startup_duration_ms"`
	RTOMS                     uint32    `json:"rto_ms"`
	RestoreTarget             string    `json:"restore_target"`
	WriterDurationMS          uint32    `json:"writer_duration_ms"`
	WriterIntervalMS          uint32    `json:"writer_interval_ms"`
	ArchiveTimeoutMS          uint32    `json:"archive_timeout_ms"`
	ArchiveBoundary           string    `json:"archive_boundary"`
	ArchiveBoundaryCount      uint64    `json:"archive_boundary_count"`
	ArchiveBoundaryWaitMS     uint32    `json:"archive_boundary_wait_ms"`
	InsertedRows              uint64    `json:"inserted_rows"`
	LastAckSeq                uint64    `json:"last_ack_seq"`
	LastAckLSN                string    `json:"last_ack_lsn"`
	RestoredMaxSeq            uint64    `json:"restored_max_seq"`
	LostRows                  uint64    `json:"lost_rows"`
	LastAckAt                 time.Time `json:"last_ack_at"`
	RestoredMaxAckAt          time.Time `json:"restored_max_ack_at"`
	DataLossMS                uint32    `json:"data_loss_ms"`
	BackupName                string    `json:"backup_name"`
	BackupRepoBytes           uint64    `json:"backup_repo_bytes"`
	SourceDataBytes           uint64    `json:"source_data_bytes"`
	RestoredDataBytes         uint64    `json:"restored_data_bytes"`
	ArchivedWALCount          uint64    `json:"archived_wal_count"`
	ArchiverArchivedCount     uint64    `json:"archiver_archived_count"`
	ArchiverFailedCount       uint64    `json:"archiver_failed_count"`
	IO                        ioSummary `json:"io"`
	RunRoot                   string    `json:"run_root,omitempty"`
	ReportPath                string    `json:"report_path"`
	PgBackRestInfoJSON        string    `json:"pgbackrest_info_json,omitempty"`
}

type markerRow struct {
	seq uint64
	at  time.Time
	lsn string
}

type markerSnapshot struct {
	maxSeq uint64
	maxAt  time.Time
	count  uint64
}

type archiverStats struct {
	archivedCount uint64
	failedCount   uint64
}

type recoveryEvent struct {
	Site                    string    `ch:"site"`
	Component               string    `ch:"component"`
	Mechanism               string    `ch:"mechanism"`
	Action                  string    `ch:"action"`
	Status                  string    `ch:"status"`
	Point                   string    `ch:"point"`
	BackupName              string    `ch:"backup_name"`
	BackupOperationID       string    `ch:"backup_operation_id"`
	RestoreOperationID      string    `ch:"restore_operation_id"`
	SourceDatabase          string    `ch:"source_database"`
	TargetDatabase          string    `ch:"target_database"`
	ExpectedTableCount      uint32    `ch:"expected_table_count"`
	RestoredTableCount      uint32    `ch:"restored_table_count"`
	ExpectedRows            uint64    `ch:"expected_rows"`
	RestoredRows            uint64    `ch:"restored_rows"`
	BackupNumFiles          uint64    `ch:"backup_num_files"`
	BackupTotalBytes        uint64    `ch:"backup_total_bytes"`
	BackupUncompressedBytes uint64    `ch:"backup_uncompressed_bytes"`
	BackupCompressedBytes   uint64    `ch:"backup_compressed_bytes"`
	BackupDurationMS        uint32    `ch:"backup_duration_ms"`
	RestoreDurationMS       uint32    `ch:"restore_duration_ms"`
	VerifyDurationMS        uint32    `ch:"verify_duration_ms"`
	ErrorKind               string    `ch:"error_kind"`
	ErrorMessage            string    `ch:"error_message"`
	DetailsJSON             string    `ch:"details_json"`
	EventAt                 time.Time `ch:"event_at"`
}

type operatorConfig struct {
	Host            string
	Port            int
	CertificateFile string
	PrivateKeyFile  string
	CAConfig        string
}

type clickHouseClientConfigXML struct {
	XMLName xml.Name
	Secure  string `xml:"secure"`
	Host    string `xml:"host"`
	Port    int    `xml:"port"`
	OpenSSL struct {
		Client struct {
			CertificateFile string `xml:"certificateFile"`
			PrivateKeyFile  string `xml:"privateKeyFile"`
			CAConfig        string `xml:"caConfig"`
		} `xml:"client"`
	} `xml:"openSSL"`
}

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := run(ctx, os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "postgresql-pitr-drill: "+err.Error())
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	opts, err := parseOptions(args)
	if err != nil {
		return err
	}
	tools := pgToolsFromRuntime(opts.runtimeRoot)
	if err := requireExecutable(tools.pgBackRest); err != nil {
		return err
	}
	if err := requireExecutable(filepath.Join(tools.binDir, "postgres")); err != nil {
		return err
	}
	pgCred, err := lookupCredential(restoreUser)
	if err != nil {
		return err
	}
	var conn chdriver.Conn
	if opts.recordClickHouse {
		conn, err = openClickHouse(ctx, opts.clickHouseConfigPath)
		if err != nil {
			return err
		}
		defer func() { _ = conn.Close() }()
		if err := ensureEvidenceSchema(ctx, conn); err != nil {
			return err
		}
	}
	r := runner{
		opts:      opts,
		tools:     tools,
		postgres:  pgCred,
		clickConn: conn,
	}
	reports := make([]drillReport, 0, opts.drills)
	for i := 0; i < opts.drills; i++ {
		report, err := r.runDrill(ctx, i)
		reports = append(reports, report)
		if err != nil {
			return err
		}
	}
	summaryPath, err := writeSummary(opts, reports)
	if err != nil {
		return err
	}
	printSummary(summaryPath, reports)
	return nil
}

func parseOptions(args []string) (options, error) {
	opts := options{
		archiveTimeout:       defaultArchiveTimeout,
		drills:               10,
		sourcePortBase:       56432,
		writerInterval:       defaultWriterInterval,
		writerSeconds:        defaultWriterSeconds,
		recordClickHouse:     true,
		clickHouseConfigPath: defaultClickHouseXML,
	}
	fs := flag.NewFlagSet("postgresql-pitr-drill", flag.ContinueOnError)
	fs.StringVar(&opts.site, "site", "", "Verself site name.")
	fs.IntVar(&opts.drills, "drills", opts.drills, "Number of destructive scratch PITR drills to run.")
	fs.StringVar(&opts.runtimeRoot, "runtime-root", defaultRuntimeRoot, "PostgreSQL recovery runtime root.")
	fs.StringVar(&opts.drillRoot, "drill-root", defaultDrillRoot, "Scratch drill root.")
	fs.StringVar(&opts.cacheRoot, "cache-root", defaultCacheRoot, "Recovery report cache root.")
	fs.IntVar(&opts.writerSeconds, "writer-seconds", opts.writerSeconds, "Post-backup workload duration per drill.")
	fs.DurationVar(&opts.writerInterval, "writer-interval", opts.writerInterval, "Interval between committed marker writes.")
	fs.DurationVar(&opts.archiveTimeout, "archive-timeout", opts.archiveTimeout, "PostgreSQL archive_timeout for the scratch source.")
	fs.StringVar(&opts.archiveBoundary, "archive-boundary", archiveBoundaryNone, "Archive boundary policy: none or after-write.")
	fs.IntVar(&opts.sourcePortBase, "source-port-base", opts.sourcePortBase, "First TCP port assigned to scratch clusters.")
	fs.BoolVar(&opts.keepScratch, "keep-scratch", false, "Keep scratch cluster data after each drill.")
	fs.BoolVar(&opts.recordClickHouse, "record-clickhouse", opts.recordClickHouse, "Insert one verself.recovery_events row per drill.")
	fs.StringVar(&opts.clickHouseConfigPath, "clickhouse-operator-config", opts.clickHouseConfigPath, "ClickHouse operator client XML.")
	if err := fs.Parse(args); err != nil {
		return options{}, err
	}
	opts.site = strings.TrimSpace(opts.site)
	if !safeSegment(opts.site) {
		return options{}, errors.New("--site must be a non-empty safe path segment")
	}
	if opts.drills < 1 || opts.drills > 20 {
		return options{}, errors.New("--drills must be between 1 and 20")
	}
	if opts.writerSeconds < 1 || opts.writerSeconds > 120 {
		return options{}, errors.New("--writer-seconds must be between 1 and 120")
	}
	if opts.writerInterval < 10*time.Millisecond || opts.writerInterval > 10*time.Second {
		return options{}, errors.New("--writer-interval must be between 10ms and 10s")
	}
	if opts.archiveTimeout < time.Second || opts.archiveTimeout > time.Minute {
		return options{}, errors.New("--archive-timeout must be between 1s and 1m")
	}
	opts.archiveBoundary = strings.TrimSpace(opts.archiveBoundary)
	switch opts.archiveBoundary {
	case archiveBoundaryNone, archiveBoundaryAfterWrite:
	default:
		return options{}, errors.New("--archive-boundary must be none or after-write")
	}
	if opts.sourcePortBase < 1024 || opts.sourcePortBase > 60000 {
		return options{}, errors.New("--source-port-base must be between 1024 and 60000")
	}
	var err error
	if opts.runtimeRoot, err = filepath.Abs(opts.runtimeRoot); err != nil {
		return options{}, fmt.Errorf("resolve --runtime-root: %w", err)
	}
	if opts.drillRoot, err = filepath.Abs(opts.drillRoot); err != nil {
		return options{}, fmt.Errorf("resolve --drill-root: %w", err)
	}
	if opts.cacheRoot, err = filepath.Abs(opts.cacheRoot); err != nil {
		return options{}, fmt.Errorf("resolve --cache-root: %w", err)
	}
	return opts, nil
}

func pgToolsFromRuntime(root string) pgTools {
	libDir := filepath.Join(root, "usr", "lib", "x86_64-linux-gnu")
	pgLibDir := filepath.Join(root, "usr", "lib", "postgresql", postgresMajor, "lib")
	return pgTools{
		binDir:        filepath.Join(root, "usr", "lib", "postgresql", postgresMajor, "bin"),
		shareDir:      filepath.Join(root, "usr", "share", "postgresql", postgresMajor),
		pgBackRest:    filepath.Join(root, "usr", "bin", "pgbackrest"),
		ldLibraryPath: strings.Join([]string{libDir, pgLibDir}, ":"),
	}
}

func (r runner) runDrill(ctx context.Context, index int) (drillReport, error) {
	started := time.Now().UTC()
	point, err := newPoint(started)
	if err != nil {
		return drillReport{}, err
	}
	stanza := "verself_" + point
	sourcePort := r.opts.sourcePortBase + index*2
	restorePort := sourcePort + 1
	paths := drillPaths{
		runRoot:       filepath.Join(r.opts.drillRoot, r.opts.site, point),
		reportDir:     reportDir(r.opts.cacheRoot, r.opts.site, point),
		sourceData:    filepath.Join(r.opts.drillRoot, r.opts.site, point, "source-pgdata"),
		restoreData:   filepath.Join(r.opts.drillRoot, r.opts.site, point, "restore-pgdata"),
		sourceSocket:  filepath.Join(r.opts.drillRoot, r.opts.site, point, "source-socket"),
		restoreSocket: filepath.Join(r.opts.drillRoot, r.opts.site, point, "restore-socket"),
		repo:          filepath.Join(r.opts.drillRoot, r.opts.site, point, "repo"),
		spool:         filepath.Join(r.opts.drillRoot, r.opts.site, point, "spool"),
		log:           filepath.Join(r.opts.drillRoot, r.opts.site, point, "log"),
	}
	paths.config = filepath.Join(paths.runRoot, "pgbackrest.conf")
	report := drillReport{
		SchemaVersion:    recoverySchemaVersion,
		Site:             r.opts.site,
		Component:        component,
		Mechanism:        mechanism,
		Point:            point,
		Stanza:           stanza,
		Status:           "running",
		StartedAt:        started,
		WriterIntervalMS: durationMS(r.opts.writerInterval),
		ArchiveTimeoutMS: durationMS(r.opts.archiveTimeout),
		ArchiveBoundary:  r.opts.archiveBoundary,
		RunRoot:          paths.runRoot,
		ReportPath:       filepath.Join(paths.reportDir, "report.json"),
	}
	if err := r.preparePaths(paths); err != nil {
		return r.failDrill(ctx, report, paths, err)
	}
	sampler := startIOSampler(ctx, 250*time.Millisecond)
	drillSucceeded := false
	defer func() {
		if !r.opts.keepScratch && drillSucceeded {
			_ = os.RemoveAll(paths.runRoot)
		}
	}()
	defer func() {
		report.IO = sampler.Stop()
	}()

	source, err := r.startSourceCluster(ctx, paths, stanza, sourcePort)
	if err != nil {
		return r.failDrill(ctx, report, paths, err)
	}
	defer source.cleanup()

	if err := r.psqlExec(ctx, paths.sourceSocket, sourcePort, "postgres", `CREATE DATABASE "`+databaseName+`";`); err != nil {
		return r.failDrill(ctx, report, paths, err)
	}
	if err := r.psqlExec(ctx, paths.sourceSocket, sourcePort, databaseName, `CREATE TABLE dr_marker (seq bigserial PRIMARY KEY, acked_at timestamptz NOT NULL DEFAULT clock_timestamp(), payload text NOT NULL);`); err != nil {
		return r.failDrill(ctx, report, paths, err)
	}
	if _, err := r.insertMarker(ctx, paths.sourceSocket, sourcePort, "baseline"); err != nil {
		return r.failDrill(ctx, report, paths, err)
	}
	if _, err := r.pgBackRest(ctx, "--config="+paths.config, "--stanza="+stanza, "stanza-create"); err != nil {
		return r.failDrill(ctx, report, paths, err)
	}
	if _, err := r.pgBackRest(ctx, "--config="+paths.config, "--stanza="+stanza, "check"); err != nil {
		return r.failDrill(ctx, report, paths, err)
	}
	backupStarted := time.Now()
	if _, err := r.pgBackRest(ctx, "--config="+paths.config, "--stanza="+stanza, "--type=full", "--start-fast", "--process-max=2", "backup"); err != nil {
		return r.failDrill(ctx, report, paths, err)
	}
	report.BackupDurationMS = durationMS(time.Since(backupStarted))
	infoJSON, backupName, err := r.pgBackRestInfo(ctx, paths.config, stanza)
	if err != nil {
		return r.failDrill(ctx, report, paths, err)
	}
	report.PgBackRestInfoJSON = infoJSON
	report.BackupName = backupName

	writerStarted := time.Now()
	lastAck := markerRow{}
	inserted := uint64(0)
	deadline := writerStarted.Add(time.Duration(r.opts.writerSeconds) * time.Second)
	for time.Now().Before(deadline) {
		row, err := r.insertMarker(ctx, paths.sourceSocket, sourcePort, fmt.Sprintf("drill=%s seq=%d", point, inserted+1))
		if err != nil {
			return r.failDrill(ctx, report, paths, err)
		}
		lastAck = row
		inserted++
		if r.opts.archiveBoundary == archiveBoundaryAfterWrite {
			waited, err := r.forceArchiveBoundary(ctx, paths.sourceSocket, sourcePort)
			if err != nil {
				return r.failDrill(ctx, report, paths, err)
			}
			report.ArchiveBoundaryCount++
			report.ArchiveBoundaryWaitMS += durationMS(waited)
		}
		time.Sleep(r.opts.writerInterval)
	}
	report.WriterDurationMS = durationMS(time.Since(writerStarted))
	report.InsertedRows = inserted
	report.LastAckSeq = lastAck.seq
	report.LastAckLSN = lastAck.lsn
	report.LastAckAt = lastAck.at
	stats, err := r.archiverStats(ctx, paths.sourceSocket, sourcePort)
	if err != nil {
		return r.failDrill(ctx, report, paths, err)
	}
	report.ArchiverArchivedCount = stats.archivedCount
	report.ArchiverFailedCount = stats.failedCount
	report.FailureInjectedAt = time.Now().UTC()
	report.SourceDataBytes = dirSize(paths.sourceData)
	if err := source.stopImmediate(ctx); err != nil {
		return r.failDrill(ctx, report, paths, err)
	}
	source.stopped = true
	if err := os.RemoveAll(paths.sourceData); err != nil {
		return r.failDrill(ctx, report, paths, fmt.Errorf("delete source PGDATA after failure injection: %w", err))
	}
	if err := os.MkdirAll(paths.restoreData, 0o700); err != nil {
		return r.failDrill(ctx, report, paths, fmt.Errorf("create restore PGDATA: %w", err))
	}
	if err := os.Chown(paths.restoreData, int(r.postgres.uid), int(r.postgres.gid)); err != nil {
		return r.failDrill(ctx, report, paths, fmt.Errorf("chown restore PGDATA: %w", err))
	}

	restoreStarted := time.Now()
	restoreArgs := []string{"--config=" + paths.config, "--stanza=" + stanza, "--pg1-path=" + paths.restoreData, "--process-max=2"}
	if r.opts.archiveBoundary == archiveBoundaryAfterWrite {
		report.RestoreTarget = report.LastAckLSN
		restoreArgs = append(restoreArgs, "--type=lsn", "--target="+report.RestoreTarget, "--target-action=promote")
	}
	restoreArgs = append(restoreArgs, "restore")
	if _, err := r.pgBackRest(ctx, restoreArgs...); err != nil {
		return r.failDrill(ctx, report, paths, err)
	}
	report.RestoreDurationMS = durationMS(time.Since(restoreStarted))
	restored, err := r.startPostgres(ctx, paths.restoreData, paths.restoreSocket, restorePort, filepath.Join(paths.log, "restore-postgres.log"), "archive_mode=off")
	if err != nil {
		return r.failDrill(ctx, report, paths, err)
	}
	defer restored.cleanup()
	startupStarted := time.Now()
	if err := r.waitForPostgres(ctx, paths.restoreSocket, restorePort); err != nil {
		return r.failDrill(ctx, report, paths, err)
	}
	if err := r.waitForRecoveryComplete(ctx, paths.restoreSocket, restorePort); err != nil {
		return r.failDrill(ctx, report, paths, err)
	}
	report.RecoveryStartupDurationMS = durationMS(time.Since(startupStarted))
	report.RestoredReadyAt = time.Now().UTC()
	report.RTOMS = durationMS(report.RestoredReadyAt.Sub(report.FailureInjectedAt))
	snapshot, err := r.markerSnapshot(ctx, paths.restoreSocket, restorePort)
	if err != nil {
		return r.failDrill(ctx, report, paths, err)
	}
	report.RestoredMaxSeq = snapshot.maxSeq
	report.RestoredMaxAckAt = snapshot.maxAt
	report.LostRows = saturatedSub(report.LastAckSeq, report.RestoredMaxSeq)
	report.DataLossMS = dataLossMS(report.LastAckAt, report.RestoredMaxAckAt, report.LostRows)
	if err := restored.stopFast(ctx); err != nil {
		return r.failDrill(ctx, report, paths, err)
	}
	restored.stopped = true
	report.BackupRepoBytes = dirSize(paths.repo)
	report.RestoredDataBytes = dirSize(paths.restoreData)
	report.ArchivedWALCount = countArchivedWAL(paths.repo)
	report.IO = sampler.Stop()
	report.Status = "succeeded"
	report.FinishedAt = time.Now().UTC()
	if err := writeJSONFile(report.ReportPath, report, 0o644); err != nil {
		return report, err
	}
	if err := r.recordReport(ctx, report); err != nil {
		return report, err
	}
	drillSucceeded = true
	fmt.Printf("postgresql-pitr-drill: point=%s status=succeeded last_ack_seq=%d restored_max_seq=%d lost_rows=%d rto_ms=%d data_loss_ms=%d\n",
		report.Point, report.LastAckSeq, report.RestoredMaxSeq, report.LostRows, report.RTOMS, report.DataLossMS)
	return report, nil
}

type drillPaths struct {
	runRoot       string
	reportDir     string
	sourceData    string
	restoreData   string
	sourceSocket  string
	restoreSocket string
	repo          string
	spool         string
	log           string
	config        string
}

func (r runner) preparePaths(paths drillPaths) error {
	for _, dir := range []string{r.opts.drillRoot, filepath.Join(r.opts.drillRoot, r.opts.site)} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create drill parent %s: %w", dir, err)
		}
		if err := os.Chmod(dir, 0o755); err != nil {
			return fmt.Errorf("chmod drill parent %s: %w", dir, err)
		}
	}
	for _, dir := range []string{paths.runRoot, paths.reportDir, paths.sourceSocket, paths.restoreSocket, paths.repo, paths.spool, paths.log} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return fmt.Errorf("create %s: %w", dir, err)
		}
	}
	for _, dir := range []string{paths.runRoot, paths.sourceSocket, paths.restoreSocket, paths.repo, paths.spool, paths.log} {
		if err := chownRecursive(dir, int(r.postgres.uid), int(r.postgres.gid)); err != nil {
			return err
		}
	}
	if err := os.Chmod(paths.reportDir, 0o755); err != nil {
		return fmt.Errorf("chmod report directory: %w", err)
	}
	return nil
}

func (r runner) startSourceCluster(ctx context.Context, paths drillPaths, stanza string, port int) (*postgresProcess, error) {
	if err := r.initdb(ctx, paths.sourceData); err != nil {
		return nil, err
	}
	conf := strings.Join([]string{
		"",
		"# PITR drill configuration.",
		"listen_addresses = ''",
		fmt.Sprintf("port = %d", port),
		fmt.Sprintf("unix_socket_directories = '%s'", paths.sourceSocket),
		"logging_collector = on",
		fmt.Sprintf("log_directory = '%s'", paths.log),
		"log_filename = 'source-postgresql.log'",
		"log_min_messages = warning",
		"wal_level = replica",
		"archive_mode = on",
		fmt.Sprintf("archive_timeout = '%s'", r.opts.archiveTimeout.String()),
		"max_wal_size = '256MB'",
		"track_commit_timestamp = on",
		fmt.Sprintf("archive_command = '%s --config=%s --stanza=%s archive-push %%p'", r.tools.pgBackRest, paths.config, stanza),
	}, "\n")
	if err := appendFileAsPostgres(filepath.Join(paths.sourceData, "postgresql.conf"), []byte(conf+"\n"), r.postgres); err != nil {
		return nil, err
	}
	pgBackRestConf := strings.Join([]string{
		"[global]",
		fmt.Sprintf("repo1-path=%s", paths.repo),
		"repo1-retention-full=2",
		"process-max=2",
		"start-fast=y",
		"compress-type=lz4",
		"log-level-console=warn",
		"log-level-file=detail",
		fmt.Sprintf("log-path=%s", paths.log),
		fmt.Sprintf("spool-path=%s", paths.spool),
		"",
		"[" + stanza + "]",
		fmt.Sprintf("pg1-path=%s", paths.sourceData),
		fmt.Sprintf("pg1-port=%d", port),
		fmt.Sprintf("pg1-socket-path=%s", paths.sourceSocket),
		"pg1-user=postgres",
		"",
	}, "\n")
	if err := writeFileAsPostgres(paths.config, []byte(pgBackRestConf), 0o600, r.postgres); err != nil {
		return nil, err
	}
	proc, err := r.startPostgres(ctx, paths.sourceData, paths.sourceSocket, port, filepath.Join(paths.log, "source-postgres.log"))
	if err != nil {
		return nil, err
	}
	if err := r.waitForPostgres(ctx, paths.sourceSocket, port); err != nil {
		proc.cleanup()
		return nil, err
	}
	return proc, nil
}

type postgresProcess struct {
	cmd     *exec.Cmd
	logFile *os.File
	tools   pgTools
	cred    credential
	pgdata  string
	stopped bool
}

func (p *postgresProcess) cleanup() {
	if p == nil || p.cmd == nil || p.stopped {
		return
	}
	_ = p.cmd.Process.Signal(os.Interrupt)
	_, _ = p.cmd.Process.Wait()
	if p.logFile != nil {
		_ = p.logFile.Close()
	}
	p.stopped = true
}

func (p *postgresProcess) stopImmediate(ctx context.Context) error {
	return p.pgCtl(ctx, "immediate")
}

func (p *postgresProcess) stopFast(ctx context.Context) error {
	return p.pgCtl(ctx, "fast")
}

func (p *postgresProcess) pgCtl(ctx context.Context, mode string) error {
	cmd := exec.CommandContext(ctx, filepath.Join(p.tools.binDir, "pg_ctl"), "-D", p.pgdata, "-m", mode, "stop")
	cmd.Env = commandEnv(p.tools)
	cmd.SysProcAttr = credentialSysProc(p.cred)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("pg_ctl stop %s: %w: %s", mode, err, strings.TrimSpace(string(output)))
	}
	if p.cmd != nil {
		if err := p.cmd.Wait(); err != nil && mode != "immediate" {
			return fmt.Errorf("postgres process wait after stop: %w", err)
		}
	}
	if p.logFile != nil {
		_ = p.logFile.Close()
	}
	return nil
}

func (r runner) initdb(ctx context.Context, pgdata string) error {
	args := []string{
		"--pgdata=" + pgdata,
		"--auth-local=trust",
		"--auth-host=reject",
		"--encoding=UTF8",
		"--locale=C.UTF-8",
		"--username=postgres",
		"-L", r.tools.shareDir,
	}
	if _, err := r.commandAsPostgres(ctx, filepath.Join(r.tools.binDir, "initdb"), args...); err != nil {
		return err
	}
	return nil
}

func (r runner) startPostgres(ctx context.Context, pgdata, socketDir string, port int, logPath string, extraSettings ...string) (*postgresProcess, error) {
	if err := os.MkdirAll(socketDir, 0o700); err != nil {
		return nil, fmt.Errorf("create postgres socket directory: %w", err)
	}
	if err := os.Chown(socketDir, int(r.postgres.uid), int(r.postgres.gid)); err != nil {
		return nil, fmt.Errorf("chown postgres socket directory: %w", err)
	}
	args := []string{
		"-D", pgdata,
		"-c", "listen_addresses=",
		"-c", "unix_socket_directories=" + socketDir,
		"-c", fmt.Sprintf("port=%d", port),
	}
	for _, setting := range extraSettings {
		args = append(args, "-c", setting)
	}
	cmd := exec.CommandContext(ctx, filepath.Join(r.tools.binDir, "postgres"), args...)
	cmd.Env = commandEnv(r.tools)
	cmd.SysProcAttr = credentialSysProc(r.postgres)
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, fmt.Errorf("create postgres log %s: %w", logPath, err)
	}
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	if err := cmd.Start(); err != nil {
		_ = logFile.Close()
		return nil, fmt.Errorf("start postgres: %w", err)
	}
	return &postgresProcess{cmd: cmd, logFile: logFile, tools: r.tools, cred: r.postgres, pgdata: pgdata}, nil
}

func (r runner) waitForPostgres(ctx context.Context, socketDir string, port int) error {
	deadline := time.Now().Add(45 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		if _, err := r.psqlOutput(ctx, socketDir, port, "postgres", "select 1;"); err == nil {
			return nil
		} else {
			lastErr = err
		}
		time.Sleep(250 * time.Millisecond)
	}
	return fmt.Errorf("postgres did not become ready: %w", lastErr)
}

func (r runner) waitForRecoveryComplete(ctx context.Context, socketDir string, port int) error {
	deadline := time.Now().Add(2 * time.Minute)
	var lastErr error
	for time.Now().Before(deadline) {
		output, err := r.psqlOutput(ctx, socketDir, port, "postgres", "SELECT NOT pg_is_in_recovery();")
		if err == nil && strings.TrimSpace(output) == "t" {
			return nil
		}
		if err != nil {
			lastErr = err
		}
		time.Sleep(250 * time.Millisecond)
	}
	if lastErr != nil {
		return fmt.Errorf("postgres recovery did not finish: %w", lastErr)
	}
	return errors.New("postgres recovery did not finish within 2m")
}

func (r runner) psqlExec(ctx context.Context, socketDir string, port int, database string, query string) error {
	_, err := r.psqlOutput(ctx, socketDir, port, database, query)
	return err
}

func (r runner) psqlOutput(ctx context.Context, socketDir string, port int, database string, query string) (string, error) {
	args := []string{
		"-h", socketDir,
		"-p", strconv.Itoa(port),
		"-U", restoreUser,
		"-d", database,
		"-X",
		"-A",
		"-t",
		"-q",
		"-v", "ON_ERROR_STOP=1",
		"-c", query,
	}
	output, err := r.commandAsPostgres(ctx, filepath.Join(r.tools.binDir, "psql"), args...)
	if err != nil {
		return "", err
	}
	return output, nil
}

func (r runner) insertMarker(ctx context.Context, socketDir string, port int, payload string) (markerRow, error) {
	escaped := strings.ReplaceAll(payload, "'", "''")
	query := "INSERT INTO dr_marker(payload) VALUES ('" + escaped + "') RETURNING seq, extract(epoch from acked_at);"
	output, err := r.psqlOutput(ctx, socketDir, port, databaseName, query)
	if err != nil {
		return markerRow{}, err
	}
	parts := strings.Split(strings.TrimSpace(output), "|")
	if len(parts) != 2 {
		return markerRow{}, fmt.Errorf("unexpected marker insert output %q", strings.TrimSpace(output))
	}
	seq, err := strconv.ParseUint(strings.TrimSpace(parts[0]), 10, 64)
	if err != nil {
		return markerRow{}, fmt.Errorf("parse inserted marker seq: %w", err)
	}
	at, err := parseEpoch(strings.TrimSpace(parts[1]))
	if err != nil {
		return markerRow{}, err
	}
	lsn, err := r.currentWALInsertLSN(ctx, socketDir, port)
	if err != nil {
		return markerRow{}, err
	}
	return markerRow{seq: seq, at: at, lsn: lsn}, nil
}

func (r runner) currentWALInsertLSN(ctx context.Context, socketDir string, port int) (string, error) {
	output, err := r.psqlOutput(ctx, socketDir, port, "postgres", "SELECT pg_current_wal_insert_lsn();")
	if err != nil {
		return "", err
	}
	lsn := strings.TrimSpace(output)
	if lsn == "" {
		return "", errors.New("PostgreSQL returned an empty WAL insert LSN")
	}
	return lsn, nil
}

func (r runner) markerSnapshot(ctx context.Context, socketDir string, port int) (markerSnapshot, error) {
	output, err := r.psqlOutput(ctx, socketDir, port, databaseName, "SELECT coalesce(max(seq), 0), coalesce(extract(epoch from max(acked_at)), 0), count(*) FROM dr_marker;")
	if err != nil {
		return markerSnapshot{}, err
	}
	parts := strings.Split(strings.TrimSpace(output), "|")
	if len(parts) != 3 {
		return markerSnapshot{}, fmt.Errorf("unexpected marker snapshot output %q", strings.TrimSpace(output))
	}
	maxSeq, err := strconv.ParseUint(strings.TrimSpace(parts[0]), 10, 64)
	if err != nil {
		return markerSnapshot{}, fmt.Errorf("parse restored max seq: %w", err)
	}
	maxAt, err := parseEpoch(strings.TrimSpace(parts[1]))
	if err != nil {
		return markerSnapshot{}, err
	}
	count, err := strconv.ParseUint(strings.TrimSpace(parts[2]), 10, 64)
	if err != nil {
		return markerSnapshot{}, fmt.Errorf("parse restored count: %w", err)
	}
	return markerSnapshot{maxSeq: maxSeq, maxAt: maxAt, count: count}, nil
}

func (r runner) archiverStats(ctx context.Context, socketDir string, port int) (archiverStats, error) {
	output, err := r.psqlOutput(ctx, socketDir, port, "postgres", "SELECT archived_count, failed_count FROM pg_stat_archiver;")
	if err != nil {
		return archiverStats{}, err
	}
	parts := strings.Split(strings.TrimSpace(output), "|")
	if len(parts) != 2 {
		return archiverStats{}, fmt.Errorf("unexpected pg_stat_archiver output %q", strings.TrimSpace(output))
	}
	archived, err := strconv.ParseUint(strings.TrimSpace(parts[0]), 10, 64)
	if err != nil {
		return archiverStats{}, fmt.Errorf("parse archived_count: %w", err)
	}
	failed, err := strconv.ParseUint(strings.TrimSpace(parts[1]), 10, 64)
	if err != nil {
		return archiverStats{}, fmt.Errorf("parse failed_count: %w", err)
	}
	return archiverStats{archivedCount: archived, failedCount: failed}, nil
}

func (r runner) forceArchiveBoundary(ctx context.Context, socketDir string, port int) (time.Duration, error) {
	before, err := r.archiverStats(ctx, socketDir, port)
	if err != nil {
		return 0, err
	}
	started := time.Now()
	if _, err := r.psqlOutput(ctx, socketDir, port, "postgres", "SELECT pg_switch_wal();"); err != nil {
		return 0, err
	}
	deadline := time.Now().Add(30 * time.Second)
	var last archiverStats
	for time.Now().Before(deadline) {
		stats, err := r.archiverStats(ctx, socketDir, port)
		if err != nil {
			return 0, err
		}
		last = stats
		if stats.failedCount > before.failedCount {
			return 0, fmt.Errorf("PostgreSQL archiver failed while forcing WAL boundary: failed_count %d -> %d", before.failedCount, stats.failedCount)
		}
		if stats.archivedCount > before.archivedCount {
			return time.Since(started), nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return 0, fmt.Errorf("PostgreSQL archiver did not archive forced WAL boundary within 30s: archived_count=%d failed_count=%d", last.archivedCount, last.failedCount)
}

func (r runner) pgBackRest(ctx context.Context, args ...string) (string, error) {
	return r.commandAsPostgres(ctx, r.tools.pgBackRest, args...)
}

func (r runner) pgBackRestInfo(ctx context.Context, configPath, stanza string) (string, string, error) {
	output, err := r.pgBackRest(ctx, "--config="+configPath, "--stanza="+stanza, "--output=json", "info")
	if err != nil {
		return "", "", err
	}
	backupName := ""
	var payload []struct {
		Backup []struct {
			Label string `json:"label"`
		} `json:"backup"`
	}
	if err := json.Unmarshal([]byte(output), &payload); err == nil && len(payload) > 0 && len(payload[0].Backup) > 0 {
		backupName = payload[0].Backup[len(payload[0].Backup)-1].Label
	}
	return strings.TrimSpace(output), backupName, nil
}

func (r runner) commandAsPostgres(ctx context.Context, command string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, command, args...)
	cmd.Env = commandEnv(r.tools)
	cmd.SysProcAttr = credentialSysProc(r.postgres)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%s failed: %w: %s", filepath.Base(command), err, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}

func (r runner) failDrill(ctx context.Context, report drillReport, paths drillPaths, drillErr error) (drillReport, error) {
	report.Status = "failed"
	report.Error = drillErr.Error()
	report.FinishedAt = time.Now().UTC()
	if report.ReportPath == "" {
		report.ReportPath = filepath.Join(paths.reportDir, "report.json")
	}
	_ = writeJSONFile(report.ReportPath, report, 0o644)
	_ = r.recordReport(ctx, report)
	return report, drillErr
}

func (r runner) recordReport(ctx context.Context, report drillReport) error {
	if r.clickConn == nil {
		return nil
	}
	details, err := json.Marshal(report)
	if err != nil {
		return fmt.Errorf("marshal drill report details: %w", err)
	}
	event := recoveryEvent{
		Site:               report.Site,
		Component:          component,
		Mechanism:          mechanism,
		Action:             "pitr_drill",
		Status:             report.Status,
		Point:              report.Point,
		BackupName:         report.BackupName,
		RestoreOperationID: "postgresql-pitr-drill-" + report.Point,
		SourceDatabase:     databaseName,
		TargetDatabase:     databaseName,
		ExpectedTableCount: 1,
		RestoredTableCount: 1,
		ExpectedRows:       report.LastAckSeq,
		RestoredRows:       report.RestoredMaxSeq,
		BackupTotalBytes:   report.BackupRepoBytes,
		BackupDurationMS:   report.BackupDurationMS,
		RestoreDurationMS:  report.RestoreDurationMS,
		VerifyDurationMS:   report.RTOMS,
		ErrorMessage:       report.Error,
		DetailsJSON:        string(details),
		EventAt:            time.Now().UTC(),
	}
	if report.Status == "failed" {
		event.ErrorKind = "postgresql_pitr_drill"
	}
	return recordRecoveryEvent(ctx, r.clickConn, event)
}

func openClickHouse(ctx context.Context, configPath string) (chdriver.Conn, error) {
	cfg, err := readOperatorConfig(configPath)
	if err != nil {
		return nil, err
	}
	tlsConfig, err := tlsConfigFromFiles(cfg)
	if err != nil {
		return nil, err
	}
	conn, err := ch.Open(&ch.Options{
		Addr: []string{fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)},
		Auth: ch.Auth{
			Database: "verself",
			Username: "clickhouse_operator",
		},
		TLS: tlsConfig,
	})
	if err != nil {
		return nil, fmt.Errorf("open ClickHouse: %w", err)
	}
	if err := conn.Ping(ctx); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("ping ClickHouse: %w", err)
	}
	return conn, nil
}

func readOperatorConfig(path string) (operatorConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return operatorConfig{}, fmt.Errorf("read ClickHouse operator config: %w", err)
	}
	var xmlCfg clickHouseClientConfigXML
	if err := xml.Unmarshal(data, &xmlCfg); err != nil {
		return operatorConfig{}, fmt.Errorf("parse ClickHouse operator config: %w", err)
	}
	secure, err := strconv.ParseBool(strings.TrimSpace(xmlCfg.Secure))
	if err != nil {
		return operatorConfig{}, fmt.Errorf("ClickHouse operator secure flag must parse as bool: %w", err)
	}
	if !secure {
		return operatorConfig{}, errors.New("ClickHouse operator config must use TLS")
	}
	cfg := operatorConfig{
		Host:            strings.TrimSpace(xmlCfg.Host),
		Port:            xmlCfg.Port,
		CertificateFile: strings.TrimSpace(xmlCfg.OpenSSL.Client.CertificateFile),
		PrivateKeyFile:  strings.TrimSpace(xmlCfg.OpenSSL.Client.PrivateKeyFile),
		CAConfig:        strings.TrimSpace(xmlCfg.OpenSSL.Client.CAConfig),
	}
	if cfg.Host == "" || cfg.Port <= 0 || cfg.CertificateFile == "" || cfg.PrivateKeyFile == "" || cfg.CAConfig == "" {
		return operatorConfig{}, errors.New("ClickHouse operator config is incomplete")
	}
	return cfg, nil
}

func tlsConfigFromFiles(cfg operatorConfig) (*tls.Config, error) {
	certPEM, err := os.ReadFile(cfg.CertificateFile)
	if err != nil {
		return nil, fmt.Errorf("read ClickHouse operator certificate: %w", err)
	}
	keyPEM, err := os.ReadFile(cfg.PrivateKeyFile)
	if err != nil {
		return nil, fmt.Errorf("read ClickHouse operator private key: %w", err)
	}
	caPEM, err := os.ReadFile(cfg.CAConfig)
	if err != nil {
		return nil, fmt.Errorf("read ClickHouse operator CA: %w", err)
	}
	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return nil, fmt.Errorf("load ClickHouse operator certificate pair: %w", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caPEM) {
		return nil, errors.New("load ClickHouse operator CA: no PEM certificates found")
	}
	return &tls.Config{Certificates: []tls.Certificate{cert}, RootCAs: pool, MinVersion: tls.VersionTLS12}, nil
}

func ensureEvidenceSchema(ctx context.Context, conn chdriver.Conn) error {
	if err := conn.Exec(ctx, "CREATE DATABASE IF NOT EXISTS verself"); err != nil {
		return fmt.Errorf("ensure recovery evidence database: %w", err)
	}
	ddl := `
CREATE TABLE IF NOT EXISTS verself.recovery_events
(
    site LowCardinality(String) CODEC(ZSTD(3)),
    component LowCardinality(String) CODEC(ZSTD(3)),
    mechanism LowCardinality(String) CODEC(ZSTD(3)),
    action LowCardinality(String) CODEC(ZSTD(3)),
    status LowCardinality(String) CODEC(ZSTD(3)),
    point String CODEC(ZSTD(3)),
    backup_name String CODEC(ZSTD(3)),
    backup_operation_id String CODEC(ZSTD(3)),
    restore_operation_id String CODEC(ZSTD(3)),
    source_database LowCardinality(String) CODEC(ZSTD(3)),
    target_database LowCardinality(String) CODEC(ZSTD(3)),
    expected_table_count UInt32 CODEC(T64, ZSTD(3)),
    restored_table_count UInt32 CODEC(T64, ZSTD(3)),
    expected_rows UInt64 CODEC(T64, ZSTD(3)),
    restored_rows UInt64 CODEC(T64, ZSTD(3)),
    backup_num_files UInt64 CODEC(T64, ZSTD(3)),
    backup_total_bytes UInt64 CODEC(T64, ZSTD(3)),
    backup_uncompressed_bytes UInt64 CODEC(T64, ZSTD(3)),
    backup_compressed_bytes UInt64 CODEC(T64, ZSTD(3)),
    backup_duration_ms UInt32 CODEC(T64, ZSTD(3)),
    restore_duration_ms UInt32 CODEC(T64, ZSTD(3)),
    verify_duration_ms UInt32 CODEC(T64, ZSTD(3)),
    error_kind String CODEC(ZSTD(3)),
    error_message String CODEC(ZSTD(3)),
    details_json String CODEC(ZSTD(3)),
    event_at DateTime64(9) CODEC(Delta(8), ZSTD(3))
)
ENGINE = MergeTree
PARTITION BY toDate(event_at)
ORDER BY (site, component, mechanism, action, status, toDateTime(event_at), point)
SETTINGS index_granularity = 8192`
	if err := conn.Exec(ctx, ddl); err != nil {
		return fmt.Errorf("ensure recovery evidence table: %w", err)
	}
	return nil
}

func recordRecoveryEvent(ctx context.Context, conn chdriver.Conn, event recoveryEvent) error {
	if event.EventAt.IsZero() {
		event.EventAt = time.Now().UTC()
	}
	batch, err := conn.PrepareBatch(ctx, "INSERT INTO "+evidenceTable)
	if err != nil {
		return fmt.Errorf("prepare recovery event insert: %w", err)
	}
	if err := batch.AppendStruct(&event); err != nil {
		_ = batch.Abort()
		return fmt.Errorf("append recovery event: %w", err)
	}
	if err := batch.Send(); err != nil {
		return fmt.Errorf("send recovery event: %w", err)
	}
	return nil
}

type diskCounters struct {
	readIOs      uint64
	readSectors  uint64
	writeIOs     uint64
	writeSectors uint64
}

type ioSample struct {
	at       time.Time
	counters diskCounters
}

type ioSampler struct {
	cancel context.CancelFunc
	done   chan struct{}
	mu     sync.Mutex
	data   []ioSample
}

type ioSummary struct {
	Samples             uint32  `json:"samples"`
	DurationMS          uint32  `json:"duration_ms"`
	ReadIOs             uint64  `json:"read_ios"`
	WriteIOs            uint64  `json:"write_ios"`
	ReadBytes           uint64  `json:"read_bytes"`
	WriteBytes          uint64  `json:"write_bytes"`
	ReadIOPSAvg         float64 `json:"read_iops_avg"`
	WriteIOPSAvg        float64 `json:"write_iops_avg"`
	ReadBytesPerSecAvg  float64 `json:"read_bytes_per_sec_avg"`
	WriteBytesPerSecAvg float64 `json:"write_bytes_per_sec_avg"`
	MaxReadIOPS         float64 `json:"max_read_iops"`
	MaxWriteIOPS        float64 `json:"max_write_iops"`
}

func startIOSampler(parent context.Context, interval time.Duration) *ioSampler {
	ctx, cancel := context.WithCancel(parent)
	s := &ioSampler{cancel: cancel, done: make(chan struct{})}
	go func() {
		defer close(s.done)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		s.addSample()
		for {
			select {
			case <-ctx.Done():
				s.addSample()
				return
			case <-ticker.C:
				s.addSample()
			}
		}
	}()
	return s
}

func (s *ioSampler) addSample() {
	counters, err := readDiskCounters()
	if err != nil {
		return
	}
	s.mu.Lock()
	s.data = append(s.data, ioSample{at: time.Now(), counters: counters})
	s.mu.Unlock()
}

func (s *ioSampler) Stop() ioSummary {
	if s == nil {
		return ioSummary{}
	}
	s.cancel()
	<-s.done
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.data) < 2 {
		return ioSummary{Samples: uint32(len(s.data))}
	}
	first := s.data[0]
	last := s.data[len(s.data)-1]
	duration := last.at.Sub(first.at).Seconds()
	if duration <= 0 {
		duration = 1
	}
	totalReadIOs := saturatedSub(last.counters.readIOs, first.counters.readIOs)
	totalWriteIOs := saturatedSub(last.counters.writeIOs, first.counters.writeIOs)
	totalReadBytes := saturatedSub(last.counters.readSectors, first.counters.readSectors) * 512
	totalWriteBytes := saturatedSub(last.counters.writeSectors, first.counters.writeSectors) * 512
	summary := ioSummary{
		Samples:             uint32(len(s.data)),
		DurationMS:          durationMS(last.at.Sub(first.at)),
		ReadIOs:             totalReadIOs,
		WriteIOs:            totalWriteIOs,
		ReadBytes:           totalReadBytes,
		WriteBytes:          totalWriteBytes,
		ReadIOPSAvg:         float64(totalReadIOs) / duration,
		WriteIOPSAvg:        float64(totalWriteIOs) / duration,
		ReadBytesPerSecAvg:  float64(totalReadBytes) / duration,
		WriteBytesPerSecAvg: float64(totalWriteBytes) / duration,
	}
	for i := 1; i < len(s.data); i++ {
		prev := s.data[i-1]
		cur := s.data[i]
		seconds := cur.at.Sub(prev.at).Seconds()
		if seconds <= 0 {
			continue
		}
		readIOPS := float64(saturatedSub(cur.counters.readIOs, prev.counters.readIOs)) / seconds
		writeIOPS := float64(saturatedSub(cur.counters.writeIOs, prev.counters.writeIOs)) / seconds
		if readIOPS > summary.MaxReadIOPS {
			summary.MaxReadIOPS = readIOPS
		}
		if writeIOPS > summary.MaxWriteIOPS {
			summary.MaxWriteIOPS = writeIOPS
		}
	}
	return summary
}

func readDiskCounters() (diskCounters, error) {
	file, err := os.Open("/proc/diskstats")
	if err != nil {
		return diskCounters{}, err
	}
	defer file.Close()
	var counters diskCounters
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 14 || !isPhysicalDisk(fields[2]) {
			continue
		}
		readIOs, _ := strconv.ParseUint(fields[3], 10, 64)
		readSectors, _ := strconv.ParseUint(fields[5], 10, 64)
		writeIOs, _ := strconv.ParseUint(fields[7], 10, 64)
		writeSectors, _ := strconv.ParseUint(fields[9], 10, 64)
		counters.readIOs += readIOs
		counters.readSectors += readSectors
		counters.writeIOs += writeIOs
		counters.writeSectors += writeSectors
	}
	if err := scanner.Err(); err != nil {
		return diskCounters{}, err
	}
	return counters, nil
}

func isPhysicalDisk(name string) bool {
	if matched, _ := regexp.MatchString(`^(sd[a-z]+|vd[a-z]+|xvd[a-z]+|nvme[0-9]+n[0-9]+)$`, name); matched {
		return true
	}
	return false
}

func lookupCredential(name string) (credential, error) {
	user, err := osuser.Lookup(name)
	if err != nil {
		return credential{}, fmt.Errorf("lookup user %s: %w", name, err)
	}
	uid, err := strconv.ParseUint(user.Uid, 10, 32)
	if err != nil {
		return credential{}, fmt.Errorf("parse uid for %s: %w", name, err)
	}
	gid, err := strconv.ParseUint(user.Gid, 10, 32)
	if err != nil {
		return credential{}, fmt.Errorf("parse gid for %s: %w", name, err)
	}
	return credential{uid: uint32(uid), gid: uint32(gid)}, nil
}

func credentialSysProc(cred credential) *syscall.SysProcAttr {
	return &syscall.SysProcAttr{
		Credential: &syscall.Credential{Uid: cred.uid, Gid: cred.gid},
	}
}

func commandEnv(tools pgTools) []string {
	env := os.Environ()
	env = append(env, "LD_LIBRARY_PATH="+tools.ldLibraryPath)
	env = append(env, "PGBACKREST_LOG_LEVEL_CONSOLE=warn")
	return env
}

func writeFileAsPostgres(path string, data []byte, mode fs.FileMode, cred credential) error {
	if err := os.WriteFile(path, data, mode); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err := os.Chown(path, int(cred.uid), int(cred.gid)); err != nil {
		return fmt.Errorf("chown %s: %w", path, err)
	}
	if err := os.Chmod(path, mode); err != nil {
		return fmt.Errorf("chmod %s: %w", path, err)
	}
	return nil
}

func appendFileAsPostgres(path string, data []byte, cred credential) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		return fmt.Errorf("open %s for append: %w", path, err)
	}
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return fmt.Errorf("append %s: %w", path, err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close %s: %w", path, err)
	}
	if err := os.Chown(path, int(cred.uid), int(cred.gid)); err != nil {
		return fmt.Errorf("chown %s: %w", path, err)
	}
	return nil
}

func chownRecursive(root string, uid, gid int) error {
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err := os.Chown(path, uid, gid); err != nil {
			return fmt.Errorf("chown %s: %w", path, err)
		}
		return nil
	})
}

func writeJSONFile(path string, value any, mode fs.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create parent for %s: %w", path, err)
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal %s: %w", path, err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return fmt.Errorf("create temp json %s: %w", path, err)
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }()
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temp json %s: %w", path, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp json %s: %w", path, err)
	}
	if err := os.Chmod(tmpPath, mode); err != nil {
		return fmt.Errorf("chmod temp json %s: %w", path, err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("commit json %s: %w", path, err)
	}
	return nil
}

func writeSummary(opts options, reports []drillReport) (string, error) {
	point := "summary-" + timestampForFile(time.Now().UTC())
	path := filepath.Join(opts.cacheRoot, "dr", "v1", "sites", opts.site, "components", component, "mechanisms", mechanism, point+".json")
	type summary struct {
		Site            string        `json:"site"`
		Component       string        `json:"component"`
		Mechanism       string        `json:"mechanism"`
		ReportCount     int           `json:"report_count"`
		Succeeded       int           `json:"succeeded"`
		Failed          int           `json:"failed"`
		MinRTOMS        uint32        `json:"min_rto_ms"`
		MaxRTOMS        uint32        `json:"max_rto_ms"`
		AvgRTOMS        float64       `json:"avg_rto_ms"`
		MaxLostRows     uint64        `json:"max_lost_rows"`
		MaxDataLossMS   uint32        `json:"max_data_loss_ms"`
		ReportPaths     []string      `json:"report_paths"`
		WrittenAt       time.Time     `json:"written_at"`
		ReportSummaries []drillReport `json:"report_summaries"`
	}
	out := summary{
		Site:            opts.site,
		Component:       component,
		Mechanism:       mechanism,
		ReportCount:     len(reports),
		MinRTOMS:        math.MaxUint32,
		WrittenAt:       time.Now().UTC(),
		ReportSummaries: reports,
	}
	var totalRTO uint64
	for _, report := range reports {
		out.ReportPaths = append(out.ReportPaths, report.ReportPath)
		if report.Status == "succeeded" {
			out.Succeeded++
		} else {
			out.Failed++
		}
		if report.RTOMS < out.MinRTOMS {
			out.MinRTOMS = report.RTOMS
		}
		if report.RTOMS > out.MaxRTOMS {
			out.MaxRTOMS = report.RTOMS
		}
		totalRTO += uint64(report.RTOMS)
		if report.LostRows > out.MaxLostRows {
			out.MaxLostRows = report.LostRows
		}
		if report.DataLossMS > out.MaxDataLossMS {
			out.MaxDataLossMS = report.DataLossMS
		}
	}
	if len(reports) == 0 {
		out.MinRTOMS = 0
	} else {
		out.AvgRTOMS = float64(totalRTO) / float64(len(reports))
	}
	if err := writeJSONFile(path, out, 0o644); err != nil {
		return "", err
	}
	return path, nil
}

func printSummary(path string, reports []drillReport) {
	type row struct {
		Point      string `json:"point"`
		Status     string `json:"status"`
		LastAckSeq uint64 `json:"last_ack_seq"`
		Restored   uint64 `json:"restored_max_seq"`
		LostRows   uint64 `json:"lost_rows"`
		RTOMS      uint32 `json:"rto_ms"`
		DataLossMS uint32 `json:"data_loss_ms"`
	}
	rows := make([]row, 0, len(reports))
	for _, report := range reports {
		rows = append(rows, row{
			Point:      report.Point,
			Status:     report.Status,
			LastAckSeq: report.LastAckSeq,
			Restored:   report.RestoredMaxSeq,
			LostRows:   report.LostRows,
			RTOMS:      report.RTOMS,
			DataLossMS: report.DataLossMS,
		})
	}
	payload := map[string]any{"summary_path": path, "reports": rows}
	data, _ := json.MarshalIndent(payload, "", "  ")
	fmt.Println(string(data))
}

func reportDir(root, site, point string) string {
	return filepath.Join(root, "dr", "v1", "sites", site, "components", component, "mechanisms", mechanism, "points", point)
}

func newPoint(now time.Time) (string, error) {
	var random [6]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", fmt.Errorf("generate point suffix: %w", err)
	}
	return now.Format(pointTimestampLayout) + "-" + hex.EncodeToString(random[:]), nil
}

func safeSegment(value string) bool {
	return safeSegmentPattern.MatchString(value)
}

func timestampForFile(t time.Time) string {
	return t.UTC().Format("20060102T150405Z")
}

func requireExecutable(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("stat executable %s: %w", path, err)
	}
	if info.IsDir() || info.Mode()&0o111 == 0 {
		return fmt.Errorf("%s is not executable", path)
	}
	return nil
}

func parseEpoch(value string) (time.Time, error) {
	seconds, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse epoch %q: %w", value, err)
	}
	whole, fraction := math.Modf(seconds)
	return time.Unix(int64(whole), int64(fraction*1e9)).UTC(), nil
}

func durationMS(d time.Duration) uint32 {
	if d <= 0 {
		return 0
	}
	ms := d.Milliseconds()
	if ms > math.MaxUint32 {
		return math.MaxUint32
	}
	return uint32(ms)
}

func dataLossMS(lastAck time.Time, restored time.Time, lostRows uint64) uint32 {
	if lostRows == 0 || lastAck.IsZero() || restored.IsZero() || !lastAck.After(restored) {
		return 0
	}
	return durationMS(lastAck.Sub(restored))
}

func saturatedSub(a, b uint64) uint64 {
	if a < b {
		return 0
	}
	return a - b
}

func dirSize(root string) uint64 {
	var size uint64
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err == nil && info.Size() > 0 {
			size += uint64(info.Size())
		}
		return nil
	})
	return size
}

func countArchivedWAL(repo string) uint64 {
	var count uint64
	_ = filepath.WalkDir(repo, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		name := d.Name()
		if isArchivedWALName(name) {
			count++
		}
		return nil
	})
	return count
}

func isArchivedWALName(name string) bool {
	if len(name) < 24 {
		return false
	}
	prefix := name[:24]
	for _, r := range prefix {
		if !((r >= '0' && r <= '9') || (r >= 'A' && r <= 'F')) {
			return false
		}
	}
	return true
}
