// Package chwriter is the deployment-tooling's typed ClickHouse
// writer. It executes INSERT statements against the controller-side
// ClickHouse via the operator's existing SSH session — the same shape
// scripts/clickhouse.sh has used since substrate v0, but with Go-side
// SQL-literal escaping in place of bash + python heredoc string
// concatenation.
//
// One row of one insert costs one ssh.Exec. Callers batching many
// rows should pass the full slice to InsertRows once; the writer
// composes one multi-row INSERT VALUES (...) expression per call.
//
// Boundary: this package does not own the SSH session. Callers
// construct it from a *sshtun.Client and a database name, then call
// Insert/InsertRows. ClickHouse access goes through `sudo -u
// clickhouse_operator clickhouse-client` to inherit the operator's
// /etc/clickhouse-client/operator.xml — same auth contract as the
// existing scripts/clickhouse.sh.
package chwriter

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/verself/deployment-tooling/internal/sshtun"
)

const tracerName = "github.com/verself/deployment-tooling/internal/chwriter"

// Executor is the minimum sshtun.Client surface chwriter needs. The
// interface lives here (not in sshtun) so a test fake can be supplied
// without importing the SSH package.
type Executor interface {
	Exec(ctx context.Context, command string) ([]byte, error)
}

// Writer composes INSERT statements and ships them through the
// operator's SSH session.
type Writer struct {
	ssh      Executor
	database string
	tracer   trace.Tracer
}

// New returns a Writer that targets the given ClickHouse database.
// The Executor is typically a *sshtun.Client; tests may pass a fake
// matching the Executor interface.
func New(ssh Executor, database string) *Writer {
	if database == "" {
		database = "verself"
	}
	return &Writer{
		ssh:      ssh,
		database: database,
		tracer:   otel.Tracer(tracerName),
	}
}

// Value is a single column value; constructed via the typed helpers
// below (String, Int, UInt, Float, DateTimeNow, Array). Direct field
// access is discouraged so the Render path can stay in one file.
type Value struct {
	rendered string
}

// String quotes and escapes per ClickHouse's standard string-literal
// rules. Backslash and apostrophe are the only metacharacters that
// matter inside a single-quoted literal.
func String(s string) Value {
	r := strings.NewReplacer(`\`, `\\`, `'`, `\'`).Replace(s)
	return Value{rendered: "'" + r + "'"}
}

// Int renders a signed integer literal.
func Int(n int64) Value {
	return Value{rendered: strconv.FormatInt(n, 10)}
}

// UInt renders an unsigned integer literal.
func UInt(n uint64) Value {
	return Value{rendered: strconv.FormatUint(n, 10)}
}

// Float renders a float literal with full precision.
func Float(f float64) Value {
	return Value{rendered: strconv.FormatFloat(f, 'g', -1, 64)}
}

// DateTimeNow renders a now64(9) call so the server stamps the
// insert with its own clock. Preferred over a Go-side clock when the
// row's intent is "this happened just now".
func DateTimeNow() Value { return Value{rendered: "now64(9)"} }

// DateTime renders a DateTime64(9, 'UTC') literal from a time.Time.
// Used when the server clock is wrong for the row's semantic time
// (e.g. a parsed Ansible task completion timestamp).
func DateTime(t time.Time) Value {
	if t.IsZero() {
		return DateTimeNow()
	}
	// fromUnixTimestamp64Nano accepts a nanosecond integer and yields a
	// DateTime64(9). It avoids client-side string formatting variance
	// across timezone-aware and naive types.
	return Value{rendered: "fromUnixTimestamp64Nano(" + strconv.FormatInt(t.UnixNano(), 10) + ")"}
}

// StringArray renders a [string,...] literal of escaped strings.
// Empty slice yields the empty array literal `[]`.
func StringArray(values []string) Value {
	if len(values) == 0 {
		return Value{rendered: "[]"}
	}
	parts := make([]string, len(values))
	for i, v := range values {
		parts[i] = String(v).rendered
	}
	return Value{rendered: "[" + strings.Join(parts, ",") + "]"}
}

// Row is a column-name → value map for a single insert row. Callers
// must use the same column set across rows in one InsertRows call;
// the writer fixes the column order from the first row's keys.
type Row map[string]Value

// Insert ships a single row to (database, table). Equivalent to
// InsertRows with a one-row slice.
func (w *Writer) Insert(ctx context.Context, table string, row Row) error {
	return w.InsertRows(ctx, table, []Row{row})
}

// InsertRows ships a multi-row INSERT VALUES against the configured
// database. Returns an error when the columns drift across rows or
// when ClickHouse rejects the statement.
func (w *Writer) InsertRows(ctx context.Context, table string, rows []Row) error {
	if len(rows) == 0 {
		return nil
	}
	if !validIdent(table) {
		return fmt.Errorf("chwriter: invalid table name: %q", table)
	}

	// Column order is fixed by the first row; subsequent rows must
	// supply exactly the same keys so the value tuples line up.
	cols := make([]string, 0, len(rows[0]))
	for col := range rows[0] {
		if !validIdent(col) {
			return fmt.Errorf("chwriter: invalid column name: %q", col)
		}
		cols = append(cols, col)
	}
	// Sort for deterministic output; column order is irrelevant to
	// ClickHouse semantics but matters for test snapshots and span
	// readability.
	sortColsInPlace(cols)

	tuples := make([]string, len(rows))
	for i, row := range rows {
		if len(row) != len(cols) {
			return fmt.Errorf("chwriter: row %d has %d cols, expected %d", i, len(row), len(cols))
		}
		parts := make([]string, len(cols))
		for j, col := range cols {
			v, ok := row[col]
			if !ok {
				return fmt.Errorf("chwriter: row %d missing column %q", i, col)
			}
			parts[j] = v.rendered
		}
		tuples[i] = "(" + strings.Join(parts, ",") + ")"
	}

	stmt := "INSERT INTO " + w.database + "." + table +
		" (" + strings.Join(cols, ",") + ") VALUES " +
		strings.Join(tuples, ",")

	ctx, span := w.tracer.Start(ctx, "verself_deploy.clickhouse.insert",
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(
			attribute.String("db.system", "clickhouse"),
			attribute.String("db.name", w.database),
			attribute.String("db.sql.table", table),
			attribute.Int("db.row_count", len(rows)),
			attribute.Int("db.statement.bytes", len(stmt)),
		),
	)
	defer span.End()

	if w.ssh == nil {
		err := errors.New("chwriter: nil ssh executor")
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}

	cmd := remoteCommand(w.database, stmt)
	if _, err := w.ssh.Exec(ctx, cmd); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return fmt.Errorf("chwriter: insert into %s.%s: %w", w.database, table, err)
	}
	span.SetStatus(codes.Ok, "")
	return nil
}

// remoteCommand mirrors scripts/clickhouse.sh's "sudo -u
// clickhouse_operator clickhouse-client --config-file ... --user ...
// --database ... --query=..." invocation. The query is single-quoted
// at the bash layer; embedded apostrophes are escaped via
// '\'' (close, escape, reopen). Fields outside the query are static
// identifiers, so no further escaping is needed.
func remoteCommand(database, stmt string) string {
	const (
		clientPath  = "/opt/verself/profile/bin/clickhouse-client"
		clientCfg   = "/etc/clickhouse-client/operator.xml"
		runAsUser   = "clickhouse_operator"
		dbUser      = "clickhouse_operator"
	)
	escaped := strings.ReplaceAll(stmt, `'`, `'\''`)
	return strings.Join([]string{
		"sudo", "-u", runAsUser,
		clientPath,
		"--config-file", clientCfg,
		"--user", dbUser,
		"--database", database,
		"--query=" + "'" + escaped + "'",
	}, " ")
}

func validIdent(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '_':
		default:
			return false
		}
	}
	return true
}

func sortColsInPlace(cols []string) {
	// Tiny insertion sort — column counts are < 20 in practice,
	// avoiding a sort.Slice closure import.
	for i := 1; i < len(cols); i++ {
		j := i
		for j > 0 && cols[j-1] > cols[j] {
			cols[j-1], cols[j] = cols[j], cols[j-1]
			j--
		}
	}
}

// Compile-time assertion that *sshtun.Client satisfies Executor.
var _ Executor = (*sshtun.Client)(nil)
