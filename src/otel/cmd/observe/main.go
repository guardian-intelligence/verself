package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
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

const serviceName = "observe"

var safeCommentValue = regexp.MustCompile(`[^A-Za-z0-9_.:@-]`)

type outputFormat string

const (
	formatTable    outputFormat = "table"
	formatJSON     outputFormat = "json"
	formatMarkdown outputFormat = "markdown"
)

type config struct {
	platformRoot string
	what         string
	signal       string
	service      string
	metric       string
	span         string
	field        string
	queryName    string
	prefix       string
	search       string
	groupBy      string
	mode         string
	traceID      string
	runKey       string
	host         string
	statusMin    uint
	minutes      uint
	limit        uint
	errorsOnly   bool
	format       outputFormat
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

	shutdown, logger, err := fmotel.Init(ctx, fmotel.Config{
		ServiceName:    serviceName,
		ServiceVersion: "0.2.0",
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
	ctx, span := tracer.Start(ctx, "observe",
		trace.WithAttributes(
			attribute.String("observe.run_id", runID),
			attribute.String("observe.what", cfg.what),
			attribute.String("observe.signal", cfg.signal),
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

	var jsonResults []jsonQueryResult
	for i, q := range queries {
		if i > 0 && cfg.format != formatJSON {
			fmt.Println()
		}
		if cfg.format != formatJSON {
			fmt.Printf("=== %s ===\n\n", q.title)
		}
		result, err := runQuery(ctx, logger, cfg, runID, q)
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			os.Exit(1)
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
				os.Exit(1)
			}
		} else {
			if err := encoder.Encode(struct {
				Queries []jsonQueryResult `json:"queries"`
			}{Queries: jsonResults}); err != nil {
				span.RecordError(err)
				span.SetStatus(codes.Error, err.Error())
				os.Exit(1)
			}
		}
	}

	span.SetStatus(codes.Ok, "")
}

func parseConfig(args []string) (config, error) {
	var cfg config
	var format string
	flags := flag.NewFlagSet("observe", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	flags.StringVar(&cfg.platformRoot, "platform-root", "", "path to src/platform")
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
	flags.UintVar(&cfg.statusMin, "status-min", envUint("STATUS_MIN", 0), "minimum HTTP status")
	flags.UintVar(&cfg.minutes, "minutes", envUint("MINUTES", 15), "lookback window for explicit operational queries")
	flags.UintVar(&cfg.limit, "limit", envUint("LIMIT", 25), "maximum rows to print")
	flags.BoolVar(&cfg.errorsOnly, "errors", envBool("ERRORS"), "only show errors where supported")
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
	if cfg.mode == "" {
		cfg.mode = "latest"
	}
	if format == "" {
		format = string(formatTable)
	}
	cfg.format = outputFormat(normalize(format))

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
	if _, err := os.Stat(filepath.Join(cfg.platformRoot, "scripts", "clickhouse.sh")); err != nil {
		return cfg, fmt.Errorf("platform root must contain scripts/clickhouse.sh: %w", err)
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

func runQuery(ctx context.Context, logger *slog.Logger, cfg config, runID string, q query) (*jsonQueryResult, error) {
	sql := withFormat(q.sql, cfg.format)
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

	args := []string{"--database", q.database, "--query_id", clickhouseQueryID}
	for key, value := range q.params {
		args = append(args, "--param_"+key+"="+value)
	}
	args = append(args, "--query", sql)

	cmd := exec.CommandContext(ctx, filepath.Join(cfg.platformRoot, "scripts", "clickhouse.sh"), args...)
	cmd.Dir = cfg.platformRoot

	logger.InfoContext(ctx, "observe query started",
		"query_id", q.id,
		"surface", cfg.what,
		"database", q.database,
	)

	stderr := &bytes.Buffer{}
	cmd.Stderr = stderr
	output, err := cmd.Output()
	// Surface ClickHouse stderr (progress notices, warnings, errors) whether
	// the command succeeded or not — buffering it was only needed to keep
	// stdout clean for row counting, not to swallow diagnostics.
	if stderr.Len() > 0 {
		_, _ = os.Stderr.Write(stderr.Bytes())
	}
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, fmt.Errorf("%s: %w", q.title, err)
	}

	if cfg.format == formatJSON {
		result, err := buildJSONQueryResult(q, sql, clickhouseQueryID, output)
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			return nil, err
		}
		span.SetStatus(codes.Ok, "")
		rows := len(result.Rows)
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

	hasRows, exactCount, exactKnown := evaluateRows(output, cfg.format)
	if !hasRows {
		printEmptyHint(cfg, q)
	} else {
		_, _ = os.Stdout.Write(output)
	}
	printNext(q.next, cfg.format)

	span.SetAttributes(attribute.Bool("observe.has_rows", hasRows))
	logAttrs := []any{
		"query_id", q.id,
		"surface", cfg.what,
		"database", q.database,
		"has_rows", hasRows,
	}
	if exactKnown {
		span.SetAttributes(attribute.Int("observe.rows", exactCount))
		logAttrs = append(logAttrs, "rows", exactCount)
	}
	span.SetStatus(codes.Ok, "")
	logger.InfoContext(ctx, "observe query completed", logAttrs...)
	return nil, nil
}

// evaluateRows reports whether the rendered output contains any data rows,
// and — when it can — the exact count. PrettyCompact (the default table
// format) draws a Unicode frame we don't parse, so for that format the exact
// count is not known; the caller uses hasRows for branching and omits the
// observe.rows span attribute rather than reporting a misleading sentinel.
func evaluateRows(output []byte, format outputFormat) (hasRows bool, exactCount int, exactKnown bool) {
	switch format {
	case formatMarkdown:
		lines := 0
		for _, line := range bytes.Split(output, []byte("\n")) {
			if len(bytes.TrimSpace(line)) > 0 {
				lines++
			}
		}
		// Markdown always emits a header row and a separator; data starts at line 3.
		if lines <= 2 {
			return false, 0, true
		}
		return true, lines - 2, true
	default:
		if len(bytes.TrimSpace(output)) == 0 {
			return false, 0, true
		}
		return true, 0, false
	}
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

func buildJSONQueryResult(q query, sql, clickhouseQueryID string, raw []byte) (jsonQueryResult, error) {
	rows := []json.RawMessage{}
	for _, line := range bytes.Split(raw, []byte("\n")) {
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		if !json.Valid(line) {
			return jsonQueryResult{}, fmt.Errorf("%s: ClickHouse returned non-JSON output: %s", q.title, string(line))
		}
		rows = append(rows, json.RawMessage(append([]byte(nil), line...)))
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

func withFormat(sql string, format outputFormat) string {
	sql = strings.TrimSpace(sql)
	switch format {
	case formatJSON:
		return sql + "\nFORMAT JSONEachRow"
	case formatMarkdown:
		return sql + "\nFORMAT Markdown"
	default:
		return sql + "\nFORMAT PrettyCompact"
	}
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
	if strings.TrimSpace(key) == "" {
		return false
	}
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
