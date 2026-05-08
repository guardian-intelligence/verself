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
	"github.com/verself/governance-service/internal/store"
	"github.com/verself/governance-service/internal/store/billingexport"
	"github.com/verself/governance-service/internal/store/identityexport"
	"github.com/verself/governance-service/internal/store/sandboxexport"
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
	queries := store.New(s.PG)
	exportID := uuid.New()
	expiresAt := time.Now().UTC().Add(s.ExportTTL)
	if err := queries.InsertExportJob(ctx, store.InsertExportJobParams{
		ExportID:           exportID,
		OrgID:              orgID,
		RequestedBy:        principal.Subject,
		IdempotencyKeyHash: idempotencyHash,
		Scopes:             scopes,
		IncludeLogs:        req.IncludeLogs,
		ExpiresAt:          pgtype.Timestamptz{Time: expiresAt, Valid: true},
	}); err != nil {
		return nil, fmt.Errorf("%w: create export job: %v", ErrStore, err)
	}

	job, err := s.buildExportArtifact(ctx, principal, exportID, scopes, req.IncludeLogs, expiresAt)
	if err != nil {
		_ = queries.MarkExportFailed(ctx, store.MarkExportFailedParams{
			ExportID:     exportID,
			ErrorMessage: err.Error(),
		})
		return nil, err
	}
	span.SetAttributes(
		attribute.String("verself.org_id", orgID),
		attribute.String("verself.export_id", exportID.String()),
		attribute.Int("verself.export_file_count", len(job.Files)),
		attribute.Int64("verself.export_bytes", job.ArtifactBytes),
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
	queries := store.New(s.PG)
	if err := queries.CompleteExportJob(ctx, store.CompleteExportJobParams{
		ExportID:       exportID,
		ArtifactPath:   artifactPath,
		ArtifactSha256: artifactSHA,
		ArtifactBytes:  stat.Size(),
		Manifest:       manifestRaw,
	}); err != nil {
		return nil, fmt.Errorf("%w: update export job: %v", ErrStore, err)
	}
	if err := queries.DeleteExportFiles(ctx, store.DeleteExportFilesParams{ExportID: exportID}); err != nil {
		return nil, fmt.Errorf("%w: reset export file metadata: %v", ErrStore, err)
	}
	for _, file := range files {
		if err := queries.InsertExportFile(ctx, store.InsertExportFileParams{
			ExportID:    exportID,
			Path:        file.meta.Path,
			ContentType: file.meta.ContentType,
			RowCount:    file.meta.Rows,
			Bytes:       file.meta.Bytes,
			Sha256:      file.meta.SHA256,
		}); err != nil {
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
	rows, err := store.New(s.PG).ListExportsForOrg(ctx, store.ListExportsForOrgParams{OrgID: principal.OrgID})
	if err != nil {
		return nil, fmt.Errorf("%w: list exports: %v", ErrStore, err)
	}
	jobs := make([]ExportJob, 0, len(rows))
	for _, row := range rows {
		job, err := exportJobFromListRow(row)
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
	span.SetAttributes(attribute.String("verself.org_id", principal.OrgID), attribute.Int("verself.export_count", len(jobs)))
	return jobs, nil
}

func (s *Service) GetExport(ctx context.Context, principal Principal, exportID string) (*ExportJob, error) {
	id, err := uuid.Parse(strings.TrimSpace(exportID))
	if err != nil {
		return nil, fmt.Errorf("%w: invalid export id", ErrInvalidArgument)
	}
	row, err := store.New(s.PG).GetExportByIDAndOrg(ctx, store.GetExportByIDAndOrgParams{
		ExportID: id,
		OrgID:    principal.OrgID,
	})
	if err != nil {
		if errorsIsNoRows(err) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("%w: get export: %v", ErrStore, err)
	}
	job, err := exportJobFromIDRow(row)
	if err != nil {
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
	if err := store.New(s.PG).MarkExportDownloaded(ctx, store.MarkExportDownloadedParams{
		ExportID: exportID,
		OrgID:    principal.OrgID,
	}); err != nil {
		return fmt.Errorf("%w: mark export downloaded: %v", ErrStore, err)
	}
	return nil
}

func (s *Service) exportByIdempotency(ctx context.Context, orgID, idempotencyHash string) (*ExportJob, error) {
	row, err := store.New(s.PG).GetExportByIdempotency(ctx, store.GetExportByIdempotencyParams{
		OrgID:              orgID,
		IdempotencyKeyHash: idempotencyHash,
	})
	if err != nil {
		if errorsIsNoRows(err) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("%w: get export by idempotency: %v", ErrStore, err)
	}
	job, err := exportJobFromIdempotencyRow(row)
	if err != nil {
		return nil, err
	}
	files, err := s.exportFiles(ctx, job.ExportID)
	if err != nil {
		return nil, err
	}
	job.Files = files
	return &job, nil
}

func exportJobFromListRow(row store.ListExportsForOrgRow) (ExportJob, error) {
	return exportJobFromStoreFields(
		row.ExportID, row.OrgID, row.RequestedBy, row.Scopes, row.IncludeLogs, row.Format, row.State,
		row.ArtifactPath, row.ArtifactSha256, row.ArtifactBytes, row.Manifest, row.ErrorCode, row.ErrorMessage,
		row.CreatedAt, row.UpdatedAt, row.CompletedAt, row.ExpiresAt,
	)
}

func exportJobFromIDRow(row store.GetExportByIDAndOrgRow) (ExportJob, error) {
	return exportJobFromStoreFields(
		row.ExportID, row.OrgID, row.RequestedBy, row.Scopes, row.IncludeLogs, row.Format, row.State,
		row.ArtifactPath, row.ArtifactSha256, row.ArtifactBytes, row.Manifest, row.ErrorCode, row.ErrorMessage,
		row.CreatedAt, row.UpdatedAt, row.CompletedAt, row.ExpiresAt,
	)
}

func exportJobFromIdempotencyRow(row store.GetExportByIdempotencyRow) (ExportJob, error) {
	return exportJobFromStoreFields(
		row.ExportID, row.OrgID, row.RequestedBy, row.Scopes, row.IncludeLogs, row.Format, row.State,
		row.ArtifactPath, row.ArtifactSha256, row.ArtifactBytes, row.Manifest, row.ErrorCode, row.ErrorMessage,
		row.CreatedAt, row.UpdatedAt, row.CompletedAt, row.ExpiresAt,
	)
}

func exportJobFromStoreFields(
	exportID uuid.UUID,
	orgID string,
	requestedBy string,
	scopes []string,
	includeLogs bool,
	format string,
	state string,
	artifactPath string,
	artifactSHA256 string,
	artifactBytes int64,
	manifest []byte,
	errorCode string,
	errorMessage string,
	createdAt pgtype.Timestamptz,
	updatedAt pgtype.Timestamptz,
	completedAt pgtype.Timestamptz,
	expiresAt pgtype.Timestamptz,
) (ExportJob, error) {
	created, err := requiredPGTime(createdAt, "created_at")
	if err != nil {
		return ExportJob{}, err
	}
	updated, err := requiredPGTime(updatedAt, "updated_at")
	if err != nil {
		return ExportJob{}, err
	}
	expires, err := requiredPGTime(expiresAt, "expires_at")
	if err != nil {
		return ExportJob{}, err
	}
	var completed *time.Time
	if completedAt.Valid {
		t := completedAt.Time
		completed = &t
	}
	return ExportJob{
		ExportID:       exportID,
		OrgID:          orgID,
		RequestedBy:    requestedBy,
		Scopes:         scopes,
		IncludeLogs:    includeLogs,
		Format:         format,
		State:          state,
		ArtifactPath:   artifactPath,
		ArtifactSHA256: artifactSHA256,
		ArtifactBytes:  artifactBytes,
		Manifest:       json.RawMessage(manifest),
		ErrorCode:      errorCode,
		ErrorMessage:   errorMessage,
		CreatedAt:      created,
		UpdatedAt:      updated,
		CompletedAt:    completed,
		ExpiresAt:      expires,
	}, nil
}

func requiredPGTime(value pgtype.Timestamptz, column string) (time.Time, error) {
	if !value.Valid {
		return time.Time{}, fmt.Errorf("%w: export job %s is null", ErrStore, column)
	}
	return value.Time, nil
}

func (s *Service) exportFiles(ctx context.Context, exportID uuid.UUID) ([]ExportFile, error) {
	rows, err := store.New(s.PG).ListExportFiles(ctx, store.ListExportFilesParams{ExportID: exportID})
	if err != nil {
		return nil, fmt.Errorf("%w: list export files: %v", ErrStore, err)
	}
	files := make([]ExportFile, 0, len(rows))
	for _, row := range rows {
		files = append(files, ExportFile{
			Path:        row.Path,
			ContentType: row.ContentType,
			Rows:        row.RowCount,
			Bytes:       row.Bytes,
			SHA256:      row.Sha256,
		})
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
	q := identityexport.New(s.IdentityPG)
	files := []exportArtifactFile{}
	add := func(path string, rows []string, err error) error {
		if err != nil {
			return fmt.Errorf("%s: %w: query postgres export: %v", path, ErrStore, err)
		}
		files = append(files, jsonlArtifactFile(path, rows))
		return nil
	}
	rows, err := q.ExportIdentityMemberCapabilitiesJSONL(ctx, identityexport.ExportIdentityMemberCapabilitiesJSONLParams{OrgID: orgID})
	if err := add("identity/member_capabilities.jsonl", rows, err); err != nil {
		return nil, err
	}
	rows, err = q.ExportIdentityAPICredentialsJSONL(ctx, identityexport.ExportIdentityAPICredentialsJSONLParams{OrgID: orgID})
	if err := add("identity/api_credentials.jsonl", rows, err); err != nil {
		return nil, err
	}
	return files, nil
}

func (s *Service) billingExportFiles(ctx context.Context, orgID string) ([]exportArtifactFile, error) {
	if s.BillingPG == nil {
		return nil, nil
	}
	q := billingexport.New(s.BillingPG)
	files := []exportArtifactFile{}
	add := func(path string, rows []string, err error) error {
		if err != nil {
			return fmt.Errorf("%s: %w: query postgres export: %v", path, ErrStore, err)
		}
		files = append(files, jsonlArtifactFile(path, rows))
		return nil
	}
	org := billingexport.ExportBillingOrgsJSONLParams{OrgID: orgID}
	rows, err := q.ExportBillingOrgsJSONL(ctx, org)
	if err := add("billing/orgs.jsonl", rows, err); err != nil {
		return nil, err
	}
	rows, err = q.ExportBillingProductsJSONL(ctx)
	if err := add("billing/catalog/products.jsonl", rows, err); err != nil {
		return nil, err
	}
	rows, err = q.ExportBillingCreditBucketsJSONL(ctx)
	if err := add("billing/catalog/credit_buckets.jsonl", rows, err); err != nil {
		return nil, err
	}
	rows, err = q.ExportBillingSKUsJSONL(ctx)
	if err := add("billing/catalog/skus.jsonl", rows, err); err != nil {
		return nil, err
	}
	rows, err = q.ExportBillingPlansJSONL(ctx)
	if err := add("billing/catalog/plans.jsonl", rows, err); err != nil {
		return nil, err
	}
	rows, err = q.ExportBillingPlanSKURatesJSONL(ctx)
	if err := add("billing/catalog/plan_sku_rates.jsonl", rows, err); err != nil {
		return nil, err
	}
	rows, err = q.ExportBillingEntitlementPoliciesJSONL(ctx)
	if err := add("billing/catalog/entitlement_policies.jsonl", rows, err); err != nil {
		return nil, err
	}
	rows, err = q.ExportBillingPlanEntitlementsJSONL(ctx)
	if err := add("billing/catalog/plan_entitlements.jsonl", rows, err); err != nil {
		return nil, err
	}
	rows, err = q.ExportBillingContractsJSONL(ctx, billingexport.ExportBillingContractsJSONLParams{OrgID: orgID})
	if err := add("billing/contracts.jsonl", rows, err); err != nil {
		return nil, err
	}
	rows, err = q.ExportBillingContractChangesJSONL(ctx, billingexport.ExportBillingContractChangesJSONLParams{OrgID: orgID})
	if err := add("billing/contract_changes.jsonl", rows, err); err != nil {
		return nil, err
	}
	rows, err = q.ExportBillingContractPhasesJSONL(ctx, billingexport.ExportBillingContractPhasesJSONLParams{OrgID: orgID})
	if err := add("billing/contract_phases.jsonl", rows, err); err != nil {
		return nil, err
	}
	rows, err = q.ExportBillingContractEntitlementLinesJSONL(ctx, billingexport.ExportBillingContractEntitlementLinesJSONLParams{OrgID: orgID})
	if err := add("billing/contract_entitlement_lines.jsonl", rows, err); err != nil {
		return nil, err
	}
	rows, err = q.ExportBillingCyclesJSONL(ctx, billingexport.ExportBillingCyclesJSONLParams{OrgID: orgID})
	if err := add("billing/billing_cycles.jsonl", rows, err); err != nil {
		return nil, err
	}
	rows, err = q.ExportBillingEntitlementPeriodsJSONL(ctx, billingexport.ExportBillingEntitlementPeriodsJSONLParams{OrgID: orgID})
	if err := add("billing/entitlement_periods.jsonl", rows, err); err != nil {
		return nil, err
	}
	rows, err = q.ExportBillingCreditGrantsJSONL(ctx, billingexport.ExportBillingCreditGrantsJSONLParams{OrgID: orgID})
	if err := add("billing/credit_grants.jsonl", rows, err); err != nil {
		return nil, err
	}
	rows, err = q.ExportBillingWindowsJSONL(ctx, billingexport.ExportBillingWindowsJSONLParams{OrgID: orgID})
	if err := add("billing/billing_windows.jsonl", rows, err); err != nil {
		return nil, err
	}
	rows, err = q.ExportBillingWindowLedgerLegsJSONL(ctx, billingexport.ExportBillingWindowLedgerLegsJSONLParams{OrgID: orgID})
	if err := add("billing/billing_window_ledger_legs.jsonl", rows, err); err != nil {
		return nil, err
	}
	rows, err = q.ExportBillingFinalizationsJSONL(ctx, billingexport.ExportBillingFinalizationsJSONLParams{OrgID: orgID})
	if err := add("billing/billing_finalizations.jsonl", rows, err); err != nil {
		return nil, err
	}
	rows, err = q.ExportBillingDocumentsJSONL(ctx, billingexport.ExportBillingDocumentsJSONLParams{OrgID: orgID})
	if err := add("billing/billing_documents.jsonl", rows, err); err != nil {
		return nil, err
	}
	rows, err = q.ExportBillingDocumentLineItemsJSONL(ctx, billingexport.ExportBillingDocumentLineItemsJSONLParams{OrgID: orgID})
	if err := add("billing/billing_document_line_items.jsonl", rows, err); err != nil {
		return nil, err
	}
	rows, err = q.ExportBillingInvoiceAdjustmentsJSONL(ctx, billingexport.ExportBillingInvoiceAdjustmentsJSONLParams{OrgID: orgID})
	if err := add("billing/invoice_adjustments.jsonl", rows, err); err != nil {
		return nil, err
	}
	rows, err = q.ExportBillingEventsJSONL(ctx, billingexport.ExportBillingEventsJSONLParams{OrgID: orgID})
	if err := add("billing/billing_events.jsonl", rows, err); err != nil {
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
	sandboxOrgID, err := strconv.ParseInt(orgID, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("%w: parse sandbox export org_id %q: %v", ErrInvalidArgument, orgID, err)
	}
	q := sandboxexport.New(s.SandboxPG)
	files := []exportArtifactFile{}
	add := func(path string, rows []string, err error) error {
		if err != nil {
			return fmt.Errorf("%s: %w: query postgres export: %v", path, ErrStore, err)
		}
		files = append(files, jsonlArtifactFile(path, rows))
		return nil
	}
	rows, err := q.ExportSandboxExecutionsJSONL(ctx, sandboxexport.ExportSandboxExecutionsJSONLParams{OrgID: sandboxOrgID})
	if err := add("sandbox/executions.jsonl", rows, err); err != nil {
		return nil, err
	}
	rows, err = q.ExportSandboxExecutionAttemptsJSONL(ctx, sandboxexport.ExportSandboxExecutionAttemptsJSONLParams{OrgID: sandboxOrgID})
	if err := add("sandbox/execution_attempts.jsonl", rows, err); err != nil {
		return nil, err
	}
	rows, err = q.ExportSandboxExecutionEventsJSONL(ctx, sandboxexport.ExportSandboxExecutionEventsJSONLParams{OrgID: sandboxOrgID})
	if err := add("sandbox/execution_events.jsonl", rows, err); err != nil {
		return nil, err
	}
	rows, err = q.ExportSandboxExecutionBillingWindowsJSONL(ctx, sandboxexport.ExportSandboxExecutionBillingWindowsJSONLParams{OrgID: sandboxOrgID})
	if err := add("sandbox/execution_billing_windows.jsonl", rows, err); err != nil {
		return nil, err
	}
	rows, err = q.ExportSandboxGithubInstallationsJSONL(ctx, sandboxexport.ExportSandboxGithubInstallationsJSONLParams{OrgID: sandboxOrgID})
	if err := add("sandbox/github_installations.jsonl", rows, err); err != nil {
		return nil, err
	}
	rows, err = q.ExportSandboxRunnerProviderRepositoriesJSONL(ctx, sandboxexport.ExportSandboxRunnerProviderRepositoriesJSONLParams{OrgID: sandboxOrgID})
	if err := add("sandbox/runner_provider_repositories.jsonl", rows, err); err != nil {
		return nil, err
	}
	rows, err = q.ExportSandboxRunnerJobsJSONL(ctx, sandboxexport.ExportSandboxRunnerJobsJSONLParams{OrgID: sandboxOrgID})
	if err := add("sandbox/runner_jobs.jsonl", rows, err); err != nil {
		return nil, err
	}
	rows, err = q.ExportSandboxRunnerAllocationsJSONL(ctx, sandboxexport.ExportSandboxRunnerAllocationsJSONLParams{OrgID: sandboxOrgID})
	if err := add("sandbox/runner_allocations.jsonl", rows, err); err != nil {
		return nil, err
	}
	rows, err = q.ExportSandboxRunnerJobBindingsJSONL(ctx, sandboxexport.ExportSandboxRunnerJobBindingsJSONLParams{OrgID: sandboxOrgID})
	if err := add("sandbox/runner_job_bindings.jsonl", rows, err); err != nil {
		return nil, err
	}
	rows, err = q.ExportSandboxExecutionFilesystemMountsJSONL(ctx, sandboxexport.ExportSandboxExecutionFilesystemMountsJSONLParams{OrgID: sandboxOrgID})
	if err := add("sandbox/execution_filesystem_mounts.jsonl", rows, err); err != nil {
		return nil, err
	}
	rows, err = q.ExportSandboxRunnerStickyDiskGenerationsJSONL(ctx, sandboxexport.ExportSandboxRunnerStickyDiskGenerationsJSONLParams{OrgID: sandboxOrgID})
	if err := add("sandbox/runner_sticky_disk_generations.jsonl", rows, err); err != nil {
		return nil, err
	}
	rows, err = q.ExportSandboxExecutionStickyDiskMountsJSONL(ctx, sandboxexport.ExportSandboxExecutionStickyDiskMountsJSONLParams{OrgID: sandboxOrgID})
	if err := add("sandbox/execution_sticky_disk_mounts.jsonl", rows, err); err != nil {
		return nil, err
	}
	rows, err = q.ExportSandboxVMResourceBoundsJSONL(ctx, sandboxexport.ExportSandboxVMResourceBoundsJSONLParams{OrgID: sandboxOrgID})
	if err := add("sandbox/vm_resource_bounds.jsonl", rows, err); err != nil {
		return nil, err
	}
	if includeLogs {
		rows, err = q.ExportSandboxExecutionLogsJSONL(ctx, sandboxexport.ExportSandboxExecutionLogsJSONLParams{OrgID: sandboxOrgID})
		if err := add("sandbox/execution_logs.jsonl", rows, err); err != nil {
			return nil, err
		}
	}
	return files, nil
}

func (s *Service) auditExportFiles(ctx context.Context, orgID string) ([]exportArtifactFile, error) {
	rows, err := s.CH.Query(ctx, auditEventExportSelectSQL()+`
		FROM verself.audit_events AS e
		ANY LEFT JOIN verself.audit_event_details AS d
			ON e.org_id = d.org_id AND e.event_date = d.event_date AND e.event_id = d.event_id
		WHERE e.org_id = $1
		ORDER BY e.recorded_at, e.sequence
	`, orgID)
	if err != nil {
		return nil, fmt.Errorf("%w: query audit export: %v", ErrStore, err)
	}
	defer func() { _ = rows.Close() }()
	var body bytes.Buffer
	var count int64
	for rows.Next() {
		var event AuditEvent
		detailJSON, err := scanAuditEventExportRow(rows, &event)
		if err != nil {
			return nil, fmt.Errorf("%w: scan audit export row: %v", ErrStore, err)
		}
		raw, err := json.Marshal(auditEventExportRow(event, detailJSON))
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

type auditRowScanner interface {
	Scan(dest ...any) error
}

func (s *Service) billingInvoicesCSV(ctx context.Context, orgID string) (exportArtifactFile, error) {
	rows, err := billingexport.New(s.BillingPG).ListBillingInvoiceExportRows(ctx, billingexport.ListBillingInvoiceExportRowsParams{OrgID: orgID})
	if err != nil {
		return exportArtifactFile{}, fmt.Errorf("%w: query invoices csv: %v", ErrStore, err)
	}
	var body bytes.Buffer
	writer := csv.NewWriter(&body)
	header := []string{"document_id", "document_number", "document_kind", "status", "payment_status", "period_start", "period_end", "issued_at", "currency", "subtotal_units", "adjustment_units", "tax_units", "total_due_units", "recipient_email", "hosted_invoice_url", "invoice_pdf_url"}
	if err := writer.Write(header); err != nil {
		return exportArtifactFile{}, fmt.Errorf("%w: write invoices csv header: %v", ErrStore, err)
	}
	var count int64
	for _, row := range rows {
		record := []string{
			row.DocumentID,
			row.DocumentNumber,
			row.DocumentKind,
			row.Status,
			row.PaymentStatus,
			exportPGTime(row.PeriodStart),
			exportPGTime(row.PeriodEnd),
			exportPGTime(row.IssuedAt),
			row.Currency,
			strconv.FormatInt(row.SubtotalUnits, 10),
			strconv.FormatInt(row.AdjustmentUnits, 10),
			strconv.FormatInt(row.TaxUnits, 10),
			strconv.FormatInt(row.TotalDueUnits, 10),
			row.RecipientEmail,
			exportPGText(row.StripeHostedInvoiceUrl),
			exportPGText(row.StripeInvoicePdfUrl),
		}
		if err := writer.Write(record); err != nil {
			return exportArtifactFile{}, fmt.Errorf("%w: write invoices csv row: %v", ErrStore, err)
		}
		count++
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		return exportArtifactFile{}, fmt.Errorf("%w: flush invoices csv: %v", ErrStore, err)
	}
	return newArtifactFile("billing/invoices.csv", "text/csv", body.Bytes(), count), nil
}

func jsonlArtifactFile(path string, rows []string) exportArtifactFile {
	var body bytes.Buffer
	for _, raw := range rows {
		body.WriteString(raw)
		body.WriteByte('\n')
	}
	return newArtifactFile(path, "application/x-ndjson", body.Bytes(), int64(len(rows)))
}

func exportPGText(value pgtype.Text) string {
	if !value.Valid {
		return ""
	}
	return value.String
}

func exportPGTime(value pgtype.Timestamptz) string {
	if !value.Valid {
		return ""
	}
	return value.Time.UTC().Format(time.RFC3339)
}

func auditEventExportSelectSQL() string {
	return `
		SELECT
			e.recorded_at, e.event_date, e.schema_version, e.event_id, e.org_id, e.sequence,
			e.event_source, e.event_name, e.audit_event,
			e.actor_type, e.actor_id, e.credential_id,
			e.target_type, e.target_id, e.permission,
			e.outcome, e.error_code, e.trace_id, e.detail_sha256,
			e.prev_hmac, e.row_hmac, e.hmac_key_id,
			ifNull(d.detail_json, '') AS detail_json
	`
}

func scanAuditEventExportRow(rows auditRowScanner, event *AuditEvent) (string, error) {
	var detailJSON string
	err := rows.Scan(
		&event.RecordedAt,
		&event.EventDate,
		&event.SchemaVersion,
		&event.EventID,
		&event.OrgID,
		&event.Sequence,
		&event.EventSource,
		&event.EventName,
		&event.AuditEvent,
		&event.ActorType,
		&event.ActorID,
		&event.CredentialID,
		&event.TargetType,
		&event.TargetID,
		&event.Permission,
		&event.Outcome,
		&event.ErrorCode,
		&event.TraceID,
		&event.DetailSHA256,
		&event.PrevHMAC,
		&event.RowHMAC,
		&event.HMACKeyID,
		&detailJSON,
	)
	return detailJSON, err
}

func auditEventExportRow(event AuditEvent, detailJSON string) map[string]any {
	row := map[string]any{
		"schema_version": event.SchemaVersion,
		"event_id":       event.EventID.String(),
		"recorded_at":    event.RecordedAt.UTC().Format(time.RFC3339Nano),
		"event_date":     event.EventDate.UTC().Format("2006-01-02"),
		"org_id":         event.OrgID,
		"sequence":       strconv.FormatUint(event.Sequence, 10),
		"event_source":   event.EventSource,
		"event_name":     event.EventName,
		"audit_event":    event.AuditEvent,
		"actor_type":     event.ActorType,
		"actor_id":       event.ActorID,
		"credential_id":  event.CredentialID,
		"target_type":    event.TargetType,
		"target_id":      event.TargetID,
		"permission":     event.Permission,
		"outcome":        event.Outcome,
		"error_code":     event.ErrorCode,
		"trace_id":       event.TraceID,
		"detail_sha256":  event.DetailSHA256,
		"prev_hmac":      event.PrevHMAC,
		"row_hmac":       event.RowHMAC,
		"hmac_key_id":    event.HMACKeyID,
	}
	if hasAuditDetail(detailJSON) {
		detail := json.RawMessage(detailJSON)
		if json.Valid(detail) {
			row["detail"] = detail
		} else {
			row["detail_json"] = detailJSON
		}
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
	defer func() { _ = f.Close() }()
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
	return fmt.Sprintf(`Verself organization export

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
