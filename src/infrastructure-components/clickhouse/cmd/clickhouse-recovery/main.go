package main

import (
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
	"math"
	"os"
	"os/signal"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	ch "github.com/ClickHouse/clickhouse-go/v2"
	chdriver "github.com/ClickHouse/clickhouse-go/v2/lib/driver"
)

const (
	component             = "clickhouse"
	defaultAction         = "exercise"
	defaultBackupDisk     = "verself_recovery_backups"
	defaultCacheRoot      = "/var/lib/verself/recovery-cache/clickhouse"
	defaultDatabase       = "verself"
	defaultMechanism      = "native_disk_full"
	defaultOperatorConfig = "/etc/clickhouse-client/operator.xml"
	evidenceTable         = "verself.recovery_events"
	pointTimestampLayout  = "20060102T150405Z"
	recoverySchemaVersion = 1
	verifyDatabasePrefix  = "dr_verify_"
	maxUint32AsInt64      = int64(math.MaxUint32)
)

var safeSegmentPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

type options struct {
	action             string
	site               string
	database           string
	point              string
	backupDisk         string
	cacheRoot          string
	operatorConfigPath string
	keepVerify         bool
}

type tableInfo struct {
	Name       string `json:"name"`
	Engine     string `json:"engine"`
	Rows       uint64 `json:"rows"`
	Bytes      uint64 `json:"bytes"`
	CreateHash string `json:"create_hash"`
}

type manifest struct {
	SchemaVersion          int         `json:"schema_version"`
	Site                   string      `json:"site"`
	Component              string      `json:"component"`
	Mechanism              string      `json:"mechanism"`
	Point                  string      `json:"point"`
	Database               string      `json:"database"`
	BackupDisk             string      `json:"backup_disk"`
	BackupPath             string      `json:"backup_path"`
	BackupName             string      `json:"backup_name"`
	BackupOperationID      string      `json:"backup_operation_id"`
	ClickHouseVersion      string      `json:"clickhouse_version"`
	StartedAt              time.Time   `json:"started_at"`
	FinishedAt             time.Time   `json:"finished_at"`
	DurationMS             uint32      `json:"duration_ms"`
	TableCount             uint32      `json:"table_count"`
	RowCount               uint64      `json:"row_count"`
	LogicalBytes           uint64      `json:"logical_bytes"`
	BackupNumFiles         uint64      `json:"backup_num_files"`
	BackupTotalBytes       uint64      `json:"backup_total_bytes"`
	BackupUncompressedSize uint64      `json:"backup_uncompressed_size"`
	BackupCompressedSize   uint64      `json:"backup_compressed_size"`
	Tables                 []tableInfo `json:"tables"`
}

type backupStatus struct {
	ID               string
	Name             string
	Status           string
	Error            string
	StartTime        time.Time
	EndTime          time.Time
	NumFiles         uint64
	TotalSize        uint64
	UncompressedSize uint64
	CompressedSize   uint64
	BytesRead        uint64
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
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "clickhouse-recovery: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	opts, err := parseOptions(args)
	if err != nil {
		return err
	}
	conn, err := openClickHouse(ctx, opts.operatorConfigPath, opts.database)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()

	if err := ensureEvidenceSchema(ctx, conn); err != nil {
		return err
	}

	switch opts.action {
	case "backup":
		manifest, err := backupDatabase(ctx, conn, opts)
		if err != nil {
			return err
		}
		printSummary("backup", manifest.Point, map[string]any{
			"backup_compressed_bytes": manifest.BackupCompressedSize,
			"backup_duration_ms":      manifest.DurationMS,
			"backup_name":             manifest.BackupName,
			"rows":                    manifest.RowCount,
			"tables":                  manifest.TableCount,
		})
		return nil
	case "verify":
		result, err := verifyDatabase(ctx, conn, opts)
		if err != nil {
			return err
		}
		printSummary("verify", result.Point, map[string]any{
			"restored_database":   result.TargetDatabase,
			"restored_rows":       result.RestoredRows,
			"restored_tables":     result.RestoredTableCount,
			"restore_duration_ms": result.RestoreDurationMS,
			"verify_duration_ms":  result.VerifyDurationMS,
		})
		return nil
	case "exercise":
		manifest, err := backupDatabase(ctx, conn, opts)
		if err != nil {
			return err
		}
		verifyOpts := opts
		verifyOpts.point = manifest.Point
		result, err := verifyDatabase(ctx, conn, verifyOpts)
		if err != nil {
			return err
		}
		printSummary("exercise", manifest.Point, map[string]any{
			"backup_compressed_bytes": manifest.BackupCompressedSize,
			"backup_duration_ms":      manifest.DurationMS,
			"restore_duration_ms":     result.RestoreDurationMS,
			"verify_duration_ms":      result.VerifyDurationMS,
			"rows":                    manifest.RowCount,
			"tables":                  manifest.TableCount,
		})
		return nil
	default:
		return fmt.Errorf("unsupported action %q; expected backup, verify, or exercise", opts.action)
	}
}

func parseOptions(args []string) (options, error) {
	var opts options
	fs := flag.NewFlagSet("clickhouse-recovery", flag.ContinueOnError)
	fs.StringVar(&opts.action, "action", defaultAction, "backup, verify, or exercise")
	fs.StringVar(&opts.site, "site", "", "Verself site name")
	fs.StringVar(&opts.database, "database", defaultDatabase, "ClickHouse database to back up and restore")
	fs.StringVar(&opts.point, "point", "", "Recovery point for verify")
	fs.StringVar(&opts.backupDisk, "backup-disk", defaultBackupDisk, "ClickHouse disk allowed for native backups")
	fs.StringVar(&opts.cacheRoot, "cache-root", defaultCacheRoot, "Local recovery manifest cache root")
	fs.StringVar(&opts.operatorConfigPath, "operator-config", defaultOperatorConfig, "ClickHouse operator client XML")
	fs.BoolVar(&opts.keepVerify, "keep-verify", false, "Keep scratch restore database for inspection")
	if err := fs.Parse(args); err != nil {
		return options{}, err
	}
	opts.action = strings.ToLower(strings.TrimSpace(opts.action))
	opts.site = strings.TrimSpace(opts.site)
	opts.database = strings.TrimSpace(opts.database)
	opts.point = strings.TrimSpace(opts.point)
	opts.backupDisk = strings.TrimSpace(opts.backupDisk)
	opts.cacheRoot = strings.TrimSpace(opts.cacheRoot)
	opts.operatorConfigPath = strings.TrimSpace(opts.operatorConfigPath)

	if opts.site == "" {
		return options{}, fmt.Errorf("--site is required")
	}
	for field, value := range map[string]string{
		"site":        opts.site,
		"database":    opts.database,
		"backup-disk": opts.backupDisk,
	} {
		if err := validateSegment(field, value); err != nil {
			return options{}, err
		}
	}
	if opts.action == "verify" {
		if opts.point == "" {
			return options{}, fmt.Errorf("--point is required for --action=verify")
		}
		if err := validateSegment("point", opts.point); err != nil {
			return options{}, err
		}
	}
	if opts.cacheRoot == "" {
		return options{}, fmt.Errorf("--cache-root is required")
	}
	if opts.operatorConfigPath == "" {
		return options{}, fmt.Errorf("--operator-config is required")
	}
	return opts, nil
}

func openClickHouse(ctx context.Context, configPath string, database string) (chdriver.Conn, error) {
	rawConfig, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("read ClickHouse operator config: %w", err)
	}
	operatorCfg, err := parseOperatorConfig(rawConfig)
	if err != nil {
		return nil, err
	}
	material, err := readTLSMaterial(operatorCfg)
	if err != nil {
		return nil, err
	}
	tlsConfig, err := buildTLSConfig(material)
	if err != nil {
		return nil, err
	}
	conn, err := ch.Open(&ch.Options{
		Addr: []string{fmt.Sprintf("%s:%d", operatorCfg.Host, operatorCfg.Port)},
		Auth: ch.Auth{
			Database: database,
			Username: "clickhouse_operator",
		},
		TLS:             tlsConfig,
		DialTimeout:     5 * time.Second,
		ReadTimeout:     30 * time.Minute,
		MaxOpenConns:    2,
		MaxIdleConns:    2,
		ConnMaxLifetime: time.Hour,
	})
	if err != nil {
		return nil, fmt.Errorf("open ClickHouse client: %w", err)
	}
	if err := conn.Ping(ctx); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("ping ClickHouse: %w", err)
	}
	return conn, nil
}

func parseOperatorConfig(raw []byte) (operatorConfig, error) {
	var parsed clickHouseClientConfigXML
	if err := xml.Unmarshal(raw, &parsed); err != nil {
		return operatorConfig{}, fmt.Errorf("parse ClickHouse operator config XML: %w", err)
	}
	cfg := operatorConfig{
		Host:            strings.TrimSpace(parsed.Host),
		Port:            parsed.Port,
		CertificateFile: strings.TrimSpace(parsed.OpenSSL.Client.CertificateFile),
		PrivateKeyFile:  strings.TrimSpace(parsed.OpenSSL.Client.PrivateKeyFile),
		CAConfig:        strings.TrimSpace(parsed.OpenSSL.Client.CAConfig),
	}
	secure, err := strconv.ParseBool(strings.TrimSpace(parsed.Secure))
	if err != nil {
		return operatorConfig{}, fmt.Errorf("ClickHouse operator secure flag must parse as bool: %w", err)
	}
	if !secure {
		return operatorConfig{}, fmt.Errorf("ClickHouse operator config must use TLS")
	}
	if cfg.Host == "" {
		return operatorConfig{}, fmt.Errorf("ClickHouse operator config missing host")
	}
	if cfg.Port <= 0 || cfg.Port > 65535 {
		return operatorConfig{}, fmt.Errorf("ClickHouse operator config has invalid port %d", cfg.Port)
	}
	if cfg.CertificateFile == "" {
		return operatorConfig{}, fmt.Errorf("ClickHouse operator config missing certificateFile")
	}
	if cfg.PrivateKeyFile == "" {
		return operatorConfig{}, fmt.Errorf("ClickHouse operator config missing privateKeyFile")
	}
	if cfg.CAConfig == "" {
		return operatorConfig{}, fmt.Errorf("ClickHouse operator config missing caConfig")
	}
	return cfg, nil
}

type tlsMaterial struct {
	certPEM []byte
	keyPEM  []byte
	caPEM   []byte
}

func readTLSMaterial(cfg operatorConfig) (tlsMaterial, error) {
	certPEM, err := os.ReadFile(cfg.CertificateFile)
	if err != nil {
		return tlsMaterial{}, fmt.Errorf("read ClickHouse operator certificate: %w", err)
	}
	keyPEM, err := os.ReadFile(cfg.PrivateKeyFile)
	if err != nil {
		return tlsMaterial{}, fmt.Errorf("read ClickHouse operator private key: %w", err)
	}
	caPEM, err := os.ReadFile(cfg.CAConfig)
	if err != nil {
		return tlsMaterial{}, fmt.Errorf("read ClickHouse operator CA: %w", err)
	}
	return tlsMaterial{certPEM: certPEM, keyPEM: keyPEM, caPEM: caPEM}, nil
}

func buildTLSConfig(material tlsMaterial) (*tls.Config, error) {
	cert, err := tls.X509KeyPair(material.certPEM, material.keyPEM)
	if err != nil {
		return nil, fmt.Errorf("load ClickHouse operator certificate pair: %w", err)
	}
	roots := x509.NewCertPool()
	if ok := roots.AppendCertsFromPEM(material.caPEM); !ok {
		return nil, fmt.Errorf("load ClickHouse operator CA: no PEM certificates found")
	}
	return &tls.Config{
		MinVersion:   tls.VersionTLS12,
		ServerName:   "127.0.0.1",
		RootCAs:      roots,
		Certificates: []tls.Certificate{cert},
	}, nil
}

func backupDatabase(ctx context.Context, conn chdriver.Conn, opts options) (manifest, error) {
	point := opts.point
	if point == "" {
		var err error
		point, err = newPoint()
		if err != nil {
			return manifest{}, err
		}
	}
	if err := validateSegment("point", point); err != nil {
		return manifest{}, err
	}

	started := time.Now().UTC()
	tables, err := inventoryDatabase(ctx, conn, opts.database)
	if err != nil {
		return manifest{}, err
	}
	version, err := clickHouseVersion(ctx, conn)
	if err != nil {
		return manifest{}, err
	}

	tableCount, rowCount, logicalBytes := summarizeTables(tables)
	backupPath := backupObjectPath(opts.site, point, opts.database)
	backupName := backupDiskName(opts.backupDisk, backupPath)
	operationID := "clickhouse-backup-" + point
	query := fmt.Sprintf(
		"BACKUP DATABASE %s TO Disk(%s, %s) SETTINGS id = %s",
		quoteIdent(opts.database),
		sqlString(opts.backupDisk),
		sqlString(backupPath),
		sqlString(operationID),
	)
	if err := conn.Exec(ctx, query); err != nil {
		event := baseEvent(opts, "backup", point)
		event.Status = "failed"
		event.BackupName = backupName
		event.BackupOperationID = operationID
		event.ExpectedTableCount = tableCount
		event.ExpectedRows = rowCount
		event.BackupDurationMS = durationMillis(time.Since(started), "backup duration")
		event.ErrorKind = fmt.Sprintf("%T", err)
		event.ErrorMessage = err.Error()
		return manifest{}, errors.Join(err, recordRecoveryEvent(ctx, conn, event))
	}

	status, err := lookupBackupStatus(ctx, conn, operationID)
	if err != nil {
		return manifest{}, err
	}
	if status.Status != "BACKUP_CREATED" {
		err := fmt.Errorf("ClickHouse backup ended with status %s: %s", status.Status, status.Error)
		event := baseEvent(opts, "backup", point)
		event.Status = "failed"
		event.BackupName = backupName
		event.BackupOperationID = operationID
		event.ExpectedTableCount = tableCount
		event.ExpectedRows = rowCount
		event.BackupDurationMS = durationMillis(time.Since(started), "backup duration")
		event.ErrorKind = "clickhouse_backup_status"
		event.ErrorMessage = err.Error()
		return manifest{}, errors.Join(err, recordRecoveryEvent(ctx, conn, event))
	}

	finished := time.Now().UTC()
	m := manifest{
		SchemaVersion:          recoverySchemaVersion,
		Site:                   opts.site,
		Component:              component,
		Mechanism:              defaultMechanism,
		Point:                  point,
		Database:               opts.database,
		BackupDisk:             opts.backupDisk,
		BackupPath:             backupPath,
		BackupName:             backupName,
		BackupOperationID:      operationID,
		ClickHouseVersion:      version,
		StartedAt:              started,
		FinishedAt:             finished,
		DurationMS:             durationMillis(finished.Sub(started), "backup duration"),
		TableCount:             tableCount,
		RowCount:               rowCount,
		LogicalBytes:           logicalBytes,
		BackupNumFiles:         status.NumFiles,
		BackupTotalBytes:       status.TotalSize,
		BackupUncompressedSize: status.UncompressedSize,
		BackupCompressedSize:   status.CompressedSize,
		Tables:                 tables,
	}
	if err := writeManifest(opts.cacheRoot, m); err != nil {
		event := eventFromManifest("backup", "failed", m)
		event.ErrorKind = fmt.Sprintf("%T", err)
		event.ErrorMessage = err.Error()
		return manifest{}, errors.Join(err, recordRecoveryEvent(ctx, conn, event))
	}

	event := eventFromManifest("backup", "succeeded", m)
	event.DetailsJSON = mustDetailsJSON(map[string]any{
		"manifest_path": manifestPath(opts.cacheRoot, m),
		"tables":        tables,
	})
	if err := recordRecoveryEvent(ctx, conn, event); err != nil {
		return manifest{}, err
	}
	return m, nil
}

type verifyResult struct {
	Point              string
	TargetDatabase     string
	RestoredTableCount uint32
	RestoredRows       uint64
	RestoreDurationMS  uint32
	VerifyDurationMS   uint32
	RestoreOperationID string
}

func verifyDatabase(ctx context.Context, conn chdriver.Conn, opts options) (verifyResult, error) {
	m, err := readManifest(opts.cacheRoot, opts.site, opts.point, opts.database)
	if err != nil {
		event := baseEvent(opts, "verify", opts.point)
		event.Status = "failed"
		event.ErrorKind = fmt.Sprintf("%T", err)
		event.ErrorMessage = err.Error()
		return verifyResult{}, errors.Join(err, recordRecoveryEvent(ctx, conn, event))
	}
	targetDatabase := verifyDatabaseName(m.Point, m.Database)
	restoreID := "clickhouse-restore-" + m.Point
	result := verifyResult{
		Point:              m.Point,
		TargetDatabase:     targetDatabase,
		RestoreOperationID: restoreID,
	}

	started := time.Now().UTC()
	if err := dropDatabase(ctx, conn, targetDatabase); err != nil {
		return verifyResult{}, err
	}
	restoreQuery := fmt.Sprintf(
		"RESTORE DATABASE %s AS %s FROM Disk(%s, %s) SETTINGS id = %s",
		quoteIdent(m.Database),
		quoteIdent(targetDatabase),
		sqlString(m.BackupDisk),
		sqlString(m.BackupPath),
		sqlString(restoreID),
	)
	restoreStarted := time.Now().UTC()
	restoreErr := conn.Exec(ctx, restoreQuery)
	restoreDuration := time.Since(restoreStarted)
	if restoreErr != nil {
		event := eventFromManifest("verify", "failed", m)
		event.TargetDatabase = targetDatabase
		event.RestoreOperationID = restoreID
		event.RestoreDurationMS = durationMillis(restoreDuration, "restore duration")
		event.VerifyDurationMS = durationMillis(time.Since(started), "verify duration")
		event.ErrorKind = fmt.Sprintf("%T", restoreErr)
		event.ErrorMessage = restoreErr.Error()
		if !opts.keepVerify {
			_ = dropDatabase(ctx, conn, targetDatabase)
		}
		return verifyResult{}, errors.Join(restoreErr, recordRecoveryEvent(ctx, conn, event))
	}

	result.RestoreDurationMS = durationMillis(restoreDuration, "restore duration")
	restoreStatus, err := lookupBackupStatus(ctx, conn, restoreID)
	if err != nil {
		return verifyResult{}, err
	}
	if restoreStatus.Status != "RESTORED" {
		err := fmt.Errorf("ClickHouse restore ended with status %s: %s", restoreStatus.Status, restoreStatus.Error)
		event := eventFromManifest("verify", "failed", m)
		event.TargetDatabase = targetDatabase
		event.RestoreOperationID = restoreID
		event.RestoreDurationMS = result.RestoreDurationMS
		event.VerifyDurationMS = durationMillis(time.Since(started), "verify duration")
		event.ErrorKind = "clickhouse_restore_status"
		event.ErrorMessage = err.Error()
		if !opts.keepVerify {
			_ = dropDatabase(ctx, conn, targetDatabase)
		}
		return verifyResult{}, errors.Join(err, recordRecoveryEvent(ctx, conn, event))
	}

	restoredTables, err := inventoryDatabase(ctx, conn, targetDatabase)
	if err != nil {
		return verifyResult{}, err
	}
	restoredTableCount, restoredRows, _ := summarizeTables(restoredTables)
	result.RestoredTableCount = restoredTableCount
	result.RestoredRows = restoredRows

	compareErr := compareInventories(m.Tables, restoredTables)
	result.VerifyDurationMS = durationMillis(time.Since(started), "verify duration")
	status := "succeeded"
	errorKind := ""
	errorMessage := ""
	if compareErr != nil {
		status = "failed"
		errorKind = fmt.Sprintf("%T", compareErr)
		errorMessage = compareErr.Error()
	}
	event := eventFromManifest("verify", status, m)
	event.TargetDatabase = targetDatabase
	event.RestoreOperationID = restoreID
	event.RestoredTableCount = restoredTableCount
	event.RestoredRows = restoredRows
	event.RestoreDurationMS = result.RestoreDurationMS
	event.VerifyDurationMS = result.VerifyDurationMS
	event.ErrorKind = errorKind
	event.ErrorMessage = errorMessage
	event.DetailsJSON = mustDetailsJSON(map[string]any{
		"manifest_path":   manifestPath(opts.cacheRoot, m),
		"restored_tables": restoredTables,
	})
	recordErr := recordRecoveryEvent(ctx, conn, event)
	if !opts.keepVerify {
		recordErr = errors.Join(recordErr, dropDatabase(ctx, conn, targetDatabase))
	}
	if compareErr != nil {
		return verifyResult{}, errors.Join(compareErr, recordErr)
	}
	if recordErr != nil {
		return verifyResult{}, recordErr
	}
	return result, nil
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

func inventoryDatabase(ctx context.Context, conn chdriver.Conn, database string) ([]tableInfo, error) {
	rows, err := conn.Query(ctx, `
SELECT
    name,
    engine,
    toUInt64(ifNull(total_rows, 0)) AS rows,
    toUInt64(ifNull(total_bytes, 0)) AS bytes,
    hex(sipHash128(create_table_query)) AS create_hash
FROM system.tables
WHERE database = {database:String}
  AND is_temporary = 0
  AND name NOT LIKE '.%'
ORDER BY name`, ch.Named("database", database))
	if err != nil {
		return nil, fmt.Errorf("inventory ClickHouse database %s: %w", database, err)
	}
	defer func() { _ = rows.Close() }()
	var out []tableInfo
	for rows.Next() {
		var table tableInfo
		if err := rows.Scan(&table.Name, &table.Engine, &table.Rows, &table.Bytes, &table.CreateHash); err != nil {
			return nil, fmt.Errorf("scan ClickHouse table inventory: %w", err)
		}
		out = append(out, table)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read ClickHouse table inventory: %w", err)
	}
	return out, nil
}

func summarizeTables(tables []tableInfo) (uint32, uint64, uint64) {
	if len(tables) > math.MaxUint32 {
		panic("ClickHouse table count exceeds uint32 range")
	}
	var rows uint64
	var bytes uint64
	for _, table := range tables {
		rows += table.Rows
		bytes += table.Bytes
	}
	return uint32(len(tables)), rows, bytes
}

func clickHouseVersion(ctx context.Context, conn chdriver.Conn) (string, error) {
	var version string
	if err := conn.QueryRow(ctx, "SELECT version()").Scan(&version); err != nil {
		return "", fmt.Errorf("query ClickHouse version: %w", err)
	}
	return version, nil
}

func lookupBackupStatus(ctx context.Context, conn chdriver.Conn, operationID string) (backupStatus, error) {
	rows, err := conn.Query(ctx, `
SELECT
    id,
    name,
    toString(status),
    error,
    start_time,
    end_time,
    num_files,
    total_size,
    uncompressed_size,
    compressed_size,
    bytes_read
FROM system.backups
WHERE id = {id:String}
ORDER BY start_time DESC
LIMIT 1`, ch.Named("id", operationID))
	if err != nil {
		return backupStatus{}, fmt.Errorf("query system.backups for %s: %w", operationID, err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		return backupStatus{}, fmt.Errorf("system.backups has no row for operation %s", operationID)
	}
	var status backupStatus
	if err := rows.Scan(
		&status.ID,
		&status.Name,
		&status.Status,
		&status.Error,
		&status.StartTime,
		&status.EndTime,
		&status.NumFiles,
		&status.TotalSize,
		&status.UncompressedSize,
		&status.CompressedSize,
		&status.BytesRead,
	); err != nil {
		return backupStatus{}, fmt.Errorf("scan system.backups row: %w", err)
	}
	if err := rows.Err(); err != nil {
		return backupStatus{}, fmt.Errorf("read system.backups row: %w", err)
	}
	return status, nil
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

func baseEvent(opts options, action string, point string) recoveryEvent {
	return recoveryEvent{
		Site:           opts.site,
		Component:      component,
		Mechanism:      defaultMechanism,
		Action:         action,
		Point:          point,
		SourceDatabase: opts.database,
		EventAt:        time.Now().UTC(),
	}
}

func eventFromManifest(action string, status string, m manifest) recoveryEvent {
	return recoveryEvent{
		Site:                    m.Site,
		Component:               component,
		Mechanism:               m.Mechanism,
		Action:                  action,
		Status:                  status,
		Point:                   m.Point,
		BackupName:              m.BackupName,
		BackupOperationID:       m.BackupOperationID,
		SourceDatabase:          m.Database,
		ExpectedTableCount:      m.TableCount,
		ExpectedRows:            m.RowCount,
		BackupNumFiles:          m.BackupNumFiles,
		BackupTotalBytes:        m.BackupTotalBytes,
		BackupUncompressedBytes: m.BackupUncompressedSize,
		BackupCompressedBytes:   m.BackupCompressedSize,
		BackupDurationMS:        m.DurationMS,
		EventAt:                 time.Now().UTC(),
	}
}

func writeManifest(cacheRoot string, m manifest) error {
	target := manifestPath(cacheRoot, m)
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		return fmt.Errorf("create manifest directory: %w", err)
	}
	raw, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal manifest: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(target), ".manifest-*.json")
	if err != nil {
		return fmt.Errorf("create temporary manifest: %w", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if _, err := tmp.Write(raw); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temporary manifest: %w", err)
	}
	if _, err := tmp.Write([]byte("\n")); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("finish temporary manifest: %w", err)
	}
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("chmod temporary manifest: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temporary manifest: %w", err)
	}
	if err := os.Rename(tmpName, target); err != nil {
		return fmt.Errorf("commit manifest: %w", err)
	}
	return nil
}

func readManifest(cacheRoot string, site string, point string, database string) (manifest, error) {
	path := filepath.Join(cacheRoot, filepath.FromSlash(manifestObjectPath(site, point, database)))
	raw, err := os.ReadFile(path)
	if err != nil {
		return manifest{}, fmt.Errorf("read manifest %s: %w", path, err)
	}
	var m manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return manifest{}, fmt.Errorf("parse manifest %s: %w", path, err)
	}
	if m.SchemaVersion != recoverySchemaVersion {
		return manifest{}, fmt.Errorf("manifest schema version %d is not supported", m.SchemaVersion)
	}
	if m.Site != site || m.Point != point || m.Database != database {
		return manifest{}, fmt.Errorf("manifest identity mismatch: got site=%s point=%s database=%s", m.Site, m.Point, m.Database)
	}
	return m, nil
}

func manifestPath(cacheRoot string, m manifest) string {
	return filepath.Join(cacheRoot, filepath.FromSlash(manifestObjectPath(m.Site, m.Point, m.Database)))
}

func manifestObjectPath(site string, point string, database string) string {
	return path.Join(recoveryPointPrefix(site, point), database+".manifest.json")
}

func backupObjectPath(site string, point string, database string) string {
	return path.Join(recoveryPointPrefix(site, point), database+".zip")
}

func recoveryPointPrefix(site string, point string) string {
	return path.Join("dr/v1/sites", site, "components/clickhouse/mechanisms", defaultMechanism, "points", point)
}

func backupDiskName(disk string, backupPath string) string {
	return fmt.Sprintf("Disk(%s, %s)", sqlString(disk), sqlString(backupPath))
}

func compareInventories(expected []tableInfo, actual []tableInfo) error {
	if len(expected) != len(actual) {
		return fmt.Errorf("table count mismatch: expected %d, restored %d", len(expected), len(actual))
	}
	expectedByName := make(map[string]tableInfo, len(expected))
	for _, table := range expected {
		expectedByName[table.Name] = table
	}
	actualNames := make([]string, 0, len(actual))
	for _, table := range actual {
		actualNames = append(actualNames, table.Name)
		want, ok := expectedByName[table.Name]
		if !ok {
			return fmt.Errorf("restored unexpected table %s", table.Name)
		}
		if want.Engine != table.Engine {
			return fmt.Errorf("table %s engine mismatch: expected %s, restored %s", table.Name, want.Engine, table.Engine)
		}
		if want.Rows != table.Rows {
			return fmt.Errorf("table %s row count mismatch: expected %d, restored %d", table.Name, want.Rows, table.Rows)
		}
	}
	sort.Strings(actualNames)
	return nil
}

func dropDatabase(ctx context.Context, conn chdriver.Conn, database string) error {
	if err := conn.Exec(ctx, "DROP DATABASE IF EXISTS "+quoteIdent(database)+" SYNC"); err != nil {
		return fmt.Errorf("drop scratch database %s: %w", database, err)
	}
	return nil
}

func verifyDatabaseName(point string, database string) string {
	safe := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z':
			return r
		case r >= 'A' && r <= 'Z':
			return r
		case r >= '0' && r <= '9':
			return r
		default:
			return '_'
		}
	}, point+"_"+database)
	return verifyDatabasePrefix + safe
}

func newPoint() (string, error) {
	var suffix [6]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		return "", fmt.Errorf("generate recovery point suffix: %w", err)
	}
	return time.Now().UTC().Format(pointTimestampLayout) + "-" + hex.EncodeToString(suffix[:]), nil
}

func validateSegment(field string, value string) error {
	if !safeSegmentPattern.MatchString(value) {
		return fmt.Errorf("%s must match %s", field, safeSegmentPattern.String())
	}
	if clean := path.Clean(value); clean != value || clean == "." || strings.HasPrefix(clean, "..") {
		return fmt.Errorf("%s must be a single safe path segment", field)
	}
	return nil
}

func quoteIdent(identifier string) string {
	return `"` + strings.ReplaceAll(identifier, `"`, `""`) + `"`
}

func sqlString(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, `'`, `\'`)
	return "'" + value + "'"
}

func durationMillis(value time.Duration, field string) uint32 {
	millis := value.Milliseconds()
	if millis < 0 || millis > maxUint32AsInt64 {
		panic(fmt.Sprintf("%s exceeds uint32 milliseconds range: %s", field, value))
	}
	return uint32(millis)
}

func mustDetailsJSON(value any) string {
	raw, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return string(raw)
}

func printSummary(action string, point string, fields map[string]any) {
	fields["action"] = action
	fields["point"] = point
	raw, err := json.Marshal(fields)
	if err != nil {
		panic(err)
	}
	fmt.Println(string(raw))
}
