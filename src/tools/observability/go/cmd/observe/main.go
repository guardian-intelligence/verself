package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	ch "github.com/ClickHouse/clickhouse-go/v2"
	opch "github.com/verself/operator-runtime/clickhouse"
	"github.com/verself/operator-runtime/evidence"
	opruntime "github.com/verself/operator-runtime/runtime"
	"github.com/verself/service-runtime/envconfig"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

const serviceName = "observe"

var safeCommentValue = regexp.MustCompile(`[^A-Za-z0-9_.:@-]`)

type outputFormat string

const (
	formatTable    outputFormat = "table"
	formatJSON     outputFormat = "json"
	formatMarkdown outputFormat = "markdown"
)

type config struct {
	repoRoot      string
	substrateRoot string
	site          string
	what          string
	signal        string
	service       string
	metric        string
	span          string
	field         string
	queryName     string
	prefix        string
	search        string
	groupBy       string
	mode          string
	traceID       string
	runKey        string
	host          string
	statusMin     uint
	minutes       uint
	limit         uint
	errorsOnly    bool
	format        outputFormat
}

type query struct {
	id        string
	title     string
	family    string
	purpose   string
	database  string
	sql       string
	params    map[string]string
	next      []string
	windowed  bool
	emptyHint string
}

type jsonQueryResult struct {
	Query struct {
		ID        string            `json:"id"`
		Title     string            `json:"title"`
		Family    string            `json:"family"`
		Purpose   string            `json:"purpose"`
		Database  string            `json:"database"`
		Params    map[string]string `json:"params"`
		QueryID   string            `json:"clickhouse_query_id"`
		SQLSHA256 string            `json:"sql_sha256"`
	} `json:"query"`
	Rows []json.RawMessage `json:"rows"`
	Next []string          `json:"next,omitempty"`
}

func main() {
	ctx := context.Background()
	cfg, err := parseConfig(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}

	if handled, err := handleStatic(cfg); handled {
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		return
	}

	if err := runObserve(ctx, cfg); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func runObserve(ctx context.Context, cfg config) error {
	runID := observeRunID()
	started := time.Now()
	command := "observe." + cfg.what
	return opruntime.Run(ctx, opruntime.Options{
		ServiceName:    serviceName,
		ServiceVersion: "0.3.0",
		Command:        command,
		RepoRoot:       cfg.repoRoot,
		Site:           cfg.site,
		NeedSSH:        true,
		NeedOTel:       true,
	}, func(rt *opruntime.Runtime) error {
		chClient, err := opch.OpenOperator(rt.Ctx, rt, opch.Config{Database: "default"})
		if err != nil {
			return err
		}
		defer func() { _ = chClient.Close() }()
		runErr := runObserveQueries(rt, chClient, cfg, runID)
		recordErr := evidence.Recorder{ClickHouse: chClient}.RecordCommandRun(rt.Ctx, rt, started, command, runErr)
		if runErr != nil {
			return errors.Join(runErr, recordErr)
		}
		return recordErr
	})
}

func runObserveQueries(rt *opruntime.Runtime, chClient *opch.Client, cfg config, runID string) error {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	ctx, span := rt.Tracer.Start(rt.Ctx, "observe",
		trace.WithAttributes(
			attribute.String("observe.run_id", runID),
			attribute.String("observe.what", cfg.what),
			attribute.String("observe.signal", cfg.signal),
			attribute.String("verself.deploy_id", os.Getenv("VERSELF_DEPLOY_ID")),
			attribute.String("verself.deploy_run_key", os.Getenv("VERSELF_DEPLOY_RUN_KEY")),
		),
	)
	defer span.End()
	queries, err := buildQueries(cfg)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}

	var jsonResults []jsonQueryResult
	for i, q := range queries {
		if i > 0 && cfg.format != formatJSON {
			fmt.Println()
		}
		if cfg.format != formatJSON {
			fmt.Printf("=== %s ===\n\n", q.title)
		}
		result, err := runQuery(ctx, logger, chClient, cfg, runID, q)
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			return err
		}
		if result != nil {
			jsonResults = append(jsonResults, *result)
		}
	}
	if cfg.format == formatJSON {
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		if len(jsonResults) == 1 {
			if err := encoder.Encode(jsonResults[0]); err != nil {
				span.RecordError(err)
				span.SetStatus(codes.Error, err.Error())
				return err
			}
		} else {
			if err := encoder.Encode(struct {
				Queries []jsonQueryResult `json:"queries"`
			}{Queries: jsonResults}); err != nil {
				span.RecordError(err)
				span.SetStatus(codes.Error, err.Error())
				return err
			}
		}
	}

	span.SetStatus(codes.Ok, "")
	return nil
}

func parseConfig(args []string) (config, error) {
	var cfg config
	var format string
	flags := flag.NewFlagSet("observe", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	flags.StringVar(&cfg.repoRoot, "repo-root", "", "verself-sh checkout root (defaults to cwd)")
	flags.StringVar(&cfg.substrateRoot, "substrate-root", "", "path to src/host")
	flags.StringVar(&cfg.site, "site", strings.TrimSpace(os.Getenv("VERSELF_SITE")), "deployment site")
	flags.StringVar(&cfg.what, "what", strings.TrimSpace(os.Getenv("WHAT")), "query family to run")
	flags.StringVar(&cfg.signal, "signal", strings.TrimSpace(os.Getenv("SIGNAL")), "signal catalog: metrics, traces, logs, http, deploys")
	flags.StringVar(&cfg.service, "service", strings.TrimSpace(os.Getenv("SERVICE")), "service name")
	flags.StringVar(&cfg.metric, "metric", strings.TrimSpace(os.Getenv("METRIC")), "metric name")
	flags.StringVar(&cfg.span, "span", strings.TrimSpace(os.Getenv("SPAN")), "span name")
	flags.StringVar(&cfg.field, "field", strings.TrimSpace(os.Getenv("FIELD")), "attribute or field name")
	flags.StringVar(&cfg.queryName, "query", strings.TrimSpace(os.Getenv("QUERY")), "observe query id to describe")
	flags.StringVar(&cfg.prefix, "prefix", strings.TrimSpace(os.Getenv("PREFIX")), "metric or name prefix filter")
	flags.StringVar(&cfg.search, "search", strings.TrimSpace(os.Getenv("SEARCH")), "case-insensitive name search")
	flags.StringVar(&cfg.groupBy, "group-by", strings.TrimSpace(os.Getenv("GROUP_BY")), "metric attribute key to group by")
	flags.StringVar(&cfg.mode, "mode", strings.TrimSpace(os.Getenv("MODE")), "metric mode: latest or rate")
	flags.StringVar(&cfg.traceID, "trace-id", strings.TrimSpace(os.Getenv("TRACE_ID")), "trace id to inspect")
	flags.StringVar(&cfg.runKey, "run-key", strings.TrimSpace(os.Getenv("RUN_KEY")), "deploy_run_key to inspect")
	flags.StringVar(&cfg.host, "host", strings.TrimSpace(os.Getenv("HOST")), "HTTP host filter")
	defaults := envconfig.New()
	flags.UintVar(&cfg.statusMin, "status-min", defaults.Uint("STATUS_MIN", 0), "minimum HTTP status")
	flags.UintVar(&cfg.minutes, "minutes", defaults.Uint("MINUTES", 15), "lookback window for explicit operational queries")
	flags.UintVar(&cfg.limit, "limit", defaults.Uint("LIMIT", 25), "maximum rows to print")
	flags.BoolVar(&cfg.errorsOnly, "errors", defaults.Bool("ERRORS", false), "only show errors where supported")
	flags.StringVar(&format, "format", strings.TrimSpace(os.Getenv("FORMAT")), "output format: table, json, markdown")

	if err := flags.Parse(args); err != nil {
		return cfg, err
	}

	cfg.what = normalize(strings.TrimSpace(cfg.what))
	cfg.signal = normalize(strings.TrimSpace(cfg.signal))
	cfg.service = strings.TrimSpace(cfg.service)
	cfg.metric = strings.TrimSpace(cfg.metric)
	cfg.span = strings.TrimSpace(cfg.span)
	cfg.field = strings.TrimSpace(cfg.field)
	cfg.queryName = strings.TrimSpace(cfg.queryName)
	cfg.prefix = strings.TrimSpace(cfg.prefix)
	cfg.search = strings.TrimSpace(cfg.search)
	cfg.groupBy = strings.TrimSpace(cfg.groupBy)
	cfg.mode = normalize(strings.TrimSpace(cfg.mode))
	cfg.traceID = strings.TrimSpace(cfg.traceID)
	cfg.runKey = strings.TrimSpace(cfg.runKey)
	cfg.host = strings.TrimSpace(cfg.host)
	cfg.site = strings.TrimSpace(cfg.site)
	if cfg.mode == "" {
		cfg.mode = "latest"
	}
	if format == "" {
		format = string(formatTable)
	}
	cfg.format = outputFormat(normalize(format))

	if cfg.repoRoot == "" {
		wd, err := os.Getwd()
		if err != nil {
			return cfg, fmt.Errorf("resolve working directory: %w", err)
		}
		cfg.repoRoot = wd
	}
	if cfg.substrateRoot == "" {
		cfg.substrateRoot = filepath.Join(cfg.repoRoot, "src", "host")
	}
	if cfg.site == "" {
		cfg.site = opruntime.DefaultSite
	}
	if cfg.minutes == 0 {
		return cfg, errors.New("--minutes must be greater than zero")
	}
	if cfg.limit == 0 || cfg.limit > 500 {
		return cfg, errors.New("--limit must be between 1 and 500")
	}
	if cfg.statusMin > 599 {
		return cfg, errors.New("--status-min must be between 0 and 599")
	}
	if cfg.format != formatTable && cfg.format != formatJSON && cfg.format != formatMarkdown {
		return cfg, errors.New("--format must be table, json, or markdown")
	}
	for label, value := range map[string]string{
		"--service":  cfg.service,
		"--metric":   cfg.metric,
		"--span":     cfg.span,
		"--field":    cfg.field,
		"--query":    cfg.queryName,
		"--prefix":   cfg.prefix,
		"--group-by": cfg.groupBy,
		"--trace-id": cfg.traceID,
		"--run-key":  cfg.runKey,
		"--host":     cfg.host,
	} {
		if err := validateToken(label, value); err != nil {
			return cfg, err
		}
	}
	if len(cfg.search) > 128 {
		return cfg, errors.New("--search must be at most 128 characters")
	}
	return cfg, nil
}

func validateToken(label, value string) error {
	if value == "" {
		return nil
	}
	if len(value) > 256 {
		return fmt.Errorf("%s must be at most 256 characters", label)
	}
	if !regexp.MustCompile(`^[A-Za-z0-9_.,:/@*+=-]+$`).MatchString(value) {
		return fmt.Errorf("%s contains unsupported characters", label)
	}
	return nil
}

func runQuery(ctx context.Context, logger *slog.Logger, chClient *opch.Client, cfg config, runID string, q query) (*jsonQueryResult, error) {
	sql := strings.TrimSpace(q.sql)
	clickhouseQueryID := queryID(runID, q)
	ctx, span := otel.Tracer(serviceName).Start(ctx, "clickhouse.query",
		trace.WithAttributes(
			attribute.String("observe.run_id", runID),
			attribute.String("observe.what", cfg.what),
			attribute.String("observe.signal", cfg.signal),
			attribute.String("observe.query_id", q.id),
			attribute.String("observe.query_family", q.family),
			attribute.String("clickhouse.database", q.database),
			attribute.String("clickhouse.query_id", clickhouseQueryID),
			attribute.String("clickhouse.query_name", q.title),
			attribute.String("clickhouse.query_sha256", queryHash(sql)),
		),
	)
	defer span.End()

	logger.InfoContext(ctx, "observe query started",
		"query_id", q.id,
		"surface", cfg.what,
		"database", q.database,
	)

	queryCtx := ch.Context(ctx, ch.WithQueryID(clickhouseQueryID))
	table, err := chClient.QueryTableParams(queryCtx, sql, q.params)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, fmt.Errorf("%s: %w", q.title, err)
	}
	rows := len(table.Rows)

	if cfg.format == formatJSON {
		result, err := buildJSONQueryResult(q, sql, clickhouseQueryID, table)
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			return nil, err
		}
		span.SetStatus(codes.Ok, "")
		span.SetAttributes(
			attribute.Bool("observe.has_rows", rows > 0),
			attribute.Int("observe.rows", rows),
		)
		logger.InfoContext(ctx, "observe query completed",
			"query_id", q.id,
			"surface", cfg.what,
			"database", q.database,
			"rows", rows,
		)
		return &result, nil
	}

	if rows == 0 {
		printEmptyHint(cfg, q)
	} else {
		if cfg.format == formatMarkdown {
			printMarkdownTable(os.Stdout, table)
		} else if err := opruntime.PrintTable(os.Stdout, table); err != nil {
			return nil, err
		}
	}
	printNext(q.next, cfg.format)

	span.SetAttributes(
		attribute.Bool("observe.has_rows", rows > 0),
		attribute.Int("observe.rows", rows),
	)
	logAttrs := []any{
		"query_id", q.id,
		"surface", cfg.what,
		"database", q.database,
		"has_rows", rows > 0,
		"rows", rows,
	}
	span.SetStatus(codes.Ok, "")
	logger.InfoContext(ctx, "observe query completed", logAttrs...)
	return nil, nil
}

func printEmptyHint(cfg config, q query) {
	var message string
	if q.windowed {
		unit := "minutes"
		if cfg.minutes == 1 {
			unit = "minute"
		}
		message = fmt.Sprintf("0 rows in the last %d %s.", cfg.minutes, unit)
	} else {
		message = "0 rows."
	}
	hints := []string{}
	if q.windowed {
		suggested := nextWindowSuggestion(cfg.minutes)
		if suggested > 0 {
			hints = append(hints, fmt.Sprintf("Widen the window: re-run with MINUTES=%d.", suggested))
		}
	}
	if strings.TrimSpace(q.emptyHint) != "" {
		hints = append(hints, strings.TrimSpace(q.emptyHint))
	}

	switch cfg.format {
	case formatMarkdown:
		fmt.Printf("_%s_\n", message)
		for _, hint := range hints {
			fmt.Printf("- %s\n", hint)
		}
	default:
		fmt.Println(message)
		for _, hint := range hints {
			fmt.Printf("  %s\n", hint)
		}
	}
}

func printMarkdownTable(w *os.File, table opruntime.Table) {
	_, _ = fmt.Fprint(w, "|")
	for _, header := range table.Headers {
		_, _ = fmt.Fprintf(w, " %s |", markdownCell(header))
	}
	_, _ = fmt.Fprintln(w)
	_, _ = fmt.Fprint(w, "|")
	for range table.Headers {
		_, _ = fmt.Fprint(w, " --- |")
	}
	_, _ = fmt.Fprintln(w)
	for _, row := range table.Rows {
		_, _ = fmt.Fprint(w, "|")
		for i := range table.Headers {
			value := ""
			if i < len(row) {
				value = row[i]
			}
			_, _ = fmt.Fprintf(w, " %s |", markdownCell(value))
		}
		_, _ = fmt.Fprintln(w)
	}
}

func markdownCell(value string) string {
	value = strings.ReplaceAll(value, "|", "\\|")
	value = strings.ReplaceAll(value, "\r\n", "<br>")
	value = strings.ReplaceAll(value, "\n", "<br>")
	return value
}

// nextWindowSuggestion returns a wider lookback in minutes, or 0 if the caller
// is already looking at a month-plus window and widening further is unlikely to help.
func nextWindowSuggestion(current uint) uint {
	switch {
	case current < 1440:
		return 1440
	case current < 10080:
		return 10080
	case current < 43200:
		return 43200
	default:
		return 0
	}
}

func buildJSONQueryResult(q query, sql, clickhouseQueryID string, table opruntime.Table) (jsonQueryResult, error) {
	rows := []json.RawMessage{}
	for _, row := range table.Rows {
		obj := make(map[string]string, len(table.Headers))
		for i, header := range table.Headers {
			if i < len(row) {
				obj[header] = row[i]
			} else {
				obj[header] = ""
			}
		}
		encoded, err := json.Marshal(obj)
		if err != nil {
			return jsonQueryResult{}, fmt.Errorf("%s: encode JSON row: %w", q.title, err)
		}
		rows = append(rows, json.RawMessage(encoded))
	}
	result := jsonQueryResult{Rows: rows, Next: q.next}
	result.Query.ID = q.id
	result.Query.Title = q.title
	result.Query.Family = q.family
	result.Query.Purpose = q.purpose
	result.Query.Database = q.database
	result.Query.Params = q.params
	result.Query.QueryID = clickhouseQueryID
	result.Query.SQLSHA256 = queryHash(sql)
	return result, nil
}

func printNext(next []string, format outputFormat) {
	if len(next) == 0 {
		return
	}
	switch format {
	case formatMarkdown:
		fmt.Println("\nNext:")
		for _, item := range next {
			fmt.Printf("- `%s`\n", item)
		}
	default:
		fmt.Println("\nNext:")
		for _, item := range next {
			fmt.Printf("  %s\n", item)
		}
	}
}

func observeRunID() string {
	for _, key := range []string{"VERSELF_OBSERVE_RUN_ID", "VERSELF_DEPLOY_RUN_KEY"} {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return value
		}
	}
	return "observe-" + time.Now().UTC().Format("20060102T150405Z")
}

func queryHash(sql string) string {
	sum := sha256.Sum256([]byte(sql))
	return hex.EncodeToString(sum[:])
}

func queryID(runID string, q query) string {
	hash := queryHash(q.sql)
	return fmt.Sprintf("observe:%s:%s:%s", commentValue(runID), commentValue(q.id), hash[:12])
}

func commentValue(value string) string {
	value = safeCommentValue.ReplaceAllString(value, "_")
	if len(value) > 96 {
		return value[:96]
	}
	return value
}

func normalize(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}
