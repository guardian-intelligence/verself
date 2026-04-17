package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	fmotel "github.com/forge-metal/otel"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

const serviceName = "fm-observe"

var safeCommentValue = regexp.MustCompile(`[^A-Za-z0-9_.:@-]`)

type config struct {
	platformRoot string
	what         string
	service      string
	metric       string
	minutes      uint
	limit        uint
	errorsOnly   bool
}

type query struct {
	name     string
	database string
	sql      string
	params   map[string]string
}

func main() {
	ctx := context.Background()
	cfg, err := parseConfig(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}

	if cfg.what == "" {
		printUsage()
		return
	}

	shutdown, logger, err := fmotel.Init(ctx, fmotel.Config{
		ServiceName:    serviceName,
		ServiceVersion: "0.1.0",
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "initialize otel: %v\n", err)
		os.Exit(1)
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := shutdown(shutdownCtx); err != nil {
			fmt.Fprintf(os.Stderr, "flush otel: %v\n", err)
		}
	}()

	runID := observeRunID()
	tracer := otel.Tracer(serviceName)
	ctx, span := tracer.Start(ctx, "fm.observe",
		trace.WithAttributes(
			attribute.String("observe.run_id", runID),
			attribute.String("observe.what", cfg.what),
			attribute.String("forge_metal.deploy_id", os.Getenv("FORGE_METAL_DEPLOY_ID")),
			attribute.String("forge_metal.deploy_run_key", os.Getenv("FORGE_METAL_DEPLOY_RUN_KEY")),
		),
	)
	defer span.End()

	queries, err := buildQueries(cfg)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}

	for i, q := range queries {
		if i > 0 {
			fmt.Println()
		}
		fmt.Printf("=== %s ===\n\n", q.name)
		if err := runQuery(ctx, logger, cfg.platformRoot, runID, cfg.what, q); err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			os.Exit(1)
		}
	}

	span.SetStatus(codes.Ok, "")
}

func parseConfig(args []string) (config, error) {
	var cfg config
	flags := flag.NewFlagSet("fm-observe", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	flags.StringVar(&cfg.platformRoot, "platform-root", "", "path to src/platform")
	flags.StringVar(&cfg.what, "what", strings.TrimSpace(os.Getenv("WHAT")), "surface to observe")
	flags.StringVar(&cfg.service, "service", strings.TrimSpace(os.Getenv("SERVICE")), "service name for service/errors surfaces")
	flags.StringVar(&cfg.metric, "metric", strings.TrimSpace(os.Getenv("METRIC")), "metric name for metric surface")
	flags.UintVar(&cfg.minutes, "minutes", envUint("MINUTES", 15), "lookback window in minutes")
	flags.UintVar(&cfg.limit, "limit", envUint("LIMIT", 25), "maximum rows to print")
	flags.BoolVar(&cfg.errorsOnly, "errors", envBool("ERRORS"), "only show errors where supported")

	if err := flags.Parse(args); err != nil {
		return cfg, err
	}
	cfg.what = strings.TrimSpace(cfg.what)
	cfg.service = strings.TrimSpace(cfg.service)
	cfg.metric = strings.TrimSpace(cfg.metric)

	if cfg.platformRoot == "" {
		wd, err := os.Getwd()
		if err != nil {
			return cfg, fmt.Errorf("resolve working directory: %w", err)
		}
		cfg.platformRoot = filepath.Join(wd, "src", "platform")
	}
	if cfg.minutes == 0 {
		return cfg, errors.New("--minutes must be greater than zero")
	}
	if cfg.limit == 0 || cfg.limit > 500 {
		return cfg, errors.New("--limit must be between 1 and 500")
	}
	if cfg.service != "" && !regexp.MustCompile(`^[A-Za-z0-9_.-]+$`).MatchString(cfg.service) {
		return cfg, errors.New("--service must contain only letters, numbers, dot, underscore, or dash")
	}
	if cfg.metric != "" && !regexp.MustCompile(`^[A-Za-z0-9_.:/-]+$`).MatchString(cfg.metric) {
		return cfg, errors.New("--metric must contain only letters, numbers, dot, underscore, slash, colon, or dash")
	}
	if _, err := os.Stat(filepath.Join(cfg.platformRoot, "scripts", "clickhouse.sh")); err != nil {
		return cfg, fmt.Errorf("platform root must contain scripts/clickhouse.sh: %w", err)
	}
	return cfg, nil
}

func buildQueries(cfg config) ([]query, error) {
	baseParams := map[string]string{
		"minutes":   strconv.FormatUint(uint64(cfg.minutes), 10),
		"row_limit": strconv.FormatUint(uint64(cfg.limit), 10),
		"service":   cfg.service,
		"metric":    cfg.metric,
	}

	switch cfg.what {
	case "catalog":
		return []query{{
			name:     "Metric Catalog",
			database: "default",
			params:   baseParams,
			sql: `
SELECT
  ServiceName AS service,
  MetricName AS metric,
  MetricKind AS kind,
  MetricUnit AS unit,
  AttributeSets AS attr_sets,
  Samples AS samples,
  LastSeenAt AS last_seen
FROM default.otel_metric_catalog_live
WHERE LastSeenAt > now() - toIntervalMinute({minutes:UInt32})
ORDER BY LastSeenAt DESC, service, metric
LIMIT {row_limit:UInt32}
FORMAT PrettyCompact`,
		}}, nil
	case "metric":
		if cfg.metric == "" {
			return nil, errors.New("WHAT=metric requires METRIC=<metric_name>")
		}
		return []query{{
			name:     "Latest Metric Samples",
			database: "default",
			params:   baseParams,
			sql: `
SELECT
  ServiceName AS service,
  MetricName AS metric,
  MetricKind AS kind,
  CurrentValue AS value,
  MetricUnit AS unit,
  SampledAt AS sampled_at,
  toString(Attributes) AS attrs
FROM default.otel_metric_latest
WHERE MetricName = {metric:String}
ORDER BY sampled_at DESC, service
LIMIT {row_limit:UInt32}
FORMAT PrettyCompact`,
		}}, nil
	case "errors":
		return []query{{
			name:     "Recent Errors",
			database: "default",
			params:   baseParams,
			sql: `
SELECT
  formatDateTime(Timestamp, '%H:%i:%S') AS time,
  SignalKind AS signal,
  ServiceName AS service,
  Severity AS severity,
  HttpStatus AS status,
  Path AS path,
  Name AS name,
  TraceId AS trace_id
FROM default.otel_signal_errors
WHERE Timestamp > now() - toIntervalMinute({minutes:UInt32})
  AND ({service:String} = '' OR ServiceName = {service:String})
ORDER BY Timestamp DESC
LIMIT {row_limit:UInt32}
FORMAT PrettyCompact`,
		}}, nil
	case "service":
		if cfg.service == "" {
			return nil, errors.New("WHAT=service requires SERVICE=<service_name>")
		}
		if cfg.errorsOnly {
			return buildQueries(config{
				what:       "errors",
				service:    cfg.service,
				minutes:    cfg.minutes,
				limit:      cfg.limit,
				errorsOnly: true,
			})
		}
		return []query{
			{
				name:     "Recent HTTP Spans",
				database: "default",
				params:   baseParams,
				sql: `
SELECT
  formatDateTime(Timestamp, '%H:%i:%S') AS time,
  SpanName AS span,
  SpanAttributes['http.method'] AS method,
  SpanAttributes['http.target'] AS path,
  SpanAttributes['http.status_code'] AS status,
  intDiv(Duration, 1000000) AS ms,
  TraceId AS trace_id
FROM default.otel_traces
WHERE Timestamp > now() - toIntervalMinute({minutes:UInt32})
  AND ServiceName = {service:String}
  AND SpanAttributes['http.target'] != ''
ORDER BY Timestamp DESC
LIMIT {row_limit:UInt32}
FORMAT PrettyCompact`,
			},
			{
				name:     "Recent Logs",
				database: "default",
				params:   baseParams,
				sql: `
SELECT
  formatDateTime(Timestamp, '%H:%i:%S') AS time,
  SeverityText AS level,
  Body AS message,
  TraceId AS trace_id
FROM default.otel_logs
WHERE Timestamp > now() - toIntervalMinute({minutes:UInt32})
  AND ServiceName = {service:String}
ORDER BY Timestamp DESC
LIMIT {row_limit:UInt32}
FORMAT PrettyCompact`,
			},
		}, nil
	case "mail":
		return []query{
			{
				name:     "Mail Events",
				database: "default",
				params:   baseParams,
				sql: `
SELECT
  formatDateTime(Timestamp, '%H:%i:%S') AS time,
  Direction AS direction,
  EventType AS event,
  nullIf(MailboxAccount, '') AS mailbox,
  nullIf(Sender, '') AS sender,
  nullIf(Subject, '') AS subject,
  Message AS message
FROM default.mail_events
WHERE Timestamp > now() - toIntervalMinute({minutes:UInt32})
ORDER BY Timestamp DESC
LIMIT {row_limit:UInt32}
FORMAT PrettyCompact`,
			},
			{
				name:     "Mail Metrics",
				database: "default",
				params:   baseParams,
				sql: `
SELECT
  MetricGroup AS group,
  MetricName AS metric,
  CurrentValue AS value,
  SampledAt AS sampled_at
FROM default.mail_metrics_latest
ORDER BY group, metric
LIMIT {row_limit:UInt32}
FORMAT PrettyCompact`,
			},
		}, nil
	case "deploy":
		return []query{{
			name:     "Recent Deploy Tasks",
			database: "default",
			params:   baseParams,
			sql: `
SELECT
  formatDateTime(Timestamp, '%H:%i:%S') AS time,
  extract(SpanAttributes['ansible.task.name'], ': ([A-Za-z0-9_-]+) :') AS role,
  SpanAttributes['ansible.task.name'] AS task,
  StatusCode AS status,
  SpanAttributes['forge_metal.deploy_run_key'] AS deploy_run_key,
  TraceId AS trace_id
FROM default.otel_traces
WHERE Timestamp > now() - toIntervalMinute({minutes:UInt32})
  AND ServiceName = 'ansible'
  AND SpanName = 'ansible.task'
ORDER BY Timestamp DESC
LIMIT {row_limit:UInt32}
FORMAT PrettyCompact`,
		}}, nil
	default:
		return nil, fmt.Errorf("unknown WHAT=%q", cfg.what)
	}
}

func runQuery(ctx context.Context, logger *slog.Logger, platformRoot, runID, surface string, q query) error {
	ctx, span := otel.Tracer(serviceName).Start(ctx, "clickhouse.query",
		trace.WithAttributes(
			attribute.String("observe.run_id", runID),
			attribute.String("observe.what", surface),
			attribute.String("clickhouse.database", q.database),
			attribute.String("clickhouse.query_id", queryID(runID, surface, q)),
			attribute.String("clickhouse.query_name", q.name),
			attribute.String("clickhouse.query_sha256", queryHash(q.sql)),
		),
	)
	defer span.End()

	marker := fmt.Sprintf("/* fm:observe run=%s surface=%s query=%s */\n",
		commentValue(runID),
		commentValue(surface),
		commentValue(q.name),
	)
	args := []string{"--database", q.database, "--query_id", queryID(runID, surface, q)}
	for key, value := range q.params {
		args = append(args, "--param_"+key+"="+value)
	}
	args = append(args, "--query", marker+q.sql)

	cmd := exec.CommandContext(ctx, filepath.Join(platformRoot, "scripts", "clickhouse.sh"), args...)
	cmd.Dir = platformRoot
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	logger.InfoContext(ctx, "observe query started",
		"surface", surface,
		"query", q.name,
		"database", q.database,
	)
	if err := cmd.Run(); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return fmt.Errorf("%s: %w", q.name, err)
	}
	span.SetStatus(codes.Ok, "")
	logger.InfoContext(ctx, "observe query completed",
		"surface", surface,
		"query", q.name,
		"database", q.database,
	)
	return nil
}

func observeRunID() string {
	for _, key := range []string{"FM_OBSERVE_RUN_ID", "FORGE_METAL_DEPLOY_RUN_KEY"} {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return value
		}
	}
	return "observe-" + time.Now().UTC().Format("20060102T150405Z")
}

func envUint(key string, fallback uint) uint {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseUint(value, 10, 32)
	if err != nil || parsed == 0 {
		return fallback
	}
	return uint(parsed)
}

func envBool(key string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(key))) {
	case "1", "true", "yes", "y":
		return true
	default:
		return false
	}
}

func queryHash(sql string) string {
	sum := sha256.Sum256([]byte(sql))
	return hex.EncodeToString(sum[:])
}

func queryID(runID, surface string, q query) string {
	hash := queryHash(q.sql)
	return fmt.Sprintf("fm-observe:%s:%s:%s", commentValue(runID), commentValue(surface), hash[:12])
}

func commentValue(value string) string {
	value = safeCommentValue.ReplaceAllString(value, "_")
	if len(value) > 96 {
		return value[:96]
	}
	return value
}

func printUsage() {
	fmt.Println(`Forge Metal Observability

Surfaces:
  catalog   live metric catalog from semantic OTel views
  metric    latest samples for one metric name
  service   recent traces and logs for one service
  errors    recent errors across traces, logs, and access logs
  mail      mail events and Stalwart metrics
  deploy    recent Ansible task spans

Examples:
  make observe WHAT=catalog
  make observe WHAT=metric METRIC=process.cpu.time
  make observe WHAT=service SERVICE=billing-service
  make observe WHAT=service SERVICE=sandbox-rental-service ERRORS=1
  make observe WHAT=mail
  make observe WHAT=deploy`)
}
