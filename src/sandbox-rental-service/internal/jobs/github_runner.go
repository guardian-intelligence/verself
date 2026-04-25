package jobs

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/verself/apiwire"
	"github.com/verself/sandbox-rental-service/internal/scheduler"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
)

var (
	ErrGitHubRunnerNotConfigured      = errors.New("github runner is not configured")
	ErrGitHubInstallationInvalid      = errors.New("github installation is invalid")
	ErrGitHubInstallationStateInvalid = errors.New("github installation state is invalid")
	ErrGitHubJITConfigMissing         = errors.New("github runner jit config is missing")
	ErrGitHubWebhookSignatureInvalid  = errors.New("github webhook signature invalid")
)

const (
	githubAPIVersion         = "2022-11-28"
	githubRunnerWorkFolder   = "_work"
	githubJITConfigFetchPath = "/internal/sandbox/v1/github-runner-jit"
	githubStickyDiskPath     = "/internal/sandbox/v1/stickydisk"
	githubCheckoutPath       = "/internal/sandbox/v1/github-checkout"
)

type GitHubRunnerConfig struct {
	AppID         int64
	AppSlug       string
	ClientID      string
	ClientSecret  string
	PrivateKeyPEM string
	WebhookSecret string
	APIBaseURL    string
	WebBaseURL    string
	RunnerGroupID int64
}

type GitHubRunner struct {
	service *Service
	cfg     GitHubRunnerConfig
	client  *http.Client

	tokenMu sync.Mutex
	tokens  map[int64]githubInstallationToken

	checkoutMu sync.Mutex
}

type GitHubInstallationConnect struct {
	State     string
	SetupURL  string
	ExpiresAt time.Time
}

type GitHubInstallationRecord struct {
	InstallationID int64
	OrgID          uint64
	AccountLogin   string
	AccountType    string
	Active         bool
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type GitHubWorkflowJobWebhook struct {
	Action       string `json:"action"`
	Installation struct {
		ID int64 `json:"id"`
	} `json:"installation"`
	Organization struct {
		Login string `json:"login"`
	} `json:"organization"`
	Repository struct {
		ID       int64  `json:"id"`
		FullName string `json:"full_name"`
		HTMLURL  string `json:"html_url"`
	} `json:"repository"`
	WorkflowJob struct {
		ID           int64     `json:"id"`
		RunID        int64     `json:"run_id"`
		Name         string    `json:"name"`
		Status       string    `json:"status"`
		Conclusion   string    `json:"conclusion"`
		Labels       []string  `json:"labels"`
		RunnerID     int64     `json:"runner_id"`
		RunnerName   string    `json:"runner_name"`
		HeadSHA      string    `json:"head_sha"`
		HeadBranch   string    `json:"head_branch"`
		WorkflowName string    `json:"workflow_name"`
		StartedAt    time.Time `json:"started_at"`
		CompletedAt  time.Time `json:"completed_at"`
	} `json:"workflow_job"`
}

type githubInstallationToken struct {
	Token     string
	ExpiresAt time.Time
}

type githubInstallationResponse struct {
	Account struct {
		Login string `json:"login"`
		Type  string `json:"type"`
	} `json:"account"`
}

type githubAccessTokenResponse struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
}

type githubJITConfigResponse struct {
	Runner struct {
		ID     int64  `json:"id"`
		Name   string `json:"name"`
		OS     string `json:"os"`
		Status string `json:"status"`
		Busy   bool   `json:"busy"`
	} `json:"runner"`
	EncodedJITConfig string `json:"encoded_jit_config"`
}

type githubRunnerListResponse struct {
	TotalCount int `json:"total_count"`
	Runners    []struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
	} `json:"runners"`
}

type githubQueuedJob struct {
	GitHubJobID        int64
	InstallationID     int64
	RepositoryID       int64
	RepositoryFullName string
	RunID              int64
	JobName            string
	HeadSHA            string
	HeadBranch         string
	Labels             []string
	OrgID              uint64
	AccountLogin       string
}

type githubAllocation struct {
	AllocationID       uuid.UUID
	InstallationID     int64
	RepositoryID       int64
	RunnerClass        string
	RunnerName         string
	GitHubRunnerID     int64
	RequestedJobID     int64
	RunID              int64
	JobName            string
	HeadSHA            string
	HeadBranch         string
	ExecutionID        uuid.UUID
	AttemptID          uuid.UUID
	State              string
	OrgID              uint64
	AccountLogin       string
	RepositoryFullName string
	Resources          apiwire.VMResources
	ProductID          string
}

func NewGitHubRunner(service *Service, cfg GitHubRunnerConfig, client *http.Client) (*GitHubRunner, error) {
	if client == nil {
		client = http.DefaultClient
	}
	return &GitHubRunner{service: service, cfg: cfg, client: client, tokens: map[int64]githubInstallationToken{}}, nil
}

func (r *GitHubRunner) Configured() bool {
	return r != nil &&
		r.cfg.AppID != 0 &&
		strings.TrimSpace(r.cfg.AppSlug) != "" &&
		strings.TrimSpace(r.cfg.ClientID) != "" &&
		strings.TrimSpace(r.cfg.PrivateKeyPEM) != "" &&
		strings.TrimSpace(r.cfg.WebhookSecret) != ""
}

func (r *GitHubRunner) BeginInstallation(ctx context.Context, orgID uint64, actorID string) (GitHubInstallationConnect, error) {
	if !r.Configured() {
		return GitHubInstallationConnect{}, ErrGitHubRunnerNotConfigured
	}
	stateBytes := make([]byte, 32)
	if _, err := rand.Read(stateBytes); err != nil {
		return GitHubInstallationConnect{}, err
	}
	state := base64.RawURLEncoding.EncodeToString(stateBytes)
	expiresAt := time.Now().UTC().Add(10 * time.Minute)
	if r.service != nil && r.service.PGX != nil {
		_, _ = r.service.PGX.Exec(ctx, `INSERT INTO github_installation_states (state, org_id, actor_id, expires_at, created_at) VALUES ($1,$2,$3,$4,$5) ON CONFLICT (state) DO NOTHING`, state, orgID, actorID, expiresAt, time.Now().UTC())
	}
	webBase := strings.TrimRight(firstNonEmpty(r.cfg.WebBaseURL, "https://github.com"), "/")
	return GitHubInstallationConnect{
		State:     state,
		SetupURL:  fmt.Sprintf("%s/apps/%s/installations/new?state=%s", webBase, r.cfg.AppSlug, state),
		ExpiresAt: expiresAt,
	}, nil
}

func (s *Service) ListGitHubInstallations(ctx context.Context, orgID uint64) ([]GitHubInstallationRecord, error) {
	if s.PGX == nil {
		return nil, nil
	}
	rows, err := s.PGX.Query(ctx, `SELECT installation_id, org_id, account_login, account_type, active, created_at, updated_at FROM github_installations WHERE org_id = $1 ORDER BY updated_at DESC`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []GitHubInstallationRecord{}
	for rows.Next() {
		var row GitHubInstallationRecord
		if err := rows.Scan(&row.InstallationID, &row.OrgID, &row.AccountLogin, &row.AccountType, &row.Active, &row.CreatedAt, &row.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func (r *GitHubRunner) CompleteInstallation(ctx context.Context, state, code string, installationID int64) (GitHubInstallationRecord, error) {
	_ = strings.TrimSpace(code)
	if !r.Configured() {
		return GitHubInstallationRecord{}, ErrGitHubRunnerNotConfigured
	}
	state = strings.TrimSpace(state)
	if state == "" || installationID <= 0 {
		return GitHubInstallationRecord{}, ErrGitHubInstallationInvalid
	}
	if r.service == nil || r.service.PGX == nil {
		return GitHubInstallationRecord{}, ErrGitHubInstallationInvalid
	}

	installation, err := r.fetchInstallation(ctx, installationID)
	if err != nil {
		return GitHubInstallationRecord{}, err
	}
	if !strings.EqualFold(installation.Account.Type, "Organization") {
		return GitHubInstallationRecord{}, ErrGitHubInstallationInvalid
	}

	tx, err := r.service.PGX.Begin(ctx)
	if err != nil {
		return GitHubInstallationRecord{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var (
		orgID   uint64
		actorID string
		expires time.Time
	)
	if err := tx.QueryRow(ctx, `SELECT org_id, actor_id, expires_at FROM github_installation_states WHERE state = $1 FOR UPDATE`, state).Scan(&orgID, &actorID, &expires); err != nil {
		return GitHubInstallationRecord{}, ErrGitHubInstallationStateInvalid
	}
	_ = actorID
	if time.Now().UTC().After(expires) {
		return GitHubInstallationRecord{}, ErrGitHubInstallationStateInvalid
	}
	now := time.Now().UTC()
	var record GitHubInstallationRecord
	if err := tx.QueryRow(ctx, `INSERT INTO github_installations (
		installation_id, org_id, account_login, account_type, active, created_at, updated_at
	) VALUES ($1,$2,$3,$4,true,$5,$5)
	ON CONFLICT (installation_id) DO UPDATE SET
		org_id = EXCLUDED.org_id,
		account_login = EXCLUDED.account_login,
		account_type = EXCLUDED.account_type,
		active = true,
		updated_at = EXCLUDED.updated_at
	RETURNING installation_id, org_id, account_login, account_type, active, created_at, updated_at`,
		installationID, orgID, installation.Account.Login, installation.Account.Type, now).Scan(&record.InstallationID, &record.OrgID, &record.AccountLogin, &record.AccountType, &record.Active, &record.CreatedAt, &record.UpdatedAt); err != nil {
		return GitHubInstallationRecord{}, err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM github_installation_states WHERE state = $1`, state); err != nil {
		return GitHubInstallationRecord{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return GitHubInstallationRecord{}, err
	}
	return record, nil
}

func (r *GitHubRunner) VerifyWebhookSignature(payload []byte, signature string) bool {
	return r != nil && verifyGitHubSignature(r.cfg.WebhookSecret, payload, signature) == nil
}

func (r *GitHubRunner) HandleWebhook(ctx context.Context, eventName string, deliveryID string, payload []byte, signature string) error {
	if !r.Configured() {
		return ErrGitHubRunnerNotConfigured
	}
	if err := verifyGitHubSignature(r.cfg.WebhookSecret, payload, signature); err != nil {
		return err
	}
	if eventName != "workflow_job" {
		return nil
	}
	ctx, span := tracer.Start(ctx, "github.webhook.workflow_job")
	defer span.End()

	var event GitHubWorkflowJobWebhook
	if err := json.Unmarshal(payload, &event); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	if r.service == nil || r.service.PGX == nil {
		return nil
	}
	status := firstNonEmpty(event.WorkflowJob.Status, event.Action)
	labels, _ := json.Marshal(event.WorkflowJob.Labels)
	now := time.Now().UTC()
	tx, err := r.service.PGX.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	_, err = tx.Exec(ctx, `INSERT INTO runner_jobs (
		provider, provider_job_id, provider_installation_id, provider_repository_id, repository_full_name, provider_run_id, job_name, head_sha, head_branch, workflow_name,
		status, conclusion, labels_json, runner_id, runner_name, started_at, completed_at, last_webhook_delivery, updated_at
	) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19)
	ON CONFLICT (provider, provider_job_id) DO UPDATE SET
		job_name = EXCLUDED.job_name,
		head_sha = COALESCE(NULLIF(EXCLUDED.head_sha, ''), runner_jobs.head_sha),
		head_branch = COALESCE(NULLIF(EXCLUDED.head_branch, ''), runner_jobs.head_branch),
		workflow_name = COALESCE(NULLIF(EXCLUDED.workflow_name, ''), runner_jobs.workflow_name),
		status = EXCLUDED.status,
		conclusion = EXCLUDED.conclusion,
		labels_json = EXCLUDED.labels_json,
		runner_id = EXCLUDED.runner_id,
		runner_name = EXCLUDED.runner_name,
		started_at = COALESCE(EXCLUDED.started_at, runner_jobs.started_at),
		completed_at = COALESCE(EXCLUDED.completed_at, runner_jobs.completed_at),
		last_webhook_delivery = EXCLUDED.last_webhook_delivery,
		updated_at = EXCLUDED.updated_at`,
		RunnerProviderGitHub, event.WorkflowJob.ID, event.Installation.ID, event.Repository.ID, event.Repository.FullName, event.WorkflowJob.RunID, event.WorkflowJob.Name, event.WorkflowJob.HeadSHA, event.WorkflowJob.HeadBranch, event.WorkflowJob.WorkflowName,
		status, event.WorkflowJob.Conclusion, string(labels), event.WorkflowJob.RunnerID, event.WorkflowJob.RunnerName,
		nullableTime(event.WorkflowJob.StartedAt), nullableTime(event.WorkflowJob.CompletedAt), deliveryID, now)
	if err != nil {
		return err
	}
	switch event.Action {
	case "queued":
		if r.service.Scheduler != nil {
			_, err = r.service.Scheduler.EnqueueRunnerCapacityReconcileTx(ctx, tx, scheduler.RunnerCapacityReconcileRequest{
				Provider:               RunnerProviderGitHub,
				ProviderInstallationID: event.Installation.ID,
				ProviderRepositoryID:   event.Repository.ID,
				ProviderJobID:          event.WorkflowJob.ID,
				CorrelationID:          deliveryID,
				TraceParent:            traceParent(ctx),
			})
		}
	case "in_progress", "completed":
		if r.service.Scheduler != nil {
			_, err = r.service.Scheduler.EnqueueRunnerJobBindTx(ctx, tx, scheduler.RunnerJobBindRequest{
				Provider:      RunnerProviderGitHub,
				ProviderJobID: event.WorkflowJob.ID,
				CorrelationID: deliveryID,
				TraceParent:   traceParent(ctx),
			})
		}
	}
	if err != nil {
		return err
	}
	span.SetAttributes(
		attribute.Int64("github.installation_id", event.Installation.ID),
		attribute.Int64("github.repository_id", event.Repository.ID),
		attribute.Int64("github.job_id", event.WorkflowJob.ID),
		attribute.String("github.workflow_job.action", event.Action),
		attribute.String("github.workflow_job.status", status),
	)
	return tx.Commit(ctx)
}

func (r *GitHubRunner) ReconcileCapacity(ctx context.Context, githubJobID int64) error {
	ctx, span := tracer.Start(ctx, "github.capacity.reconcile")
	defer span.End()
	span.SetAttributes(traceInt64("github.job_id", githubJobID))
	job, err := r.loadQueuedJob(ctx, githubJobID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			span.SetAttributes(attribute.Bool("github.capacity.noop", true))
			return nil
		}
		return err
	}
	runnerClass, resources, productID, err := r.runnerClassForLabels(ctx, job.Labels)
	if err != nil {
		return err
	}
	if runnerClass == "" {
		span.SetAttributes(attribute.Bool("github.capacity.no_matching_runner_class", true))
		return nil
	}
	existing, err := r.activeAllocationForJob(ctx, job.GitHubJobID)
	if err != nil {
		return err
	}
	if existing != uuid.Nil {
		span.SetAttributes(attribute.String("github.allocation_id", existing.String()), attribute.Bool("github.capacity.existing_allocation", true))
		return nil
	}
	allocationID := uuid.New()
	runnerName := githubRunnerName(job.GitHubJobID, allocationID)
	now := time.Now().UTC()
	tx, err := r.service.PGX.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `INSERT INTO runner_allocations (
		allocation_id, provider, provider_installation_id, provider_repository_id, runner_class, runner_name, state,
		requested_for_provider_job_id, allocate_by, jit_by, vm_submitted_by, runner_listening_by,
		assignment_by, vm_exit_by, cleanup_by, created_at, updated_at
	) VALUES ($1,$2,$3,$4,$5,$6,'pending',$7,$8,$9,$10,$11,$12,$13,$14,$15,$15)`,
		allocationID, RunnerProviderGitHub, job.InstallationID, job.RepositoryID, runnerClass, runnerName, job.GitHubJobID,
		now.Add(30*time.Second), now.Add(time.Minute), now.Add(2*time.Minute), now.Add(5*time.Minute),
		now.Add(10*time.Minute), now.Add(3*time.Hour), now.Add(3*time.Hour), now); err != nil {
		return err
	}
	if _, err := r.service.Scheduler.EnqueueRunnerAllocateTx(ctx, tx, scheduler.RunnerAllocateRequest{
		AllocationID:  allocationID.String(),
		CorrelationID: CorrelationIDFromContext(ctx),
		TraceParent:   traceParent(ctx),
	}); err != nil {
		return err
	}
	span.SetAttributes(
		attribute.String("github.allocation_id", allocationID.String()),
		attribute.String("github.runner_class", runnerClass),
		attribute.Int("vmresources.vcpus", int(resources.VCPUs)),
		attribute.Int("vmresources.memory_mib", int(resources.MemoryMiB)),
		attribute.Int("vmresources.root_disk_gib", int(resources.RootDiskGiB)),
		attribute.String("billing.product_id", productID),
	)
	return tx.Commit(ctx)
}

func (r *GitHubRunner) AllocateRunner(ctx context.Context, allocationID uuid.UUID) error {
	ctx, span := tracer.Start(ctx, "github.runner.allocate")
	defer span.End()
	span.SetAttributes(attribute.String("github.allocation_id", allocationID.String()))
	allocation, err := r.loadAllocation(ctx, allocationID)
	if err != nil {
		return err
	}
	if allocation.State == "vm_submitted" || allocation.State == "assigned" || allocation.State == "cleaned" {
		return nil
	}
	attemptID := uuid.New()
	stickyMounts, err := r.prepareStickyDiskMounts(ctx, allocation, attemptID)
	if err != nil {
		failureReason := "sticky_disk_compile_failed"
		if errors.Is(err, ErrGitHubWorkflowContentsPermission) {
			failureReason = "github_app_contents_permission_required"
		}
		_ = r.setAllocationState(ctx, allocationID, "failed", failureReason)
		return err
	}
	if err := r.setAllocationState(ctx, allocationID, "jit_creating", ""); err != nil {
		return err
	}
	jit, err := r.createJITConfig(ctx, allocation.InstallationID, allocation.AccountLogin, allocation.RunnerName, allocation.RunnerClass)
	if err != nil {
		_ = r.setAllocationState(ctx, allocationID, "failed", "jit_config_failed")
		return err
	}
	runnerID := jit.Runner.ID
	if runnerID == 0 {
		_ = r.setAllocationState(ctx, allocationID, "failed", "jit_config_missing_runner_id")
		return fmt.Errorf("github jit config missing runner id")
	}
	if _, err := r.service.PGX.Exec(ctx, `UPDATE runner_allocations SET provider_runner_id = $1, runner_name = $2, state = 'jit_created', updated_at = $3 WHERE allocation_id = $4`, runnerID, firstNonEmpty(jit.Runner.Name, allocation.RunnerName), time.Now().UTC(), allocationID); err != nil {
		return err
	}

	executionID, attemptID, err := r.service.Submit(WithCorrelationID(ctx, CorrelationIDFromContext(ctx)), allocation.OrgID, fmt.Sprintf("github-app:%d", allocation.InstallationID), SubmitRequest{
		Kind:                   KindDirect,
		SourceKind:             SourceKindGitHubAction,
		WorkloadKind:           WorkloadKindRunner,
		SourceRef:              allocation.RepositoryFullName,
		RunnerClass:            allocation.RunnerClass,
		ExternalProvider:       RunnerProviderGitHub,
		ExternalTaskID:         strconv.FormatInt(allocation.RequestedJobID, 10),
		Provider:               RunnerProviderGitHub,
		ProductID:              allocation.ProductID,
		IdempotencyKey:         "github-runner:" + allocationID.String(),
		RunCommand:             githubRunnerCommand(),
		MaxWallSeconds:         uint64((3 * time.Hour).Seconds()),
		Resources:              allocation.Resources,
		AttemptID:              attemptID,
		StickyDiskMounts:       stickyMounts,
		RunnerAllocationID:     allocationID,
		RunnerBootstrapKind:    RunnerBootstrapGitHubJIT,
		RunnerBootstrapPayload: jit.EncodedJITConfig,
	})
	if err != nil {
		_ = r.deleteRunner(ctx, allocation.InstallationID, allocation.AccountLogin, runnerID)
		_ = r.setAllocationState(ctx, allocationID, "failed", "execution_submit_failed")
		return err
	}
	span.SetAttributes(
		attribute.Int64("github.runner_id", runnerID),
		attribute.String("github.runner_name", firstNonEmpty(jit.Runner.Name, allocation.RunnerName)),
		attribute.String("execution.id", executionID.String()),
		attribute.String("attempt.id", attemptID.String()),
	)
	return nil
}

func (r *GitHubRunner) BindJob(ctx context.Context, githubJobID int64) error {
	ctx, span := tracer.Start(ctx, "github.job.bind")
	defer span.End()
	span.SetAttributes(traceInt64("github.job_id", githubJobID))
	job, err := r.loadJobForBinding(ctx, githubJobID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return err
	}
	if job.runnerID == 0 && job.runnerName == "" {
		span.SetAttributes(attribute.Bool("github.job.no_runner_identity", true))
		return nil
	}
	allocationID, err := r.findAllocationForRunner(ctx, job.runnerID, job.runnerName)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			span.SetAttributes(attribute.Bool("github.job.unmatched_runner", true))
			return nil
		}
		return err
	}
	now := time.Now().UTC()
	tx, err := r.service.PGX.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `INSERT INTO runner_job_bindings (
		binding_id, allocation_id, provider, provider_job_id, provider_runner_id, runner_name, bound_at, created_at
	) VALUES ($1,$2,$3,$4,$5,$6,$7,$7)
	ON CONFLICT (provider, provider_job_id) DO NOTHING`, uuid.New(), allocationID, RunnerProviderGitHub, githubJobID, job.runnerID, job.runnerName, now); err != nil {
		return err
	}
	state := "assigned"
	if job.status == "completed" {
		state = "job_completed"
	}
	if _, err := tx.Exec(ctx, `UPDATE runner_allocations SET state = $1, assignment_by = COALESCE(assignment_by, $2), cleanup_by = $3, updated_at = $2 WHERE allocation_id = $4 AND state <> 'cleaned'`, state, now, now.Add(30*time.Minute), allocationID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE executions e
		SET external_task_id = $1,
		    updated_at = $2
		FROM runner_allocations a
		WHERE a.allocation_id = $3
		  AND a.execution_id = e.execution_id
		  AND e.workload_kind = 'runner'`, strconv.FormatInt(githubJobID, 10), now, allocationID); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	span.SetAttributes(attribute.String("github.allocation_id", allocationID.String()), attribute.String("github.workflow_job.status", job.status))
	if job.status == "completed" && r.service.Scheduler != nil {
		_, _ = r.service.Scheduler.EnqueueRunnerCleanup(ctx, scheduler.RunnerCleanupRequest{
			AllocationID:  allocationID.String(),
			CorrelationID: CorrelationIDFromContext(ctx),
			TraceParent:   traceParent(ctx),
		})
	}
	return nil
}

func (r *GitHubRunner) CleanupRunner(ctx context.Context, allocationID uuid.UUID) error {
	ctx, span := tracer.Start(ctx, "github.runner.cleanup")
	defer span.End()
	span.SetAttributes(attribute.String("github.allocation_id", allocationID.String()))
	allocation, err := r.loadAllocation(ctx, allocationID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return err
	}
	if allocation.State == "cleaned" {
		return nil
	}
	if allocation.GitHubRunnerID != 0 {
		if err := r.deleteRunner(ctx, allocation.InstallationID, allocation.AccountLogin, allocation.GitHubRunnerID); err != nil {
			return err
		}
	} else if allocation.RunnerName != "" {
		if err := r.deleteRunnerByName(ctx, allocation.InstallationID, allocation.AccountLogin, allocation.RunnerName); err != nil {
			return err
		}
	}
	now := time.Now().UTC()
	_, err = r.service.PGX.Exec(ctx, `UPDATE runner_allocations SET state = 'cleaned', cleanup_by = $1, updated_at = $1 WHERE allocation_id = $2`, now, allocationID)
	if err != nil {
		return err
	}
	_, _ = r.service.PGX.Exec(ctx, `DELETE FROM runner_bootstrap_configs WHERE allocation_id = $1`, allocationID)
	return nil
}

func (r *GitHubRunner) execEnv(ctx context.Context, executionID, attemptID uuid.UUID) map[string]string {
	var allocationID uuid.UUID
	if err := r.service.PGX.QueryRow(ctx, `SELECT allocation_id FROM runner_allocations WHERE provider = 'github' AND execution_id = $1`, executionID).Scan(&allocationID); err != nil {
		return nil
	}
	return map[string]string{
		"VERSELF_GITHUB_JIT_TOKEN": r.deriveJITFetchToken(allocationID, attemptID),
		"VERSELF_GITHUB_JIT_PATH":  githubJITConfigFetchPath,
		"VERSELF_STICKY_TOKEN":     r.deriveStickyDiskToken(executionID, attemptID),
		"VERSELF_STICKY_PATH":      githubStickyDiskPath,
		"VERSELF_CHECKOUT_TOKEN":   r.deriveCheckoutToken(executionID, attemptID),
		"VERSELF_CHECKOUT_PATH":    githubCheckoutPath,
	}
}

func (r *GitHubRunner) ConsumeJITConfig(ctx context.Context, token string) (string, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return "", ErrGitHubJITConfigMissing
	}
	config, err := r.service.ConsumeRunnerBootstrapConfig(ctx, token, RunnerBootstrapGitHubJIT)
	if err != nil {
		return "", ErrGitHubJITConfigMissing
	}
	return config, nil
}

func (r *GitHubRunner) loadQueuedJob(ctx context.Context, githubJobID int64) (githubQueuedJob, error) {
	row := r.service.PGX.QueryRow(ctx, `SELECT
		j.provider_job_id, j.provider_installation_id, j.provider_repository_id, j.repository_full_name, j.provider_run_id, j.job_name, j.head_sha, j.head_branch, j.labels_json,
		i.org_id, i.account_login
		FROM runner_jobs j
		JOIN github_installations i ON i.installation_id = j.provider_installation_id AND i.active
		WHERE j.provider = 'github' AND j.provider_job_id = $1 AND j.status = 'queued'`, githubJobID)
	var job githubQueuedJob
	var labelsRaw []byte
	if err := row.Scan(&job.GitHubJobID, &job.InstallationID, &job.RepositoryID, &job.RepositoryFullName, &job.RunID, &job.JobName, &job.HeadSHA, &job.HeadBranch, &labelsRaw, &job.OrgID, &job.AccountLogin); err != nil {
		return githubQueuedJob{}, err
	}
	_ = json.Unmarshal(labelsRaw, &job.Labels)
	return job, nil
}

func (r *GitHubRunner) runnerClassForLabels(ctx context.Context, labels []string) (string, apiwire.VMResources, string, error) {
	for _, label := range labels {
		label = strings.TrimSpace(label)
		if label == "" {
			continue
		}
		resources, productID, ok, err := r.service.runnerClassResources(ctx, label)
		if err != nil {
			return "", apiwire.VMResources{}, "", err
		}
		if !ok {
			continue
		}
		return label, resources, productID, nil
	}
	return "", apiwire.VMResources{}, "", nil
}

func (r *GitHubRunner) activeAllocationForJob(ctx context.Context, githubJobID int64) (uuid.UUID, error) {
	var allocationID uuid.UUID
	err := r.service.PGX.QueryRow(ctx, `SELECT allocation_id FROM runner_allocations
		WHERE provider = 'github'
		  AND requested_for_provider_job_id = $1
		  AND state NOT IN ('failed', 'cleaned')
		ORDER BY created_at DESC
		LIMIT 1`, githubJobID).Scan(&allocationID)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, nil
	}
	return allocationID, err
}

func (r *GitHubRunner) loadAllocation(ctx context.Context, allocationID uuid.UUID) (githubAllocation, error) {
	row := r.service.PGX.QueryRow(ctx, `SELECT
		a.allocation_id, a.provider_installation_id, a.provider_repository_id, a.runner_class, a.runner_name,
		a.provider_runner_id, a.requested_for_provider_job_id, COALESCE(j.provider_run_id, 0), COALESCE(j.job_name, ''), COALESCE(j.head_sha, ''), COALESCE(j.head_branch, ''), COALESCE(a.execution_id, '00000000-0000-0000-0000-000000000000'::uuid),
		COALESCE(a.attempt_id, '00000000-0000-0000-0000-000000000000'::uuid), a.state,
		i.org_id, i.account_login, COALESCE(j.repository_full_name, ''),
		c.product_id, c.vcpus, c.memory_mib, c.rootfs_gib
		FROM runner_allocations a
		JOIN github_installations i ON i.installation_id = a.provider_installation_id
		JOIN runner_classes c ON c.runner_class = a.runner_class
		LEFT JOIN runner_jobs j ON j.provider = a.provider AND j.provider_job_id = a.requested_for_provider_job_id
		WHERE a.provider = 'github' AND a.allocation_id = $1`, allocationID)
	var out githubAllocation
	var vcpus, memoryMiB, rootfsGiB int
	if err := row.Scan(&out.AllocationID, &out.InstallationID, &out.RepositoryID, &out.RunnerClass, &out.RunnerName, &out.GitHubRunnerID, &out.RequestedJobID, &out.RunID, &out.JobName, &out.HeadSHA, &out.HeadBranch, &out.ExecutionID, &out.AttemptID, &out.State, &out.OrgID, &out.AccountLogin, &out.RepositoryFullName, &out.ProductID, &vcpus, &memoryMiB, &rootfsGiB); err != nil {
		return githubAllocation{}, err
	}
	out.Resources = apiwire.VMResources{VCPUs: uint32(vcpus), MemoryMiB: uint32(memoryMiB), RootDiskGiB: uint32(rootfsGiB), KernelImage: apiwire.KernelImageDefault}
	return out, nil
}

func (r *GitHubRunner) setAllocationState(ctx context.Context, allocationID uuid.UUID, state, reason string) error {
	_, err := r.service.PGX.Exec(ctx, `UPDATE runner_allocations SET state = $1, failure_reason = $2, updated_at = $3 WHERE provider = 'github' AND allocation_id = $4`, state, strings.TrimSpace(reason), time.Now().UTC(), allocationID)
	return err
}

func (r *GitHubRunner) loadJobForBinding(ctx context.Context, githubJobID int64) (struct {
	runnerID   int64
	runnerName string
	status     string
}, error,
) {
	var out struct {
		runnerID   int64
		runnerName string
		status     string
	}
	err := r.service.PGX.QueryRow(ctx, `SELECT runner_id, runner_name, status FROM runner_jobs WHERE provider = 'github' AND provider_job_id = $1`, githubJobID).Scan(&out.runnerID, &out.runnerName, &out.status)
	return out, err
}

func (r *GitHubRunner) findAllocationForRunner(ctx context.Context, runnerID int64, runnerName string) (uuid.UUID, error) {
	var allocationID uuid.UUID
	err := r.service.PGX.QueryRow(ctx, `SELECT allocation_id FROM runner_allocations
		WHERE provider = 'github'
		  AND (($1::bigint <> 0 AND provider_runner_id = $1)
		   OR ($2::text <> '' AND runner_name = $2))
		ORDER BY created_at DESC
		LIMIT 1`, runnerID, runnerName).Scan(&allocationID)
	return allocationID, err
}

func (r *GitHubRunner) createAppJWT() (string, error) {
	key, err := jwt.ParseRSAPrivateKeyFromPEM([]byte(r.cfg.PrivateKeyPEM))
	if err != nil {
		return "", fmt.Errorf("parse github app private key: %w", err)
	}
	now := time.Now().UTC()
	claims := jwt.RegisteredClaims{
		Issuer:    strconv.FormatInt(r.cfg.AppID, 10),
		IssuedAt:  jwt.NewNumericDate(now.Add(-60 * time.Second)),
		ExpiresAt: jwt.NewNumericDate(now.Add(9 * time.Minute)),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	return token.SignedString(key)
}

func (r *GitHubRunner) installationToken(ctx context.Context, installationID int64) (string, error) {
	r.tokenMu.Lock()
	cached, ok := r.tokens[installationID]
	r.tokenMu.Unlock()
	if ok && time.Now().UTC().Before(cached.ExpiresAt.Add(-time.Minute)) {
		return cached.Token, nil
	}
	appJWT, err := r.createAppJWT()
	if err != nil {
		return "", err
	}
	var resp githubAccessTokenResponse
	if err := r.githubRequest(ctx, http.MethodPost, fmt.Sprintf("/app/installations/%d/access_tokens", installationID), appJWT, nil, &resp, http.StatusCreated); err != nil {
		return "", err
	}
	if strings.TrimSpace(resp.Token) == "" {
		return "", fmt.Errorf("github installation token response missing token")
	}
	r.tokenMu.Lock()
	r.tokens[installationID] = githubInstallationToken{Token: resp.Token, ExpiresAt: resp.ExpiresAt}
	r.tokenMu.Unlock()
	return resp.Token, nil
}

func (r *GitHubRunner) fetchInstallation(ctx context.Context, installationID int64) (githubInstallationResponse, error) {
	appJWT, err := r.createAppJWT()
	if err != nil {
		return githubInstallationResponse{}, err
	}
	var resp githubInstallationResponse
	if err := r.githubRequest(ctx, http.MethodGet, fmt.Sprintf("/app/installations/%d", installationID), appJWT, nil, &resp, http.StatusOK); err != nil {
		return githubInstallationResponse{}, err
	}
	return resp, nil
}

func (r *GitHubRunner) createJITConfig(ctx context.Context, installationID int64, org, runnerName, runnerClass string) (githubJITConfigResponse, error) {
	token, err := r.installationToken(ctx, installationID)
	if err != nil {
		return githubJITConfigResponse{}, err
	}
	body := map[string]any{
		"name":            runnerName,
		"runner_group_id": r.cfg.RunnerGroupID,
		"labels":          []string{"self-hosted", "linux", "x64", runnerClass},
		"work_folder":     githubRunnerWorkFolder,
	}
	var resp githubJITConfigResponse
	if err := r.githubRequest(ctx, http.MethodPost, "/orgs/"+url.PathEscape(org)+"/actions/runners/generate-jitconfig", token, body, &resp, http.StatusCreated); err != nil {
		return githubJITConfigResponse{}, err
	}
	if strings.TrimSpace(resp.EncodedJITConfig) == "" {
		return githubJITConfigResponse{}, fmt.Errorf("github jit config response missing encoded_jit_config")
	}
	return resp, nil
}

func (r *GitHubRunner) deleteRunner(ctx context.Context, installationID int64, org string, runnerID int64) error {
	token, err := r.installationToken(ctx, installationID)
	if err != nil {
		return err
	}
	err = r.githubRequest(ctx, http.MethodDelete, fmt.Sprintf("/orgs/%s/actions/runners/%d", url.PathEscape(org), runnerID), token, nil, nil, http.StatusNoContent, http.StatusNotFound)
	if err != nil {
		return err
	}
	return nil
}

func (r *GitHubRunner) deleteRunnerByName(ctx context.Context, installationID int64, org, runnerName string) error {
	token, err := r.installationToken(ctx, installationID)
	if err != nil {
		return err
	}
	var list githubRunnerListResponse
	if err := r.githubRequest(ctx, http.MethodGet, "/orgs/"+url.PathEscape(org)+"/actions/runners?per_page=100", token, nil, &list, http.StatusOK); err != nil {
		return err
	}
	for _, runner := range list.Runners {
		if runner.Name == runnerName {
			return r.deleteRunner(ctx, installationID, org, runner.ID)
		}
	}
	return nil
}

func (r *GitHubRunner) githubRequest(ctx context.Context, method, path, bearer string, body any, out any, expected ...int) error {
	apiBase := strings.TrimRight(firstNonEmpty(r.cfg.APIBaseURL, "https://api.github.com"), "/")
	var reader io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, apiBase+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", githubAPIVersion)
	req.Header.Set("User-Agent", "verself-sandbox-rental")
	req.Header.Set("Authorization", "Bearer "+bearer)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := r.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	for _, status := range expected {
		if resp.StatusCode == status {
			if out != nil && resp.Body != nil {
				return json.NewDecoder(resp.Body).Decode(out)
			}
			return nil
		}
	}
	detail, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	return fmt.Errorf("github api %s %s: status %d: %s", method, path, resp.StatusCode, strings.TrimSpace(string(detail)))
}

func (r *GitHubRunner) deriveJITFetchToken(allocationID, attemptID uuid.UUID) string {
	mac := hmac.New(sha256.New, []byte("verself-github-jit:"+r.cfg.WebhookSecret))
	mac.Write([]byte(allocationID.String()))
	mac.Write([]byte(":"))
	mac.Write([]byte(attemptID.String()))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func (r *GitHubRunner) deriveStickyDiskToken(executionID, attemptID uuid.UUID) string {
	mac := hmac.New(sha256.New, []byte("verself-sticky-disk:"+r.cfg.WebhookSecret))
	mac.Write([]byte(executionID.String()))
	mac.Write([]byte(":"))
	mac.Write([]byte(attemptID.String()))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func (r *GitHubRunner) deriveCheckoutToken(executionID, attemptID uuid.UUID) string {
	mac := hmac.New(sha256.New, []byte("verself-checkout:"+r.cfg.WebhookSecret))
	mac.Write([]byte(executionID.String()))
	mac.Write([]byte(":"))
	mac.Write([]byte(attemptID.String()))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(token)))
	return hex.EncodeToString(sum[:])
}

func verifyGitHubSignature(secret string, payload []byte, signature string) error {
	secret = strings.TrimSpace(secret)
	signature = strings.TrimSpace(signature)
	if secret == "" {
		return fmt.Errorf("%w: missing secret", ErrGitHubWebhookSignatureInvalid)
	}
	const prefix = "sha256="
	if !strings.HasPrefix(signature, prefix) {
		return fmt.Errorf("%w: missing signature", ErrGitHubWebhookSignatureInvalid)
	}
	got, err := hex.DecodeString(strings.TrimPrefix(signature, prefix))
	if err != nil {
		return fmt.Errorf("%w: decode signature: %v", ErrGitHubWebhookSignatureInvalid, err)
	}
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(payload)
	if !hmac.Equal(got, mac.Sum(nil)) {
		return ErrGitHubWebhookSignatureInvalid
	}
	return nil
}

func githubRunnerName(githubJobID int64, allocationID uuid.UUID) string {
	shortID := strings.ReplaceAll(allocationID.String(), "-", "")
	if len(shortID) > 10 {
		shortID = shortID[:10]
	}
	return fmt.Sprintf("verself-%d-%s", githubJobID, shortID)
}

func githubRunnerCommand() string {
	return `set -eu
jit_file="$(mktemp)"
header_file="$(mktemp)"
cleanup() { rm -f "$jit_file" "$header_file"; }
trap cleanup EXIT
printf 'header = "X-Verself-Runner-Bootstrap: %s"\n' "${VERSELF_GITHUB_JIT_TOKEN:?}" > "$header_file"
if [ -n "${VERSELF_TRACEPARENT:-}" ]; then
  printf 'header = "traceparent: %s"\n' "$VERSELF_TRACEPARENT" >> "$header_file"
fi
curl -fsS --retry 3 --retry-delay 1 --config "$header_file" "${VERSELF_HOST_SERVICE_HTTP_ORIGIN:?}${VERSELF_GITHUB_JIT_PATH:?}" -o "$jit_file"
unset VERSELF_TRACEPARENT
unset VERSELF_GITHUB_JIT_TOKEN
cd /opt/actions-runner
rm -rf _work
ln -s /workspace _work
exec ./run.sh --jitconfig "$(cat "$jit_file")"`
}

func nullableTime(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value.UTC()
}

func traceInt64(key string, value int64) attribute.KeyValue {
	return attribute.Int64(key, value)
}
