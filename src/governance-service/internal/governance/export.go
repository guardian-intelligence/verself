package governance

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/attribute"
)

var defaultScopes = []string{"identity", "billing", "sandbox", "audit"}

type CreateExportRequest struct {
	Scopes         []string
	IncludeLogs    bool
	IdempotencyKey string
}

type ExportFile struct {
	Path        string `json:"path"`
	ContentType string `json:"content_type"`
	Rows        int64  `json:"rows"`
	Bytes       int64  `json:"bytes"`
	SHA256      string `json:"sha256"`
}

type ExportJob struct {
	ExportID       uuid.UUID       `json:"export_id"`
	OrgID          string          `json:"org_id"`
	RequestedBy    string          `json:"requested_by"`
	Scopes         []string        `json:"scopes"`
	IncludeLogs    bool            `json:"include_logs"`
	Format         string          `json:"format"`
	State          string          `json:"state"`
	ArtifactPath   string          `json:"-"`
	ArtifactSHA256 string          `json:"artifact_sha256"`
	ArtifactBytes  int64           `json:"artifact_bytes"`
	Files          []ExportFile    `json:"files"`
	CreatedAt      time.Time       `json:"created_at"`
	UpdatedAt      time.Time       `json:"updated_at"`
	CompletedAt    *time.Time      `json:"completed_at,omitempty"`
	ExpiresAt      time.Time       `json:"expires_at"`
	ErrorCode      string          `json:"error_code,omitempty"`
	ErrorMessage   string          `json:"error_message,omitempty"`
	Manifest       json.RawMessage `json:"manifest,omitempty"`
}

type exportArtifactFile struct {
	meta ExportFile
	body []byte
}

func (s *Service) CreateExport(ctx context.Context, principal Principal, req CreateExportRequest) (*ExportJob, error) {
	ctx, span := tracer.Start(ctx, "governance.export.create")
	defer span.End()
	if err := s.Validate(); err != nil {
		return nil, err
	}
	orgID := strings.TrimSpace(principal.OrgID)
	if orgID == "" || strings.TrimSpace(principal.Subject) == "" {
		return nil, fmt.Errorf("%w: principal org and subject are required", ErrInvalidArgument)
	}
	idempotencyHash := hashText(req.IdempotencyKey)
	if idempotencyHash == "" {
		return nil, fmt.Errorf("%w: idempotency key is required", ErrInvalidArgument)
	}
	scopes, err := normalizeScopes(req.Scopes)
	if err != nil {
		return nil, err
	}
	if existing, err := s.exportByIdempotency(ctx, orgID, idempotencyHash); err == nil {
		return existing, nil
	} else if !errorsIsNotFound(err) {
		return nil, err
	}
	exportID := uuid.New()
	expiresAt := time.Now().UTC().Add(s.ExportTTL)
	if _, err := s.PG.Exec(ctx, `
		INSERT INTO governance_export_jobs (
			export_id, org_id, requested_by, idempotency_key_hash, scopes, include_logs,
			format, state, expires_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, 'tar.gz', 'running', $7)
	`, exportID, orgID, principal.Subject, idempotencyHash, scopes, req.IncludeLogs, expiresAt); err != nil {
		return nil, fmt.Errorf("%w: create export job: %v", ErrStore, err)
	}

	job, err := s.buildExportArtifact(ctx, principal, exportID, scopes, req.IncludeLogs, expiresAt)
	if err != nil {
		_, _ = s.PG.Exec(ctx, `
			UPDATE governance_export_jobs
			SET state = 'failed', error_code = 'export-build-failed', error_message = $2, updated_at = now()
			WHERE export_id = $1
		`, exportID, err.Error())
		return nil, err
	}
	span.SetAttributes(
		attribute.String("forge_metal.org_id", orgID),
		attribute.String("forge_metal.export_id", exportID.String()),
		attribute.Int("forge_metal.export_file_count", len(job.Files)),
		attribute.Int64("forge_metal.export_bytes", job.ArtifactBytes),
	)
	return job, nil
}

func (s *Service) buildExportArtifact(ctx context.Context, principal Principal, exportID uuid.UUID, scopes []string, includeLogs bool, expiresAt time.Time) (*ExportJob, error) {
	ctx, span := tracer.Start(ctx, "governance.export.build")
	defer span.End()
	artifactPath, err := s.exportPath(principal.OrgID, exportID.String())
	if err != nil {
		return nil, err
	}
	files, err := s.collectExportFiles(ctx, principal.OrgID, exportID, scopes, includeLogs, expiresAt)
	if err != nil {
		return nil, err
	}
	sort.Slice(files, func(i, j int) bool { return files[i].meta.Path < files[j].meta.Path })
	manifest := map[string]any{
		"export_id":    exportID.String(),
		"org_id":       principal.OrgID,
		"requested_by": principal.Subject,
		"created_at":   time.Now().UTC().Format(time.RFC3339Nano),
		"expires_at":   expiresAt.Format(time.RFC3339Nano),
		"format":       "tar.gz",
		"scopes":       scopes,
		"include_logs": includeLogs,
		"files":        exportFilesMetadata(files),
	}
	manifestBody, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("%w: marshal manifest: %v", ErrInvalidArgument, err)
	}
	files = append([]exportArtifactFile{newArtifactFile("manifest.json", "application/json", manifestBody, 1)}, files...)
	if err := writeTarGzip(artifactPath, files); err != nil {
		return nil, err
	}
	stat, err := os.Stat(artifactPath)
	if err != nil {
		return nil, fmt.Errorf("%w: stat artifact: %v", ErrStore, err)
	}
	artifactSHA, err := sha256File(artifactPath)
	if err != nil {
		return nil, err
	}
	manifestRaw, _ := json.Marshal(manifest)
	if _, err := s.PG.Exec(ctx, `
		UPDATE governance_export_jobs
		SET state = 'completed',
		    artifact_path = $2,
		    artifact_sha256 = $3,
		    artifact_bytes = $4,
		    manifest = $5,
		    updated_at = now(),
		    completed_at = now()
		WHERE export_id = $1
	`, exportID, artifactPath, artifactSHA, stat.Size(), manifestRaw); err != nil {
		return nil, fmt.Errorf("%w: update export job: %v", ErrStore, err)
	}
	if _, err := s.PG.Exec(ctx, `DELETE FROM governance_export_files WHERE export_id = $1`, exportID); err != nil {
		return nil, fmt.Errorf("%w: reset export file metadata: %v", ErrStore, err)
	}
	for _, file := range files {
		if _, err := s.PG.Exec(ctx, `
			INSERT INTO governance_export_files (export_id, path, content_type, row_count, bytes, sha256)
			VALUES ($1, $2, $3, $4, $5, $6)
		`, exportID, file.meta.Path, file.meta.ContentType, file.meta.Rows, file.meta.Bytes, file.meta.SHA256); err != nil {
			return nil, fmt.Errorf("%w: insert export file metadata: %v", ErrStore, err)
		}
	}
	job, err := s.GetExport(ctx, principal, exportID.String())
	if err != nil {
		return nil, err
	}
	return job, nil
}

func (s *Service) ListExports(ctx context.Context, principal Principal) ([]ExportJob, error) {
	ctx, span := tracer.Start(ctx, "governance.export.list")
	defer span.End()
	rows, err := s.PG.Query(ctx, `
		SELECT export_id, org_id, requested_by, scopes, include_logs, format, state,
		       artifact_path, artifact_sha256, artifact_bytes, manifest, error_code, error_message,
		       created_at, updated_at, completed_at, expires_at
		FROM governance_export_jobs
		WHERE org_id = $1
		ORDER BY created_at DESC, export_id DESC
		LIMIT 25
	`, principal.OrgID)
	if err != nil {
		return nil, fmt.Errorf("%w: list exports: %v", ErrStore, err)
	}
	defer rows.Close()
	var jobs []ExportJob
	for rows.Next() {
		job, err := scanExportJob(rows)
		if err != nil {
			return nil, err
		}
		files, err := s.exportFiles(ctx, job.ExportID)
		if err != nil {
			return nil, err
		}
		job.Files = files
		jobs = append(jobs, job)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("%w: export rows: %v", ErrStore, err)
	}
	span.SetAttributes(attribute.String("forge_metal.org_id", principal.OrgID), attribute.Int("forge_metal.export_count", len(jobs)))
	return jobs, nil
}

func (s *Service) GetExport(ctx context.Context, principal Principal, exportID string) (*ExportJob, error) {
	id, err := uuid.Parse(strings.TrimSpace(exportID))
	if err != nil {
		return nil, fmt.Errorf("%w: invalid export id", ErrInvalidArgument)
	}
	row := s.PG.QueryRow(ctx, `
		SELECT export_id, org_id, requested_by, scopes, include_logs, format, state,
		       artifact_path, artifact_sha256, artifact_bytes, manifest, error_code, error_message,
		       created_at, updated_at, completed_at, expires_at
		FROM governance_export_jobs
		WHERE export_id = $1 AND org_id = $2
	`, id, principal.OrgID)
	job, err := scanExportJob(row)
	if err != nil {
		if errorsIsNoRows(err) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	files, err := s.exportFiles(ctx, job.ExportID)
	if err != nil {
		return nil, err
	}
	job.Files = files
	return &job, nil
}

func (s *Service) MarkExportDownloaded(ctx context.Context, principal Principal, exportID uuid.UUID) error {
	_, err := s.PG.Exec(ctx, `
		UPDATE governance_export_jobs
		SET downloaded_at = now(), updated_at = now()
		WHERE export_id = $1 AND org_id = $2
	`, exportID, principal.OrgID)
	if err != nil {
		return fmt.Errorf("%w: mark export downloaded: %v", ErrStore, err)
	}
	return nil
}

func (s *Service) exportByIdempotency(ctx context.Context, orgID, idempotencyHash string) (*ExportJob, error) {
	row := s.PG.QueryRow(ctx, `
		SELECT export_id, org_id, requested_by, scopes, include_logs, format, state,
		       artifact_path, artifact_sha256, artifact_bytes, manifest, error_code, error_message,
		       created_at, updated_at, completed_at, expires_at
		FROM governance_export_jobs
		WHERE org_id = $1 AND idempotency_key_hash = $2
	`, orgID, idempotencyHash)
	job, err := scanExportJob(row)
	if err != nil {
		if errorsIsNoRows(err) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	files, err := s.exportFiles(ctx, job.ExportID)
	if err != nil {
		return nil, err
	}
	job.Files = files
	return &job, nil
}

type exportJobScanner interface {
	Scan(dest ...any) error
}

func scanExportJob(row exportJobScanner) (ExportJob, error) {
	var job ExportJob
	var scopes []string
	var completedAt pgtype.Timestamptz
	if err := row.Scan(
		&job.ExportID, &job.OrgID, &job.RequestedBy, &scopes, &job.IncludeLogs, &job.Format, &job.State,
		&job.ArtifactPath, &job.ArtifactSHA256, &job.ArtifactBytes, &job.Manifest, &job.ErrorCode, &job.ErrorMessage,
		&job.CreatedAt, &job.UpdatedAt, &completedAt, &job.ExpiresAt,
	); err != nil {
		if errorsIsNoRows(err) {
			return ExportJob{}, err
		}
		return ExportJob{}, fmt.Errorf("%w: scan export job: %v", ErrStore, err)
	}
	job.Scopes = scopes
	if completedAt.Valid {
		t := completedAt.Time
		job.CompletedAt = &t
	}
	return job, nil
}

func (s *Service) exportFiles(ctx context.Context, exportID uuid.UUID) ([]ExportFile, error) {
	rows, err := s.PG.Query(ctx, `
		SELECT path, content_type, row_count, bytes, sha256
		FROM governance_export_files
		WHERE export_id = $1
		ORDER BY path
	`, exportID)
	if err != nil {
		return nil, fmt.Errorf("%w: list export files: %v", ErrStore, err)
	}
	defer rows.Close()
	var files []ExportFile
	for rows.Next() {
		var file ExportFile
		if err := rows.Scan(&file.Path, &file.ContentType, &file.Rows, &file.Bytes, &file.SHA256); err != nil {
			return nil, fmt.Errorf("%w: scan export file: %v", ErrStore, err)
		}
		files = append(files, file)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("%w: export files rows: %v", ErrStore, err)
	}
	return files, nil
}

func (s *Service) collectExportFiles(ctx context.Context, orgID string, exportID uuid.UUID, scopes []string, includeLogs bool, expiresAt time.Time) ([]exportArtifactFile, error) {
	files := []exportArtifactFile{
		newArtifactFile("README.txt", "text/plain", []byte(exportReadme(orgID, exportID, scopes, includeLogs, expiresAt)), 1),
	}
	for _, scope := range scopes {
		switch scope {
		case "identity":
			scopeFiles, err := s.identityExportFiles(ctx, orgID)
			if err != nil {
				return nil, err
			}
			files = append(files, scopeFiles...)
		case "billing":
			scopeFiles, err := s.billingExportFiles(ctx, orgID)
			if err != nil {
				return nil, err
			}
			files = append(files, scopeFiles...)
		case "sandbox":
			scopeFiles, err := s.sandboxExportFiles(ctx, orgID, includeLogs)
			if err != nil {
				return nil, err
			}
			files = append(files, scopeFiles...)
		case "audit":
			scopeFiles, err := s.auditExportFiles(ctx, orgID)
			if err != nil {
				return nil, err
			}
			files = append(files, scopeFiles...)
		}
	}
	return files, nil
}

func (s *Service) identityExportFiles(ctx context.Context, orgID string) ([]exportArtifactFile, error) {
	if s.IdentityPG == nil {
		return nil, nil
	}
	return s.pgJSONLFiles(ctx, s.IdentityPG, []pgExportQuery{
		{Path: "identity/member_capabilities.jsonl", SQL: `SELECT row_to_json(t)::text FROM (SELECT * FROM identity_member_capabilities WHERE org_id = $1) t`, Args: []any{orgID}},
		{Path: "identity/api_credentials.jsonl", SQL: `
			SELECT row_to_json(t)::text
			FROM (
				SELECT c.credential_id, c.org_id, c.subject_id, c.client_id, c.display_name, c.auth_method,
				       c.status, c.policy_version_at_issue, c.created_at, c.created_by, c.updated_at,
				       c.expires_at, c.revoked_at, c.revoked_by, c.last_used_at,
				       COALESCE(array_agg(p.permission ORDER BY p.permission) FILTER (WHERE p.permission IS NOT NULL), '{}') AS permissions
				FROM identity_api_credentials c
				LEFT JOIN identity_api_credential_permissions p ON p.credential_id = c.credential_id
				WHERE c.org_id = $1
				GROUP BY c.credential_id
				ORDER BY c.created_at, c.credential_id
			) t`, Args: []any{orgID}},
	})
}

func (s *Service) billingExportFiles(ctx context.Context, orgID string) ([]exportArtifactFile, error) {
	if s.BillingPG == nil {
		return nil, nil
	}
	files, err := s.pgJSONLFiles(ctx, s.BillingPG, []pgExportQuery{
		{Path: "billing/orgs.jsonl", SQL: `SELECT row_to_json(t)::text FROM (SELECT * FROM orgs WHERE org_id = $1) t`, Args: []any{orgID}},
		{Path: "billing/catalog/products.jsonl", SQL: `SELECT row_to_json(t)::text FROM (SELECT * FROM products ORDER BY product_id) t`},
		{Path: "billing/catalog/credit_buckets.jsonl", SQL: `SELECT row_to_json(t)::text FROM (SELECT * FROM credit_buckets ORDER BY sort_order, bucket_id) t`},
		{Path: "billing/catalog/skus.jsonl", SQL: `SELECT row_to_json(t)::text FROM (SELECT * FROM skus ORDER BY product_id, bucket_id, sku_id) t`},
		{Path: "billing/catalog/plans.jsonl", SQL: `SELECT row_to_json(t)::text FROM (SELECT * FROM plans ORDER BY product_id, tier, plan_id) t`},
		{Path: "billing/catalog/plan_sku_rates.jsonl", SQL: `SELECT row_to_json(t)::text FROM (SELECT * FROM plan_sku_rates ORDER BY plan_id, sku_id, active_from) t`},
		{Path: "billing/catalog/entitlement_policies.jsonl", SQL: `SELECT row_to_json(t)::text FROM (SELECT * FROM entitlement_policies ORDER BY product_id, source, policy_id) t`},
		{Path: "billing/catalog/plan_entitlements.jsonl", SQL: `SELECT row_to_json(t)::text FROM (SELECT * FROM plan_entitlements ORDER BY plan_id, sort_order, policy_id) t`},
		{Path: "billing/contracts.jsonl", SQL: `SELECT row_to_json(t)::text FROM (SELECT * FROM contracts WHERE org_id = $1 ORDER BY created_at, contract_id) t`, Args: []any{orgID}},
		{Path: "billing/contract_changes.jsonl", SQL: `SELECT row_to_json(t)::text FROM (SELECT * FROM contract_changes WHERE org_id = $1 ORDER BY created_at, change_id) t`, Args: []any{orgID}},
		{Path: "billing/contract_phases.jsonl", SQL: `SELECT row_to_json(t)::text FROM (SELECT * FROM contract_phases WHERE org_id = $1 ORDER BY created_at, phase_id) t`, Args: []any{orgID}},
		{Path: "billing/contract_entitlement_lines.jsonl", SQL: `SELECT row_to_json(t)::text FROM (SELECT * FROM contract_entitlement_lines WHERE org_id = $1 ORDER BY created_at, line_id) t`, Args: []any{orgID}},
		{Path: "billing/billing_cycles.jsonl", SQL: `SELECT row_to_json(t)::text FROM (SELECT * FROM billing_cycles WHERE org_id = $1 ORDER BY starts_at, cycle_id) t`, Args: []any{orgID}},
		{Path: "billing/entitlement_periods.jsonl", SQL: `SELECT row_to_json(t)::text FROM (SELECT * FROM entitlement_periods WHERE org_id = $1 ORDER BY period_start, period_id) t`, Args: []any{orgID}},
		{Path: "billing/credit_grants.jsonl", SQL: `SELECT row_to_json(t)::text FROM (SELECT * FROM credit_grants WHERE org_id = $1 ORDER BY starts_at, grant_id) t`, Args: []any{orgID}},
		{Path: "billing/billing_windows.jsonl", SQL: `SELECT row_to_json(t)::text FROM (SELECT * FROM billing_windows WHERE org_id = $1 ORDER BY window_start, window_id) t`, Args: []any{orgID}},
		{Path: "billing/billing_window_ledger_legs.jsonl", SQL: `SELECT row_to_json(t)::text FROM (SELECT l.* FROM billing_window_ledger_legs l JOIN billing_windows w ON w.window_id = l.window_id WHERE w.org_id = $1 ORDER BY l.window_id, l.leg_seq) t`, Args: []any{orgID}},
		{Path: "billing/billing_finalizations.jsonl", SQL: `SELECT row_to_json(t)::text FROM (SELECT * FROM billing_finalizations WHERE org_id = $1 ORDER BY created_at, finalization_id) t`, Args: []any{orgID}},
		{Path: "billing/billing_documents.jsonl", SQL: `SELECT row_to_json(t)::text FROM (SELECT * FROM billing_documents WHERE org_id = $1 ORDER BY created_at, document_id) t`, Args: []any{orgID}},
		{Path: "billing/billing_document_line_items.jsonl", SQL: `SELECT row_to_json(t)::text FROM (SELECT li.* FROM billing_document_line_items li JOIN billing_documents d ON d.document_id = li.document_id WHERE d.org_id = $1 ORDER BY li.document_id, li.line_item_id) t`, Args: []any{orgID}},
		{Path: "billing/invoice_adjustments.jsonl", SQL: `SELECT row_to_json(t)::text FROM (SELECT * FROM invoice_adjustments WHERE org_id = $1 ORDER BY created_at, adjustment_id) t`, Args: []any{orgID}},
		{Path: "billing/billing_events.jsonl", SQL: `SELECT row_to_json(t)::text FROM (SELECT * FROM billing_events WHERE org_id = $1 ORDER BY occurred_at, event_id) t`, Args: []any{orgID}},
	})
	if err != nil {
		return nil, err
	}
	invoices, err := s.billingInvoicesCSV(ctx, orgID)
	if err != nil {
		return nil, err
	}
	files = append(files, invoices)
	return files, nil
}

func (s *Service) sandboxExportFiles(ctx context.Context, orgID string, includeLogs bool) ([]exportArtifactFile, error) {
	if s.SandboxPG == nil {
		return nil, nil
	}
	orgWhere := "org_id::text = $1"
	queries := []pgExportQuery{
		{Path: "sandbox/executions.jsonl", SQL: `SELECT row_to_json(t)::text FROM (SELECT * FROM executions WHERE ` + orgWhere + ` ORDER BY created_at, execution_id) t`, Args: []any{orgID}},
		{Path: "sandbox/execution_attempts.jsonl", SQL: `SELECT row_to_json(t)::text FROM (SELECT a.* FROM execution_attempts a JOIN executions e ON e.execution_id = a.execution_id WHERE e.org_id::text = $1 ORDER BY a.created_at, a.attempt_id) t`, Args: []any{orgID}},
		{Path: "sandbox/execution_events.jsonl", SQL: `SELECT row_to_json(t)::text FROM (SELECT ev.* FROM execution_events ev JOIN executions e ON e.execution_id = ev.execution_id WHERE e.org_id::text = $1 ORDER BY ev.created_at, ev.event_seq) t`, Args: []any{orgID}},
		{Path: "sandbox/execution_billing_windows.jsonl", SQL: `SELECT row_to_json(t)::text FROM (SELECT bw.* FROM execution_billing_windows bw JOIN execution_attempts a ON a.attempt_id = bw.attempt_id JOIN executions e ON e.execution_id = a.execution_id WHERE e.org_id::text = $1 ORDER BY bw.window_start, bw.billing_window_id) t`, Args: []any{orgID}},
		{Path: "sandbox/github_installations.jsonl", SQL: `SELECT row_to_json(t)::text FROM (SELECT * FROM github_installations WHERE ` + orgWhere + ` ORDER BY created_at, installation_id) t`, Args: []any{orgID}},
		{Path: "sandbox/github_workflow_jobs.jsonl", SQL: `SELECT row_to_json(t)::text FROM (SELECT j.* FROM github_workflow_jobs j JOIN github_installations i ON i.installation_id = j.installation_id WHERE i.org_id::text = $1 ORDER BY j.created_at, j.github_job_id) t`, Args: []any{orgID}},
		{Path: "sandbox/github_runner_allocations.jsonl", SQL: `SELECT row_to_json(t)::text FROM (SELECT a.* FROM github_runner_allocations a JOIN github_installations i ON i.installation_id = a.installation_id WHERE i.org_id::text = $1 ORDER BY a.created_at, a.allocation_id) t`, Args: []any{orgID}},
		{Path: "sandbox/github_runner_job_bindings.jsonl", SQL: `SELECT row_to_json(t)::text FROM (SELECT b.* FROM github_runner_job_bindings b JOIN github_runner_allocations a ON a.allocation_id = b.allocation_id JOIN github_installations i ON i.installation_id = a.installation_id WHERE i.org_id::text = $1 ORDER BY b.created_at, b.binding_id) t`, Args: []any{orgID}},
		{Path: "sandbox/execution_filesystem_mounts.jsonl", SQL: `SELECT row_to_json(t)::text FROM (SELECT m.* FROM execution_filesystem_mounts m JOIN executions e ON e.execution_id = m.execution_id WHERE e.org_id::text = $1 ORDER BY m.execution_id, m.sort_order, m.mount_name) t`, Args: []any{orgID}},
		{Path: "sandbox/github_sticky_disk_generations.jsonl", SQL: `SELECT row_to_json(t)::text FROM (SELECT g.* FROM github_sticky_disk_generations g JOIN github_installations i ON i.installation_id = g.installation_id WHERE i.org_id::text = $1 ORDER BY g.updated_at, g.installation_id, g.repository_id, g.key_hash) t`, Args: []any{orgID}},
		{Path: "sandbox/execution_sticky_disk_mounts.jsonl", SQL: `SELECT row_to_json(t)::text FROM (SELECT m.* FROM execution_sticky_disk_mounts m JOIN executions e ON e.execution_id = m.execution_id WHERE e.org_id::text = $1 ORDER BY m.created_at, m.mount_id) t`, Args: []any{orgID}},
		{Path: "sandbox/vm_resource_bounds.jsonl", SQL: `SELECT row_to_json(t)::text FROM (SELECT * FROM vm_resource_bounds WHERE ` + orgWhere + ` ORDER BY updated_at) t`, Args: []any{orgID}},
	}
	if includeLogs {
		queries = append(queries, pgExportQuery{Path: "sandbox/execution_logs.jsonl", SQL: `SELECT row_to_json(t)::text FROM (SELECT l.* FROM execution_logs l JOIN executions e ON e.execution_id = l.execution_id WHERE e.org_id::text = $1 ORDER BY l.attempt_id, l.seq) t`, Args: []any{orgID}})
	}
	return s.pgJSONLFiles(ctx, s.SandboxPG, queries)
}

func (s *Service) auditExportFiles(ctx context.Context, orgID string) ([]exportArtifactFile, error) {
	rows, err := s.CH.Query(ctx, `
		SELECT
			event_id, recorded_at, event_date, org_id, service_name, operation_id, audit_event,
			principal_type, principal_id, principal_email,
			permission, resource_kind, resource_id, action, org_scope, rate_limit_class,
			result, error_code, error_message, client_ip, user_agent_hash, idempotency_key_hash,
			request_id, trace_id, payload_json, content_sha256, sequence, prev_hmac, row_hmac
		FROM forge_metal.audit_events
		WHERE org_id = $1
		ORDER BY recorded_at, sequence
	`, orgID)
	if err != nil {
		return nil, fmt.Errorf("%w: query audit export: %v", ErrStore, err)
	}
	defer rows.Close()
	var body bytes.Buffer
	var count int64
	for rows.Next() {
		var event AuditEvent
		if err := rows.ScanStruct(&event); err != nil {
			return nil, fmt.Errorf("%w: scan audit export row: %v", ErrStore, err)
		}
		raw, err := json.Marshal(auditEventExportRow(event))
		if err != nil {
			return nil, fmt.Errorf("%w: marshal audit export row: %v", ErrStore, err)
		}
		body.Write(raw)
		body.WriteByte('\n')
		count++
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("%w: audit export rows: %v", ErrStore, err)
	}
	return []exportArtifactFile{newArtifactFile("audit/audit_events.jsonl", "application/x-ndjson", body.Bytes(), count)}, nil
}

type pgExportQuery struct {
	Path string
	SQL  string
	Args []any
}

func (s *Service) pgJSONLFiles(ctx context.Context, pool *pgxpool.Pool, queries []pgExportQuery) ([]exportArtifactFile, error) {
	files := make([]exportArtifactFile, 0, len(queries))
	for _, query := range queries {
		body, count, err := pgRowsJSONL(ctx, pool, query.SQL, query.Args...)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", query.Path, err)
		}
		files = append(files, newArtifactFile(query.Path, "application/x-ndjson", body, count))
	}
	return files, nil
}

func pgRowsJSONL(ctx context.Context, pool *pgxpool.Pool, query string, args ...any) ([]byte, int64, error) {
	rows, err := pool.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("%w: query postgres export: %v", ErrStore, err)
	}
	defer rows.Close()
	var out bytes.Buffer
	var count int64
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, 0, fmt.Errorf("%w: scan postgres export row: %v", ErrStore, err)
		}
		out.WriteString(raw)
		out.WriteByte('\n')
		count++
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("%w: postgres export rows: %v", ErrStore, err)
	}
	return out.Bytes(), count, nil
}

func (s *Service) billingInvoicesCSV(ctx context.Context, orgID string) (exportArtifactFile, error) {
	rows, err := s.BillingPG.Query(ctx, `
		SELECT document_id, COALESCE(document_number, ''), document_kind, status, payment_status,
		       period_start, period_end, COALESCE(issued_at, 'epoch'::timestamptz),
		       currency, subtotal_units, adjustment_units, tax_units, total_due_units,
		       recipient_email, stripe_hosted_invoice_url, stripe_invoice_pdf_url
		FROM billing_documents
		WHERE org_id = $1
		ORDER BY created_at, document_id
	`, orgID)
	if err != nil {
		return exportArtifactFile{}, fmt.Errorf("%w: query invoices csv: %v", ErrStore, err)
	}
	defer rows.Close()
	var body bytes.Buffer
	writer := csv.NewWriter(&body)
	header := []string{"document_id", "document_number", "document_kind", "status", "payment_status", "period_start", "period_end", "issued_at", "currency", "subtotal_units", "adjustment_units", "tax_units", "total_due_units", "recipient_email", "hosted_invoice_url", "invoice_pdf_url"}
	if err := writer.Write(header); err != nil {
		return exportArtifactFile{}, fmt.Errorf("%w: write invoices csv header: %v", ErrStore, err)
	}
	var count int64
	for rows.Next() {
		values := make([]any, len(header))
		ptrs := make([]any, len(header))
		for i := range values {
			ptrs[i] = &values[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return exportArtifactFile{}, fmt.Errorf("%w: scan invoices csv: %v", ErrStore, err)
		}
		record := make([]string, len(values))
		for i, value := range values {
			record[i] = exportValueString(value)
			if i == 7 && record[i] == "1970-01-01T00:00:00Z" {
				record[i] = ""
			}
		}
		if err := writer.Write(record); err != nil {
			return exportArtifactFile{}, fmt.Errorf("%w: write invoices csv row: %v", ErrStore, err)
		}
		count++
	}
	if err := rows.Err(); err != nil {
		return exportArtifactFile{}, fmt.Errorf("%w: invoices rows: %v", ErrStore, err)
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		return exportArtifactFile{}, fmt.Errorf("%w: flush invoices csv: %v", ErrStore, err)
	}
	return newArtifactFile("billing/invoices.csv", "text/csv", body.Bytes(), count), nil
}

func exportValueString(value any) string {
	switch v := value.(type) {
	case nil:
		return ""
	case time.Time:
		return v.UTC().Format(time.RFC3339)
	case []byte:
		return hex.EncodeToString(v)
	case int64:
		return strconv.FormatInt(v, 10)
	case int32:
		return strconv.FormatInt(int64(v), 10)
	case bool:
		return strconv.FormatBool(v)
	default:
		return fmt.Sprint(v)
	}
}

func auditEventExportRow(event AuditEvent) map[string]any {
	payload := json.RawMessage(event.PayloadJSON)
	row := map[string]any{
		"event_id":             event.EventID.String(),
		"recorded_at":          event.RecordedAt.UTC().Format(time.RFC3339Nano),
		"event_date":           event.EventDate.UTC().Format("2006-01-02"),
		"org_id":               event.OrgID,
		"service_name":         event.ServiceName,
		"operation_id":         event.OperationID,
		"audit_event":          event.AuditEvent,
		"principal_type":       event.PrincipalType,
		"principal_id":         event.PrincipalID,
		"principal_email":      event.PrincipalEmail,
		"permission":           event.Permission,
		"resource_kind":        event.ResourceKind,
		"resource_id":          event.ResourceID,
		"action":               event.Action,
		"org_scope":            event.OrgScope,
		"rate_limit_class":     event.RateLimitClass,
		"result":               event.Result,
		"error_code":           event.ErrorCode,
		"error_message":        event.ErrorMessage,
		"client_ip":            event.ClientIP,
		"user_agent_hash":      event.UserAgentHash,
		"idempotency_key_hash": event.IdempotencyKeyHash,
		"request_id":           event.RequestID,
		"trace_id":             event.TraceID,
		"content_sha256":       event.ContentSHA256,
		"sequence":             strconv.FormatUint(event.Sequence, 10),
		"prev_hmac":            event.PrevHMAC,
		"row_hmac":             event.RowHMAC,
	}
	if json.Valid(payload) {
		row["payload"] = payload
	} else {
		row["payload_json"] = event.PayloadJSON
	}
	return row
}

func newArtifactFile(path, contentType string, body []byte, rows int64) exportArtifactFile {
	sum := sha256.Sum256(body)
	return exportArtifactFile{
		meta: ExportFile{
			Path:        path,
			ContentType: contentType,
			Rows:        rows,
			Bytes:       int64(len(body)),
			SHA256:      hex.EncodeToString(sum[:]),
		},
		body: body,
	}
}

func writeTarGzip(path string, files []exportArtifactFile) error {
	tmp := path + ".tmp"
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return fmt.Errorf("%w: create artifact directory: %v", ErrStore, err)
	}
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o640)
	if err != nil {
		return fmt.Errorf("%w: create artifact: %v", ErrStore, err)
	}
	defer func() { _ = f.Close() }()
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	for _, file := range files {
		header := &tar.Header{
			Name:    file.meta.Path,
			Mode:    0o640,
			Size:    int64(len(file.body)),
			ModTime: time.Now().UTC(),
		}
		if err := tw.WriteHeader(header); err != nil {
			return fmt.Errorf("%w: write tar header: %v", ErrStore, err)
		}
		if _, err := tw.Write(file.body); err != nil {
			return fmt.Errorf("%w: write tar body: %v", ErrStore, err)
		}
	}
	if err := tw.Close(); err != nil {
		return fmt.Errorf("%w: close tar: %v", ErrStore, err)
	}
	if err := gz.Close(); err != nil {
		return fmt.Errorf("%w: close gzip: %v", ErrStore, err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("%w: close artifact: %v", ErrStore, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("%w: publish artifact: %v", ErrStore, err)
	}
	return nil
}

func sha256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func exportFilesMetadata(files []exportArtifactFile) []ExportFile {
	out := make([]ExportFile, 0, len(files))
	for _, file := range files {
		out = append(out, file.meta)
	}
	return out
}

func normalizeScopes(scopes []string) ([]string, error) {
	allowed := map[string]struct{}{"identity": {}, "billing": {}, "sandbox": {}, "audit": {}}
	if len(scopes) == 0 {
		return append([]string(nil), defaultScopes...), nil
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(scopes))
	for _, scope := range scopes {
		scope = strings.ToLower(strings.TrimSpace(scope))
		if _, ok := allowed[scope]; !ok {
			return nil, fmt.Errorf("%w: unsupported export scope %q", ErrInvalidArgument, scope)
		}
		if _, ok := seen[scope]; ok {
			continue
		}
		seen[scope] = struct{}{}
		out = append(out, scope)
	}
	sort.Strings(out)
	return out, nil
}

func exportReadme(orgID string, exportID uuid.UUID, scopes []string, includeLogs bool, expiresAt time.Time) string {
	return fmt.Sprintf(`Forge Metal organization export

export_id: %s
org_id: %s
scopes: %s
include_logs: %t
expires_at: %s

Data files use JSON Lines unless the file extension is .csv. The manifest
contains row counts, byte counts, and SHA-256 checksums for each file.
Secrets, credential hashes, provider webhook payloads, River internals, and
short-lived runner setup/JIT material are intentionally excluded.
`, exportID.String(), orgID, strings.Join(scopes, ","), includeLogs, expiresAt.UTC().Format(time.RFC3339Nano))
}

func errorsIsNoRows(err error) bool {
	return errors.Is(err, pgx.ErrNoRows)
}

func errorsIsNotFound(err error) bool {
	return errors.Is(err, ErrNotFound) || errorsIsNoRows(err)
}
