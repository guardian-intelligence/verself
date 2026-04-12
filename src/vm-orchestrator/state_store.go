package vmorchestrator

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

const defaultStateDBPath = "/var/lib/forge-metal/vm-orchestrator/state.db"

var (
	errHostRunExists        = errors.New("host run already exists")
	errHostRunNotFound      = errors.New("host run not found")
	errHostRunStateConflict = errors.New("host run state conflict")
)

type hostRunSnapshot struct {
	RunID          string
	State          RunState
	TerminalReason string
	ExitCode       int
	ErrorMessage   string
	Result         *RunResult
	UpdatedAt      time.Time
}

type hostRunEvent struct {
	Seq       uint64
	EventType string
	Attrs     map[string]string
	CreatedAt time.Time
}

type hostStateStore struct {
	db *sql.DB
}

func openHostStateStore(path string, _ *slog.Logger) (*hostStateStore, error) {
	path = normalizeStateDBPath(path)
	db, err := openStateDB(path)
	if err != nil {
		return nil, err
	}
	store := &hostStateStore{db: db}
	if err := store.ensureSchema(context.Background()); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func normalizeStateDBPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return defaultStateDBPath
	}
	return path
}

func openStateDB(path string) (*sql.DB, error) {
	if path != ":memory:" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return nil, fmt.Errorf("mkdir state db dir for %s: %w", path, err)
		}
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open state db %s: %w", path, err)
	}
	db.SetConnMaxLifetime(0)
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	pragmas := []string{
		"PRAGMA busy_timeout=5000",
		"PRAGMA foreign_keys=ON",
		"PRAGMA synchronous=NORMAL",
	}
	for _, pragma := range pragmas {
		if _, err := db.Exec(pragma); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("set sqlite pragma %q: %w", pragma, err)
		}
	}
	if path != ":memory:" {
		if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil && !strings.Contains(err.Error(), "database is locked") {
			_ = db.Close()
			return nil, fmt.Errorf("set sqlite pragma %q: %w", "PRAGMA journal_mode=WAL", err)
		}
	}

	return db, nil
}

func (s *hostStateStore) close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *hostStateStore) ensureSchema(ctx context.Context) error {
	ddl := []string{
		`CREATE TABLE IF NOT EXISTS host_runs (
			run_id TEXT PRIMARY KEY,
			state TEXT NOT NULL,
			result_json TEXT NOT NULL DEFAULT '',
			terminal_reason TEXT NOT NULL DEFAULT '',
			exit_code INTEGER NOT NULL DEFAULT 0,
			created_at_unix_nano INTEGER NOT NULL,
			updated_at_unix_nano INTEGER NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_host_runs_state ON host_runs(state)`,
		`CREATE TABLE IF NOT EXISTS host_run_events (
			run_id TEXT NOT NULL,
			event_seq INTEGER NOT NULL,
			event_type TEXT NOT NULL,
			payload_json TEXT NOT NULL,
			created_at_unix_nano INTEGER NOT NULL,
			PRIMARY KEY (run_id, event_seq)
		)`,
		`CREATE TABLE IF NOT EXISTS network_slots (
			slot_index INTEGER PRIMARY KEY,
			generation INTEGER NOT NULL DEFAULT 0,
			state TEXT NOT NULL DEFAULT 'free',
			run_id TEXT NOT NULL DEFAULT '',
			tap_name TEXT NOT NULL DEFAULT '',
			subnet_cidr TEXT NOT NULL DEFAULT '',
			host_cidr TEXT NOT NULL DEFAULT '',
			guest_ip TEXT NOT NULL DEFAULT '',
			gateway_ip TEXT NOT NULL DEFAULT '',
			mac TEXT NOT NULL DEFAULT '',
			firecracker_pid INTEGER NOT NULL DEFAULT 0,
			firecracker_start_ticks INTEGER NOT NULL DEFAULT 0,
			created_at_unix_nano INTEGER NOT NULL DEFAULT 0,
			updated_at_unix_nano INTEGER NOT NULL DEFAULT 0
		)`,
		`CREATE INDEX IF NOT EXISTS idx_network_slots_state ON network_slots(state)`,
		`CREATE INDEX IF NOT EXISTS idx_network_slots_run_id ON network_slots(run_id)`,
		`CREATE TABLE IF NOT EXISTS checkpoint_ops (
			run_id TEXT NOT NULL,
			op_seq INTEGER NOT NULL,
			request_id TEXT NOT NULL,
			op_type TEXT NOT NULL,
			ref TEXT NOT NULL,
			accepted INTEGER NOT NULL,
			version_id TEXT NOT NULL DEFAULT '',
			error_text TEXT NOT NULL DEFAULT '',
			created_at_unix_nano INTEGER NOT NULL,
			PRIMARY KEY (run_id, op_seq)
		)`,
	}

	for _, stmt := range ddl {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("apply state db schema: %w", err)
		}
	}
	return nil
}

func (s *hostStateStore) createRun(ctx context.Context, runID string, initialState RunState, attrs map[string]string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin create run tx: %w", err)
	}
	defer rollbackTx(tx)

	nowUnixNano := time.Now().UTC().UnixNano()
	if _, err := tx.ExecContext(
		ctx,
		`INSERT INTO host_runs (run_id, state, created_at_unix_nano, updated_at_unix_nano) VALUES (?, ?, ?, ?)`,
		runID,
		runStateName(initialState),
		nowUnixNano,
		nowUnixNano,
	); err != nil {
		if isUniqueConstraint(err) {
			return errHostRunExists
		}
		return fmt.Errorf("insert host run %s: %w", runID, err)
	}

	if err := insertHostRunEventTx(ctx, tx, runID, "run_accepted", attrs, nowUnixNano); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit create run tx: %w", err)
	}
	return nil
}

func (s *hostStateStore) transitionRunState(
	ctx context.Context,
	runID string,
	expected []RunState,
	next RunState,
	eventType string,
	attrs map[string]string,
	terminalReason string,
	result *RunResult,
) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transition run tx: %w", err)
	}
	defer rollbackTx(tx)

	nowUnixNano := time.Now().UTC().UnixNano()
	resultJSON := ""
	exitCode := 0
	if result != nil {
		payload, marshalErr := json.Marshal(result)
		if marshalErr != nil {
			return fmt.Errorf("marshal run result for %s: %w", runID, marshalErr)
		}
		resultJSON = string(payload)
		exitCode = result.ExitCode
	}

	query := `UPDATE host_runs SET state = ?, result_json = ?, terminal_reason = ?, exit_code = ?, updated_at_unix_nano = ? WHERE run_id = ?`
	args := []any{runStateName(next), resultJSON, terminalReason, exitCode, nowUnixNano, runID}
	if len(expected) > 0 {
		query += ` AND state IN (` + sqlPlaceholders(len(expected)) + `)`
		for _, state := range expected {
			args = append(args, runStateName(state))
		}
	}

	updateRes, err := tx.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("update run state %s -> %s for %s: %w", strings.Join(runStateNames(expected), ","), runStateName(next), runID, err)
	}
	rows, err := updateRes.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected for run state transition %s: %w", runID, err)
	}
	if rows == 0 {
		var current string
		rowErr := tx.QueryRowContext(ctx, `SELECT state FROM host_runs WHERE run_id = ?`, runID).Scan(&current)
		switch {
		case errors.Is(rowErr, sql.ErrNoRows):
			return errHostRunNotFound
		case rowErr != nil:
			return fmt.Errorf("load run state after transition miss for %s: %w", runID, rowErr)
		default:
			return fmt.Errorf("%w: run %s currently in state %s", errHostRunStateConflict, runID, current)
		}
	}

	if err := insertHostRunEventTx(ctx, tx, runID, eventType, attrs, nowUnixNano); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit run transition %s: %w", runID, err)
	}
	return nil
}

func (s *hostStateStore) appendRunEvent(ctx context.Context, runID, eventType string, attrs map[string]string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin append run event tx: %w", err)
	}
	defer rollbackTx(tx)

	nowUnixNano := time.Now().UTC().UnixNano()
	if err := insertHostRunEventTx(ctx, tx, runID, eventType, attrs, nowUnixNano); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit append run event for %s: %w", runID, err)
	}
	return nil
}

func (s *hostStateStore) getRunSnapshot(ctx context.Context, runID string) (hostRunSnapshot, error) {
	var (
		stateName       string
		resultJSON      string
		terminalReason  string
		exitCode        int
		updatedUnixNano int64
	)

	if err := s.db.QueryRowContext(
		ctx,
		`SELECT state, result_json, terminal_reason, exit_code, updated_at_unix_nano FROM host_runs WHERE run_id = ?`,
		runID,
	).Scan(&stateName, &resultJSON, &terminalReason, &exitCode, &updatedUnixNano); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return hostRunSnapshot{}, errHostRunNotFound
		}
		return hostRunSnapshot{}, fmt.Errorf("query host run snapshot %s: %w", runID, err)
	}

	state, err := parseRunState(stateName)
	if err != nil {
		return hostRunSnapshot{}, err
	}

	var result *RunResult
	if strings.TrimSpace(resultJSON) != "" {
		decoded := RunResult{}
		if err := json.Unmarshal([]byte(resultJSON), &decoded); err != nil {
			return hostRunSnapshot{}, fmt.Errorf("decode host run result %s: %w", runID, err)
		}
		result = &decoded
	}

	snapshot := hostRunSnapshot{
		RunID:          runID,
		State:          state,
		TerminalReason: terminalReason,
		ExitCode:       exitCode,
		ErrorMessage:   terminalReason,
		Result:         result,
		UpdatedAt:      time.Unix(0, updatedUnixNano).UTC(),
	}
	return snapshot, nil
}

func (s *hostStateStore) listRunEvents(ctx context.Context, runID string, fromSeq uint64, limit int) ([]hostRunEvent, error) {
	if limit <= 0 {
		limit = 100
	}

	rows, err := s.db.QueryContext(
		ctx,
		`SELECT event_seq, event_type, payload_json, created_at_unix_nano
		 FROM host_run_events
		 WHERE run_id = ? AND event_seq > ?
		 ORDER BY event_seq ASC
		 LIMIT ?`,
		runID,
		fromSeq,
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("query host run events for %s: %w", runID, err)
	}
	defer rows.Close()

	out := make([]hostRunEvent, 0, limit)
	for rows.Next() {
		var (
			seq           uint64
			eventType     string
			payloadJSON   string
			createdUnixNs int64
		)
		if err := rows.Scan(&seq, &eventType, &payloadJSON, &createdUnixNs); err != nil {
			return nil, fmt.Errorf("scan host run event for %s: %w", runID, err)
		}
		attrs := map[string]string{}
		if payloadJSON != "" {
			if err := json.Unmarshal([]byte(payloadJSON), &attrs); err != nil {
				return nil, fmt.Errorf("decode host run event payload for %s seq=%d: %w", runID, seq, err)
			}
		}
		out = append(out, hostRunEvent{
			Seq:       seq,
			EventType: eventType,
			Attrs:     attrs,
			CreatedAt: time.Unix(0, createdUnixNs).UTC(),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate host run events for %s: %w", runID, err)
	}
	return out, nil
}

func (s *hostStateStore) countActiveRuns(ctx context.Context) (int, error) {
	var count int
	if err := s.db.QueryRowContext(
		ctx,
		`SELECT COUNT(*) FROM host_runs WHERE state IN (?, ?)`,
		runStateName(RunStatePending),
		runStateName(RunStateRunning),
	).Scan(&count); err != nil {
		return 0, fmt.Errorf("count active host runs: %w", err)
	}
	return count, nil
}

func insertHostRunEventTx(
	ctx context.Context,
	tx *sql.Tx,
	runID, eventType string,
	attrs map[string]string,
	nowUnixNano int64,
) error {
	var nextSeq uint64
	if err := tx.QueryRowContext(
		ctx,
		`SELECT COALESCE(MAX(event_seq), 0) + 1 FROM host_run_events WHERE run_id = ?`,
		runID,
	).Scan(&nextSeq); err != nil {
		return fmt.Errorf("compute host run event seq for %s: %w", runID, err)
	}

	if attrs == nil {
		attrs = map[string]string{}
	}
	payload, err := json.Marshal(attrs)
	if err != nil {
		return fmt.Errorf("marshal host run event payload for %s: %w", runID, err)
	}

	if _, err := tx.ExecContext(
		ctx,
		`INSERT INTO host_run_events (run_id, event_seq, event_type, payload_json, created_at_unix_nano) VALUES (?, ?, ?, ?, ?)`,
		runID,
		nextSeq,
		eventType,
		string(payload),
		nowUnixNano,
	); err != nil {
		return fmt.Errorf("insert host run event for %s: %w", runID, err)
	}
	return nil
}

func runStateName(state RunState) string {
	switch state {
	case RunStatePending:
		return "pending"
	case RunStateRunning:
		return "running"
	case RunStateSucceeded:
		return "succeeded"
	case RunStateFailed:
		return "failed"
	case RunStateCanceled:
		return "canceled"
	default:
		return "unspecified"
	}
}

func runStateNames(states []RunState) []string {
	out := make([]string, 0, len(states))
	for _, state := range states {
		out = append(out, runStateName(state))
	}
	return out
}

func parseRunState(name string) (RunState, error) {
	switch strings.TrimSpace(name) {
	case "pending":
		return RunStatePending, nil
	case "running":
		return RunStateRunning, nil
	case "succeeded":
		return RunStateSucceeded, nil
	case "failed":
		return RunStateFailed, nil
	case "canceled":
		return RunStateCanceled, nil
	case "unspecified", "":
		return RunStateUnspecified, nil
	default:
		return RunStateUnspecified, fmt.Errorf("unknown persisted run state %q", name)
	}
}

func sqlPlaceholders(count int) string {
	if count <= 0 {
		return ""
	}
	parts := make([]string, count)
	for i := range count {
		parts[i] = "?"
	}
	return strings.Join(parts, ",")
}

func rollbackTx(tx *sql.Tx) {
	if tx != nil {
		_ = tx.Rollback()
	}
}

func isUniqueConstraint(err error) bool {
	return strings.Contains(err.Error(), "UNIQUE constraint failed")
}
