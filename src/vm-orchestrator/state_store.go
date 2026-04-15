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
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

const defaultStateDBPath = "/var/lib/forge-metal/vm-orchestrator/state.db"

var (
	errLeaseExists        = errors.New("lease already exists")
	errLeaseNotFound      = errors.New("lease not found")
	errLeaseStateConflict = errors.New("lease state conflict")
	errExecExists         = errors.New("exec already exists")
	errExecNotFound       = errors.New("exec not found")
	hostStateWriteMu      sync.Mutex
)

type hostStateStore struct {
	db *sql.DB
}

type leaseSnapshot struct {
	LeaseID        string
	State          LeaseState
	Spec           LeaseSpec
	RuntimeProfile string
	TrustClass     string
	VMIP           string
	Allowlist      []string
	AcquiredAt     time.Time
	ReadyAt        time.Time
	ExpiresAt      time.Time
	TerminalAt     time.Time
	TerminalReason string
}

type execSnapshot struct {
	LeaseID                string
	ExecID                 string
	State                  ExecState
	Spec                   ExecSpec
	ExitCode               int
	TerminalReason         string
	Output                 string
	QueuedAt               time.Time
	StartedAt              time.Time
	FirstByteAt            time.Time
	ExitedAt               time.Time
	StdoutBytes            uint64
	StderrBytes            uint64
	DroppedLogBytes        uint64
	ZFSWritten             uint64
	RootfsProvisionedBytes uint64
	Metrics                *VMMetrics
}

type leaseEventRecord struct {
	Seq       uint64
	Type      LeaseEventType
	ExecID    string
	Attrs     map[string]string
	CreatedAt time.Time
}

func openHostStateStore(path string, _ *slog.Logger) (*hostStateStore, error) {
	db, err := openStateDB(normalizeStateDBPath(path))
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
	for _, pragma := range []string{
		"PRAGMA busy_timeout=5000",
		"PRAGMA foreign_keys=ON",
		"PRAGMA synchronous=NORMAL",
	} {
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
	if err := s.ensureNetworkSlotShape(ctx); err != nil {
		return err
	}

	ddl := []string{
		`CREATE TABLE IF NOT EXISTS leases (
			lease_id TEXT PRIMARY KEY,
			state TEXT NOT NULL,
			spec_json TEXT NOT NULL,
			runtime_profile TEXT NOT NULL,
			trust_class TEXT NOT NULL,
			vm_ip TEXT NOT NULL DEFAULT '',
			checkpoint_save_allowlist_json TEXT NOT NULL DEFAULT '[]',
			acquired_at_unix_nano INTEGER NOT NULL,
			ready_at_unix_nano INTEGER NOT NULL DEFAULT 0,
			expires_at_unix_nano INTEGER NOT NULL,
			terminal_at_unix_nano INTEGER NOT NULL DEFAULT 0,
			terminal_reason TEXT NOT NULL DEFAULT '',
			updated_at_unix_nano INTEGER NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_leases_state ON leases(state)`,
		`CREATE INDEX IF NOT EXISTS idx_leases_expires ON leases(expires_at_unix_nano)`,
		`CREATE TABLE IF NOT EXISTS execs (
			lease_id TEXT NOT NULL,
			exec_id TEXT NOT NULL,
			state TEXT NOT NULL,
			spec_json TEXT NOT NULL,
			exit_code INTEGER NOT NULL DEFAULT 0,
			terminal_reason TEXT NOT NULL DEFAULT '',
			output TEXT NOT NULL DEFAULT '',
			queued_at_unix_nano INTEGER NOT NULL,
			started_at_unix_nano INTEGER NOT NULL DEFAULT 0,
			first_byte_at_unix_nano INTEGER NOT NULL DEFAULT 0,
			exited_at_unix_nano INTEGER NOT NULL DEFAULT 0,
			stdout_bytes INTEGER NOT NULL DEFAULT 0,
			stderr_bytes INTEGER NOT NULL DEFAULT 0,
			dropped_log_bytes INTEGER NOT NULL DEFAULT 0,
			zfs_written INTEGER NOT NULL DEFAULT 0,
			rootfs_provisioned_bytes INTEGER NOT NULL DEFAULT 0,
			metrics_json TEXT NOT NULL DEFAULT '',
			updated_at_unix_nano INTEGER NOT NULL,
			PRIMARY KEY (lease_id, exec_id),
			FOREIGN KEY (lease_id) REFERENCES leases(lease_id) ON DELETE CASCADE
		)`,
		`CREATE INDEX IF NOT EXISTS idx_execs_state ON execs(state)`,
		`CREATE TABLE IF NOT EXISTS lease_events (
			lease_id TEXT NOT NULL,
			event_seq INTEGER NOT NULL,
			event_type TEXT NOT NULL,
			exec_id TEXT NOT NULL DEFAULT '',
			payload_json TEXT NOT NULL,
			created_at_unix_nano INTEGER NOT NULL,
			PRIMARY KEY (lease_id, event_seq),
			FOREIGN KEY (lease_id) REFERENCES leases(lease_id) ON DELETE CASCADE
		)`,
		`CREATE TABLE IF NOT EXISTS idempotency_keys (
			scope TEXT NOT NULL,
			idempotency_key TEXT NOT NULL,
			response_json TEXT NOT NULL,
			created_at_unix_nano INTEGER NOT NULL,
			PRIMARY KEY (scope, idempotency_key)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_idempotency_created ON idempotency_keys(created_at_unix_nano)`,
		`CREATE TABLE IF NOT EXISTS network_slots (
			slot_index INTEGER PRIMARY KEY,
			generation INTEGER NOT NULL DEFAULT 0,
			state TEXT NOT NULL DEFAULT 'free',
			lease_id TEXT NOT NULL DEFAULT '',
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
		`CREATE INDEX IF NOT EXISTS idx_network_slots_lease_id ON network_slots(lease_id)`,
	}
	for _, stmt := range ddl {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("apply state db schema: %w", err)
		}
	}
	return nil
}

func (s *hostStateStore) ensureNetworkSlotShape(ctx context.Context) error {
	rows, err := s.db.QueryContext(ctx, `PRAGMA table_info(network_slots)`)
	if err != nil {
		return fmt.Errorf("inspect network_slots schema: %w", err)
	}
	defer rows.Close()

	foundLeaseID := false
	for rows.Next() {
		var cid int
		var name, typ string
		var notnull int
		var dflt any
		var pk int
		if err := rows.Scan(&cid, &name, &typ, &notnull, &dflt, &pk); err != nil {
			return fmt.Errorf("scan network_slots schema: %w", err)
		}
		switch name {
		case "lease_id":
			foundLeaseID = true
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate network_slots schema: %w", err)
	}
	if !foundLeaseID {
		if _, err := s.db.ExecContext(ctx, `DROP TABLE IF EXISTS network_slots`); err != nil {
			return fmt.Errorf("drop incompatible network_slots table: %w", err)
		}
	}
	return nil
}

func (s *hostStateStore) createLease(ctx context.Context, snapshot leaseSnapshot) error {
	specJSON, err := json.Marshal(snapshot.Spec)
	if err != nil {
		return fmt.Errorf("marshal lease spec: %w", err)
	}
	allowJSON, err := json.Marshal(snapshot.Allowlist)
	if err != nil {
		return fmt.Errorf("marshal checkpoint allowlist: %w", err)
	}
	hostStateWriteMu.Lock()
	defer hostStateWriteMu.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin create lease tx: %w", err)
	}
	defer rollbackTx(tx)

	now := time.Now().UTC()
	_, err = tx.ExecContext(ctx, `INSERT INTO leases (
		lease_id, state, spec_json, runtime_profile, trust_class, vm_ip,
		checkpoint_save_allowlist_json, acquired_at_unix_nano, expires_at_unix_nano, updated_at_unix_nano
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		snapshot.LeaseID,
		leaseStateName(snapshot.State),
		string(specJSON),
		snapshot.RuntimeProfile,
		snapshot.TrustClass,
		snapshot.VMIP,
		string(allowJSON),
		snapshot.AcquiredAt.UnixNano(),
		snapshot.ExpiresAt.UnixNano(),
		now.UnixNano(),
	)
	if err != nil {
		if isUniqueConstraint(err) {
			return errLeaseExists
		}
		return fmt.Errorf("insert lease %s: %w", snapshot.LeaseID, err)
	}
	if err := insertLeaseEventTx(ctx, tx, snapshot.LeaseID, LeaseEventLeaseAcquired, "", map[string]string{
		"runtime_profile": snapshot.RuntimeProfile,
		"trust_class":     snapshot.TrustClass,
		"expires_at":      snapshot.ExpiresAt.Format(time.RFC3339Nano),
	}, now.UnixNano()); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit create lease %s: %w", snapshot.LeaseID, err)
	}
	return nil
}

func (s *hostStateStore) setLeaseReady(ctx context.Context, leaseID, vmIP string, readyAt time.Time) error {
	hostStateWriteMu.Lock()
	defer hostStateWriteMu.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin lease ready tx: %w", err)
	}
	defer rollbackTx(tx)
	res, err := tx.ExecContext(ctx, `UPDATE leases SET state = ?, vm_ip = ?, ready_at_unix_nano = ?, updated_at_unix_nano = ? WHERE lease_id = ? AND state = ?`,
		leaseStateName(LeaseStateReady), vmIP, readyAt.UnixNano(), readyAt.UnixNano(), leaseID, leaseStateName(LeaseStateAcquiring))
	if err != nil {
		return fmt.Errorf("mark lease ready %s: %w", leaseID, err)
	}
	if rows, _ := res.RowsAffected(); rows == 0 {
		return errLeaseStateConflict
	}
	if err := insertLeaseEventTx(ctx, tx, leaseID, LeaseEventVMReady, "", map[string]string{
		"vm_ip": vmIP,
	}, readyAt.UnixNano()); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit lease ready %s: %w", leaseID, err)
	}
	return nil
}

func (s *hostStateStore) renewLease(ctx context.Context, leaseID string, expiresAt time.Time, allowlist []string) error {
	allowJSON, err := json.Marshal(allowlist)
	if err != nil {
		return fmt.Errorf("marshal checkpoint allowlist: %w", err)
	}
	hostStateWriteMu.Lock()
	defer hostStateWriteMu.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin renew lease tx: %w", err)
	}
	defer rollbackTx(tx)
	now := time.Now().UTC()
	res, err := tx.ExecContext(ctx, `UPDATE leases SET expires_at_unix_nano = ?, checkpoint_save_allowlist_json = ?, updated_at_unix_nano = ? WHERE lease_id = ? AND state IN (?, ?)`,
		expiresAt.UnixNano(), string(allowJSON), now.UnixNano(), leaseID, leaseStateName(LeaseStateAcquiring), leaseStateName(LeaseStateReady))
	if err != nil {
		return fmt.Errorf("renew lease %s: %w", leaseID, err)
	}
	if rows, _ := res.RowsAffected(); rows == 0 {
		return errLeaseStateConflict
	}
	if err := insertLeaseEventTx(ctx, tx, leaseID, LeaseEventLeaseRenewed, "", map[string]string{
		"expires_at": expiresAt.Format(time.RFC3339Nano),
	}, now.UnixNano()); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit renew lease %s: %w", leaseID, err)
	}
	return nil
}

func (s *hostStateStore) finishLease(ctx context.Context, leaseID string, state LeaseState, reason string, eventType LeaseEventType) error {
	hostStateWriteMu.Lock()
	defer hostStateWriteMu.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin finish lease tx: %w", err)
	}
	defer rollbackTx(tx)
	now := time.Now().UTC()
	res, err := tx.ExecContext(ctx, `UPDATE leases SET state = ?, terminal_reason = ?, terminal_at_unix_nano = ?, updated_at_unix_nano = ? WHERE lease_id = ? AND state NOT IN (?, ?, ?)`,
		leaseStateName(state), reason, now.UnixNano(), now.UnixNano(), leaseID,
		leaseStateName(LeaseStateReleased), leaseStateName(LeaseStateExpired), leaseStateName(LeaseStateCrashed))
	if err != nil {
		return fmt.Errorf("finish lease %s: %w", leaseID, err)
	}
	if rows, _ := res.RowsAffected(); rows == 0 {
		return nil
	}
	if err := insertLeaseEventTx(ctx, tx, leaseID, eventType, "", map[string]string{"reason": reason}, now.UnixNano()); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit finish lease %s: %w", leaseID, err)
	}
	return nil
}

func (s *hostStateStore) getLease(ctx context.Context, leaseID string) (leaseSnapshot, error) {
	row := s.db.QueryRowContext(ctx, `SELECT state, spec_json, runtime_profile, trust_class, vm_ip, checkpoint_save_allowlist_json, acquired_at_unix_nano, ready_at_unix_nano, expires_at_unix_nano, terminal_at_unix_nano, terminal_reason FROM leases WHERE lease_id = ?`, leaseID)
	return scanLeaseSnapshot(row, leaseID)
}

func (s *hostStateStore) listLeases(ctx context.Context, includeTerminal bool, limit int) ([]leaseSnapshot, error) {
	if limit <= 0 || limit > 1000 {
		limit = 1000
	}
	query := `SELECT lease_id, state, spec_json, runtime_profile, trust_class, vm_ip, checkpoint_save_allowlist_json, acquired_at_unix_nano, ready_at_unix_nano, expires_at_unix_nano, terminal_at_unix_nano, terminal_reason FROM leases`
	if !includeTerminal {
		query += ` WHERE state NOT IN ('released', 'expired', 'crashed')`
	}
	query += ` ORDER BY acquired_at_unix_nano DESC LIMIT ?`
	rows, err := s.db.QueryContext(ctx, query, limit)
	if err != nil {
		return nil, fmt.Errorf("query leases: %w", err)
	}
	defer rows.Close()
	out := make([]leaseSnapshot, 0, limit)
	for rows.Next() {
		var (
			leaseID, stateName, specJSON, runtimeProfile, trustClass, vmIP, allowJSON, terminalReason string
			acquiredNS, readyNS, expiresNS, terminalNS                                                int64
		)
		if err := rows.Scan(&leaseID, &stateName, &specJSON, &runtimeProfile, &trustClass, &vmIP, &allowJSON, &acquiredNS, &readyNS, &expiresNS, &terminalNS, &terminalReason); err != nil {
			return nil, fmt.Errorf("scan lease row: %w", err)
		}
		snap, err := decodeLeaseSnapshot(leaseID, stateName, specJSON, runtimeProfile, trustClass, vmIP, allowJSON, acquiredNS, readyNS, expiresNS, terminalNS, terminalReason)
		if err != nil {
			return nil, err
		}
		out = append(out, snap)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate leases: %w", err)
	}
	return out, nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanLeaseSnapshot(row rowScanner, leaseID string) (leaseSnapshot, error) {
	var (
		stateName, specJSON, runtimeProfile, trustClass, vmIP, allowJSON, terminalReason string
		acquiredNS, readyNS, expiresNS, terminalNS                                       int64
	)
	if err := row.Scan(&stateName, &specJSON, &runtimeProfile, &trustClass, &vmIP, &allowJSON, &acquiredNS, &readyNS, &expiresNS, &terminalNS, &terminalReason); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return leaseSnapshot{}, errLeaseNotFound
		}
		return leaseSnapshot{}, fmt.Errorf("scan lease %s: %w", leaseID, err)
	}
	return decodeLeaseSnapshot(leaseID, stateName, specJSON, runtimeProfile, trustClass, vmIP, allowJSON, acquiredNS, readyNS, expiresNS, terminalNS, terminalReason)
}

func decodeLeaseSnapshot(leaseID, stateName, specJSON, runtimeProfile, trustClass, vmIP, allowJSON string, acquiredNS, readyNS, expiresNS, terminalNS int64, terminalReason string) (leaseSnapshot, error) {
	state, err := parseLeaseState(stateName)
	if err != nil {
		return leaseSnapshot{}, err
	}
	spec := LeaseSpec{}
	if err := json.Unmarshal([]byte(specJSON), &spec); err != nil {
		return leaseSnapshot{}, fmt.Errorf("decode lease spec %s: %w", leaseID, err)
	}
	allowlist := []string{}
	if strings.TrimSpace(allowJSON) != "" {
		if err := json.Unmarshal([]byte(allowJSON), &allowlist); err != nil {
			return leaseSnapshot{}, fmt.Errorf("decode checkpoint allowlist %s: %w", leaseID, err)
		}
	}
	return leaseSnapshot{
		LeaseID:        leaseID,
		State:          state,
		Spec:           spec,
		RuntimeProfile: runtimeProfile,
		TrustClass:     trustClass,
		VMIP:           vmIP,
		Allowlist:      allowlist,
		AcquiredAt:     unixNanoTime(acquiredNS),
		ReadyAt:        unixNanoTime(readyNS),
		ExpiresAt:      unixNanoTime(expiresNS),
		TerminalAt:     unixNanoTime(terminalNS),
		TerminalReason: terminalReason,
	}, nil
}

func (s *hostStateStore) createExec(ctx context.Context, snap execSnapshot) error {
	specJSON, err := json.Marshal(snap.Spec)
	if err != nil {
		return fmt.Errorf("marshal exec spec: %w", err)
	}
	hostStateWriteMu.Lock()
	defer hostStateWriteMu.Unlock()
	now := time.Now().UTC()
	_, err = s.db.ExecContext(ctx, `INSERT INTO execs (lease_id, exec_id, state, spec_json, queued_at_unix_nano, updated_at_unix_nano) VALUES (?, ?, ?, ?, ?, ?)`,
		snap.LeaseID, snap.ExecID, execStateName(ExecStatePending), string(specJSON), now.UnixNano(), now.UnixNano())
	if err != nil {
		if isUniqueConstraint(err) {
			return errExecExists
		}
		return fmt.Errorf("insert exec %s/%s: %w", snap.LeaseID, snap.ExecID, err)
	}
	return nil
}

func (s *hostStateStore) markExecStarted(ctx context.Context, leaseID, execID string, startedAt time.Time) error {
	hostStateWriteMu.Lock()
	defer hostStateWriteMu.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin exec started tx: %w", err)
	}
	defer rollbackTx(tx)
	res, err := tx.ExecContext(ctx, `UPDATE execs SET state = ?, started_at_unix_nano = ?, updated_at_unix_nano = ? WHERE lease_id = ? AND exec_id = ? AND state = ?`,
		execStateName(ExecStateRunning), startedAt.UnixNano(), startedAt.UnixNano(), leaseID, execID, execStateName(ExecStatePending))
	if err != nil {
		return fmt.Errorf("mark exec started %s/%s: %w", leaseID, execID, err)
	}
	if rows, _ := res.RowsAffected(); rows == 0 {
		return errExecNotFound
	}
	if err := insertLeaseEventTx(ctx, tx, leaseID, LeaseEventExecStarted, execID, map[string]string{
		"started_at": startedAt.UTC().Format(time.RFC3339Nano),
	}, startedAt.UnixNano()); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit exec started %s/%s: %w", leaseID, execID, err)
	}
	return nil
}

func (s *hostStateStore) finishExec(ctx context.Context, snap execSnapshot) error {
	metricsJSON := ""
	if snap.Metrics != nil {
		payload, err := json.Marshal(snap.Metrics)
		if err != nil {
			return fmt.Errorf("marshal exec metrics: %w", err)
		}
		metricsJSON = string(payload)
	}
	hostStateWriteMu.Lock()
	defer hostStateWriteMu.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin finish exec tx: %w", err)
	}
	defer rollbackTx(tx)
	now := time.Now().UTC()
	res, err := tx.ExecContext(ctx, `UPDATE execs SET state = ?, exit_code = ?, terminal_reason = ?, output = ?, first_byte_at_unix_nano = ?, exited_at_unix_nano = ?, stdout_bytes = ?, stderr_bytes = ?, dropped_log_bytes = ?, zfs_written = ?, rootfs_provisioned_bytes = ?, metrics_json = ?, updated_at_unix_nano = ? WHERE lease_id = ? AND exec_id = ?`,
		execStateName(snap.State), snap.ExitCode, snap.TerminalReason, snap.Output,
		snap.FirstByteAt.UnixNano(), snap.ExitedAt.UnixNano(),
		snap.StdoutBytes, snap.StderrBytes, snap.DroppedLogBytes,
		snap.ZFSWritten, snap.RootfsProvisionedBytes, metricsJSON, now.UnixNano(),
		snap.LeaseID, snap.ExecID)
	if err != nil {
		return fmt.Errorf("finish exec %s/%s: %w", snap.LeaseID, snap.ExecID, err)
	}
	if rows, _ := res.RowsAffected(); rows == 0 {
		return errExecNotFound
	}
	eventType := LeaseEventExecFinished
	if snap.State == ExecStateCanceled {
		eventType = LeaseEventExecCanceled
	}
	if err := insertLeaseEventTx(ctx, tx, snap.LeaseID, eventType, snap.ExecID, map[string]string{
		"exit_code": fmt.Sprintf("%d", snap.ExitCode),
		"state":     execStateName(snap.State),
		"reason":    snap.TerminalReason,
	}, now.UnixNano()); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit finish exec %s/%s: %w", snap.LeaseID, snap.ExecID, err)
	}
	return nil
}

func (s *hostStateStore) getExec(ctx context.Context, leaseID, execID string) (execSnapshot, error) {
	row := s.db.QueryRowContext(ctx, `SELECT state, spec_json, exit_code, terminal_reason, output, queued_at_unix_nano, started_at_unix_nano, first_byte_at_unix_nano, exited_at_unix_nano, stdout_bytes, stderr_bytes, dropped_log_bytes, zfs_written, rootfs_provisioned_bytes, metrics_json FROM execs WHERE lease_id = ? AND exec_id = ?`, leaseID, execID)
	var (
		stateName, specJSON, terminalReason, output, metricsJSON string
		exitCode                                                 int
		queuedNS, startedNS, firstByteNS, exitedNS               int64
		stdoutBytes, stderrBytes, droppedBytes                   uint64
		zfsWritten, rootfsProvisioned                            uint64
	)
	if err := row.Scan(&stateName, &specJSON, &exitCode, &terminalReason, &output, &queuedNS, &startedNS, &firstByteNS, &exitedNS, &stdoutBytes, &stderrBytes, &droppedBytes, &zfsWritten, &rootfsProvisioned, &metricsJSON); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return execSnapshot{}, errExecNotFound
		}
		return execSnapshot{}, fmt.Errorf("query exec %s/%s: %w", leaseID, execID, err)
	}
	state, err := parseExecState(stateName)
	if err != nil {
		return execSnapshot{}, err
	}
	spec := ExecSpec{}
	if err := json.Unmarshal([]byte(specJSON), &spec); err != nil {
		return execSnapshot{}, fmt.Errorf("decode exec spec %s/%s: %w", leaseID, execID, err)
	}
	var metrics *VMMetrics
	if strings.TrimSpace(metricsJSON) != "" {
		decoded := VMMetrics{}
		if err := json.Unmarshal([]byte(metricsJSON), &decoded); err != nil {
			return execSnapshot{}, fmt.Errorf("decode exec metrics %s/%s: %w", leaseID, execID, err)
		}
		metrics = &decoded
	}
	return execSnapshot{
		LeaseID:                leaseID,
		ExecID:                 execID,
		State:                  state,
		Spec:                   spec,
		ExitCode:               exitCode,
		TerminalReason:         terminalReason,
		Output:                 output,
		QueuedAt:               unixNanoTime(queuedNS),
		StartedAt:              unixNanoTime(startedNS),
		FirstByteAt:            unixNanoTime(firstByteNS),
		ExitedAt:               unixNanoTime(exitedNS),
		StdoutBytes:            stdoutBytes,
		StderrBytes:            stderrBytes,
		DroppedLogBytes:        droppedBytes,
		ZFSWritten:             zfsWritten,
		RootfsProvisionedBytes: rootfsProvisioned,
		Metrics:                metrics,
	}, nil
}

func (s *hostStateStore) appendLeaseEvent(ctx context.Context, leaseID string, eventType LeaseEventType, execID string, attrs map[string]string) error {
	hostStateWriteMu.Lock()
	defer hostStateWriteMu.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin append lease event tx: %w", err)
	}
	defer rollbackTx(tx)
	if err := insertLeaseEventTx(ctx, tx, leaseID, eventType, execID, attrs, time.Now().UTC().UnixNano()); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit append lease event %s: %w", leaseID, err)
	}
	return nil
}

func (s *hostStateStore) listLeaseEvents(ctx context.Context, leaseID string, fromSeq uint64, limit int) ([]leaseEventRecord, error) {
	if limit <= 0 || limit > 1000 {
		limit = 256
	}
	rows, err := s.db.QueryContext(ctx, `SELECT event_seq, event_type, exec_id, payload_json, created_at_unix_nano FROM lease_events WHERE lease_id = ? AND event_seq > ? ORDER BY event_seq ASC LIMIT ?`, leaseID, fromSeq, limit)
	if err != nil {
		return nil, fmt.Errorf("query lease events %s: %w", leaseID, err)
	}
	defer rows.Close()
	out := make([]leaseEventRecord, 0, limit)
	for rows.Next() {
		var seq uint64
		var typeName, execID, payloadJSON string
		var createdNS int64
		if err := rows.Scan(&seq, &typeName, &execID, &payloadJSON, &createdNS); err != nil {
			return nil, fmt.Errorf("scan lease event %s: %w", leaseID, err)
		}
		attrs := map[string]string{}
		if payloadJSON != "" {
			if err := json.Unmarshal([]byte(payloadJSON), &attrs); err != nil {
				return nil, fmt.Errorf("decode lease event %s seq=%d: %w", leaseID, seq, err)
			}
		}
		out = append(out, leaseEventRecord{Seq: seq, Type: LeaseEventType(typeName), ExecID: execID, Attrs: attrs, CreatedAt: unixNanoTime(createdNS)})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate lease events %s: %w", leaseID, err)
	}
	return out, nil
}

func insertLeaseEventTx(ctx context.Context, tx *sql.Tx, leaseID string, eventType LeaseEventType, execID string, attrs map[string]string, createdNS int64) error {
	var nextSeq uint64
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(event_seq), 0) + 1 FROM lease_events WHERE lease_id = ?`, leaseID).Scan(&nextSeq); err != nil {
		return fmt.Errorf("compute lease event seq %s: %w", leaseID, err)
	}
	if attrs == nil {
		attrs = map[string]string{}
	}
	payload, err := json.Marshal(attrs)
	if err != nil {
		return fmt.Errorf("marshal lease event payload %s: %w", leaseID, err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO lease_events (lease_id, event_seq, event_type, exec_id, payload_json, created_at_unix_nano) VALUES (?, ?, ?, ?, ?, ?)`,
		leaseID, nextSeq, string(eventType), execID, string(payload), createdNS); err != nil {
		return fmt.Errorf("insert lease event %s: %w", leaseID, err)
	}
	return nil
}

func (s *hostStateStore) countActiveLeases(ctx context.Context) (int, error) {
	var count int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM leases WHERE state IN (?, ?)`, leaseStateName(LeaseStateAcquiring), leaseStateName(LeaseStateReady)).Scan(&count); err != nil {
		return 0, fmt.Errorf("count active leases: %w", err)
	}
	return count, nil
}

func (s *hostStateStore) getIdempotency(ctx context.Context, scope, key string) (string, bool, error) {
	if strings.TrimSpace(key) == "" {
		return "", false, nil
	}
	var response string
	if err := s.db.QueryRowContext(ctx, `SELECT response_json FROM idempotency_keys WHERE scope = ? AND idempotency_key = ?`, scope, key).Scan(&response); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", false, nil
		}
		return "", false, fmt.Errorf("query idempotency %s: %w", scope, err)
	}
	return response, true, nil
}

func (s *hostStateStore) putIdempotency(ctx context.Context, scope, key, response string) error {
	if strings.TrimSpace(key) == "" {
		return nil
	}
	hostStateWriteMu.Lock()
	defer hostStateWriteMu.Unlock()
	now := time.Now().UTC().UnixNano()
	_, err := s.db.ExecContext(ctx, `INSERT INTO idempotency_keys (scope, idempotency_key, response_json, created_at_unix_nano) VALUES (?, ?, ?, ?) ON CONFLICT(scope, idempotency_key) DO NOTHING`, scope, key, response, now)
	if err != nil {
		return fmt.Errorf("store idempotency %s: %w", scope, err)
	}
	return nil
}

func leaseStateName(state LeaseState) string {
	switch state {
	case LeaseStateAcquiring:
		return "acquiring"
	case LeaseStateReady:
		return "ready"
	case LeaseStateDraining:
		return "draining"
	case LeaseStateReleased:
		return "released"
	case LeaseStateExpired:
		return "expired"
	case LeaseStateCrashed:
		return "crashed"
	default:
		return "unspecified"
	}
}

func parseLeaseState(name string) (LeaseState, error) {
	switch strings.TrimSpace(name) {
	case "acquiring":
		return LeaseStateAcquiring, nil
	case "ready":
		return LeaseStateReady, nil
	case "draining":
		return LeaseStateDraining, nil
	case "released":
		return LeaseStateReleased, nil
	case "expired":
		return LeaseStateExpired, nil
	case "crashed":
		return LeaseStateCrashed, nil
	case "", "unspecified":
		return LeaseStateUnspecified, nil
	default:
		return LeaseStateUnspecified, fmt.Errorf("unknown lease state %q", name)
	}
}

func execStateName(state ExecState) string {
	switch state {
	case ExecStatePending:
		return "pending"
	case ExecStateRunning:
		return "running"
	case ExecStateExited:
		return "exited"
	case ExecStateFailed:
		return "failed"
	case ExecStateCanceled:
		return "canceled"
	case ExecStateKilledByLeaseExpiry:
		return "killed_by_lease_expiry"
	default:
		return "unspecified"
	}
}

func parseExecState(name string) (ExecState, error) {
	switch strings.TrimSpace(name) {
	case "pending":
		return ExecStatePending, nil
	case "running":
		return ExecStateRunning, nil
	case "exited":
		return ExecStateExited, nil
	case "failed":
		return ExecStateFailed, nil
	case "canceled":
		return ExecStateCanceled, nil
	case "killed_by_lease_expiry":
		return ExecStateKilledByLeaseExpiry, nil
	case "", "unspecified":
		return ExecStateUnspecified, nil
	default:
		return ExecStateUnspecified, fmt.Errorf("unknown exec state %q", name)
	}
}

func unixNanoTime(value int64) time.Time {
	if value <= 0 {
		return time.Time{}
	}
	return time.Unix(0, value).UTC()
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
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed")
}
