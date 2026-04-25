package jobs

import (
	"bytes"
	"context"
	"crypto/hmac"
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
	"time"

	"github.com/forge-metal/apiwire"
	"github.com/forge-metal/sandbox-rental-service/internal/scheduler"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"go.opentelemetry.io/otel/attribute"
)

var (
	ErrForgejoRunnerNotConfigured     = errors.New("forgejo runner is not configured")
	ErrForgejoWebhookSignatureInvalid = errors.New("forgejo webhook signature invalid")
)

const forgejoWebhookPath = "/webhooks/forgejo/actions"

type ForgejoRunnerConfig struct {
	APIBaseURL      string
	RunnerBaseURL   string
	WebhookBaseURL  string
	Token           string
	WebhookSecret   string
	BootstrapSecret string
}

type ForgejoRunner struct {
	service *Service
	cfg     ForgejoRunnerConfig
	client  *http.Client
}

type forgejoRepositoryRecord struct {
	ProviderRepositoryID int64
	OrgID                uint64
	SourceRepositoryID   uuid.UUID
	ProviderOwner        string
	ProviderRepo         string
	RepositoryFullName   string
}

type forgejoQueuedJob struct {
	ProviderJobID        int64
	ProviderRepositoryID int64
	ProviderTaskID       int64
	ProviderJobHandle    string
	RepositoryFullName   string
	JobName              string
	HeadSHA              string
	HeadBranch           string
	Labels               []string
	OrgID                uint64
}

type forgejoAllocation struct {
	AllocationID         uuid.UUID
	ProviderRepositoryID int64
	RunnerClass          string
	Labels               []string
	RunnerName           string
	ProviderRunnerID     int64
	RequestedJobID       int64
	ProviderTaskID       int64
	ProviderJobHandle    string
	JobName              string
	HeadSHA              string
	HeadBranch           string
	ExecutionID          uuid.UUID
	AttemptID            uuid.UUID
	State                string
	OrgID                uint64
	ProviderOwner        string
	ProviderRepo         string
	RepositoryFullName   string
	Resources            apiwire.VMResources
	ProductID            string
}

type forgejoWebhookEvent struct {
	Ref        string `json:"ref"`
	After      string `json:"after"`
	Repository struct {
		ID       int64  `json:"id"`
		Name     string `json:"name"`
		FullName string `json:"full_name"`
		Owner    struct {
			Login    string `json:"login"`
			Username string `json:"username"`
		} `json:"owner"`
	} `json:"repository"`
	Action string `json:"action"`
}

type forgejoHook struct {
	ID     int64             `json:"id"`
	Type   string            `json:"type"`
	Config map[string]string `json:"config"`
	Active bool              `json:"active"`
}

type forgejoRunnerRegistrationResponse struct {
	ID    int64  `json:"id"`
	UUID  string `json:"uuid"`
	Token string `json:"token"`
}

type forgejoActionRunJob struct {
	ID      int64    `json:"id"`
	Handle  string   `json:"handle"`
	TaskID  int64    `json:"task_id"`
	Status  string   `json:"status"`
	RunsOn  []string `json:"runs_on"`
	Name    string   `json:"name"`
	Attempt int64    `json:"attempt"`
	RepoID  int64    `json:"repo_id"`
	OwnerID int64    `json:"owner_id"`
}

type forgejoBootstrapPayload struct {
	ServerURL    string   `json:"server_url"`
	RunnerUUID   string   `json:"runner_uuid"`
	RunnerToken  string   `json:"runner_token"`
	RunnerLabels []string `json:"runner_labels"`
	JobHandle    string   `json:"job_handle"`
}

func NewForgejoRunner(service *Service, cfg ForgejoRunnerConfig, client *http.Client) (*ForgejoRunner, error) {
	if client == nil {
		client = http.DefaultClient
	}
	return &ForgejoRunner{service: service, cfg: cfg, client: client}, nil
}

func (r *ForgejoRunner) Configured() bool {
	return r != nil &&
		strings.TrimSpace(r.cfg.APIBaseURL) != "" &&
		strings.TrimSpace(r.cfg.RunnerBaseURL) != "" &&
		strings.TrimSpace(r.cfg.WebhookBaseURL) != "" &&
		strings.TrimSpace(r.cfg.Token) != "" &&
		strings.TrimSpace(r.cfg.WebhookSecret) != "" &&
		strings.TrimSpace(r.cfg.BootstrapSecret) != ""
}

func (r *ForgejoRunner) RegisterRepository(ctx context.Context, req RunnerRepositoryRegistration) error {
	if !r.Configured() {
		return ErrForgejoRunnerNotConfigured
	}
	ctx, span := tracer.Start(ctx, "forgejo.runner.repository.register")
	defer span.End()
	req, err := normalizeRunnerRepositoryRegistration(req)
	if err != nil {
		recordRunnerError(span, err)
		return err
	}
	if err := r.ensureWebhook(ctx, req.ProviderOwner, req.ProviderRepo); err != nil {
		recordRunnerError(span, err)
		return err
	}
	now := time.Now().UTC()
	tx, err := r.service.PGX.Begin(ctx)
	if err != nil {
		recordRunnerError(span, err)
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	_, err = tx.Exec(ctx, `INSERT INTO runner_provider_repositories (
		provider, provider_repository_id, org_id, source_repository_id, provider_owner, provider_repo, repository_full_name, active, created_at, updated_at
	) VALUES ('forgejo',$1,$2,$3,$4,$5,$6,true,$7,$7)
	ON CONFLICT (provider, provider_repository_id) DO UPDATE SET
		org_id = EXCLUDED.org_id,
		source_repository_id = EXCLUDED.source_repository_id,
		provider_owner = EXCLUDED.provider_owner,
		provider_repo = EXCLUDED.provider_repo,
		repository_full_name = EXCLUDED.repository_full_name,
		active = true,
		updated_at = EXCLUDED.updated_at`,
		req.ProviderRepositoryID, req.OrgID, nullableUUID(req.SourceRepositoryID), req.ProviderOwner, req.ProviderRepo, req.RepositoryFullName, now)
	if err != nil {
		recordRunnerError(span, err)
		return err
	}
	if r.service.Scheduler != nil {
		if _, err := r.service.Scheduler.EnqueueRunnerRepositorySyncTx(ctx, tx, scheduler.RunnerRepositorySyncRequest{
			Provider:             RunnerProviderForgejo,
			ProviderRepositoryID: req.ProviderRepositoryID,
			CorrelationID:        CorrelationIDFromContext(ctx),
			TraceParent:          traceParent(ctx),
		}); err != nil {
			recordRunnerError(span, err)
			return err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		recordRunnerError(span, err)
		return err
	}
	span.SetAttributes(
		attribute.Int64("forgejo.repository_id", req.ProviderRepositoryID),
		attribute.String("forgejo.repository", req.RepositoryFullName),
	)
	return nil
}

func (r *ForgejoRunner) HandleWebhook(ctx context.Context, eventName, deliveryID string, payload []byte, signature string) error {
	if !r.Configured() {
		return ErrForgejoRunnerNotConfigured
	}
	if err := verifyForgejoSignature(r.cfg.WebhookSecret, payload, signature); err != nil {
		return err
	}
	eventName = strings.TrimSpace(eventName)
	if !forgejoEventTriggersSync(eventName) {
		return nil
	}
	ctx, span := tracer.Start(ctx, "forgejo.webhook.actions")
	defer span.End()
	var event forgejoWebhookEvent
	if err := json.Unmarshal(payload, &event); err != nil {
		recordRunnerError(span, err)
		return err
	}
	repoID := event.Repository.ID
	if repoID <= 0 {
		return fmt.Errorf("forgejo webhook missing repository id")
	}
	span.SetAttributes(
		attribute.String("forgejo.event", eventName),
		attribute.String("forgejo.delivery_id", deliveryID),
		attribute.Int64("forgejo.repository_id", repoID),
	)
	if r.service.Scheduler == nil {
		return ErrRunnerUnavailable
	}
	_, err := r.service.Scheduler.EnqueueRunnerRepositorySync(ctx, scheduler.RunnerRepositorySyncRequest{
		Provider:             RunnerProviderForgejo,
		ProviderRepositoryID: repoID,
		CorrelationID:        deliveryID,
		TraceParent:          traceParent(ctx),
	})
	if err != nil {
		recordRunnerError(span, err)
	}
	return err
}

func (r *ForgejoRunner) SyncRepositoryJobs(ctx context.Context, providerRepositoryID int64) error {
	if !r.Configured() {
		return ErrForgejoRunnerNotConfigured
	}
	ctx, span := tracer.Start(ctx, "forgejo.runner.repository.sync")
	defer span.End()
	repo, err := r.loadRepository(ctx, providerRepositoryID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			span.SetAttributes(attribute.Bool("forgejo.repository_sync.noop", true))
			return nil
		}
		recordRunnerError(span, err)
		return err
	}
	classes, err := r.activeRunnerClasses(ctx)
	if err != nil {
		recordRunnerError(span, err)
		return err
	}
	count, err := r.syncRepositoryJobsOnce(ctx, repo, classes)
	if err != nil {
		recordRunnerError(span, err)
		return err
	}
	if count == 0 {
		timer := time.NewTimer(time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
		count, err = r.syncRepositoryJobsOnce(ctx, repo, classes)
		if err != nil {
			recordRunnerError(span, err)
			return err
		}
	}
	span.SetAttributes(attribute.Int64("forgejo.repository_id", providerRepositoryID), attribute.Int("forgejo.runner_jobs_seen", count))
	return nil
}

func (r *ForgejoRunner) ReconcileCapacity(ctx context.Context, providerJobID int64) error {
	ctx, span := tracer.Start(ctx, "forgejo.capacity.reconcile")
	defer span.End()
	span.SetAttributes(attribute.Int64("forgejo.job_id", providerJobID))
	job, err := r.loadQueuedJob(ctx, providerJobID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			span.SetAttributes(attribute.Bool("forgejo.capacity.noop", true))
			return nil
		}
		recordRunnerError(span, err)
		return err
	}
	runnerClass, resources, productID, err := r.runnerClassForLabels(ctx, job.Labels)
	if err != nil {
		recordRunnerError(span, err)
		return err
	}
	if runnerClass == "" {
		span.SetAttributes(attribute.Bool("forgejo.capacity.no_matching_runner_class", true))
		return nil
	}
	existing, err := r.activeAllocationForJob(ctx, providerJobID)
	if err != nil {
		recordRunnerError(span, err)
		return err
	}
	if existing != uuid.Nil {
		span.SetAttributes(attribute.String("runner.allocation_id", existing.String()), attribute.Bool("forgejo.capacity.existing_allocation", true))
		return nil
	}
	allocationID := uuid.New()
	runnerName := forgejoRunnerName(providerJobID, allocationID)
	now := time.Now().UTC()
	tx, err := r.service.PGX.Begin(ctx)
	if err != nil {
		recordRunnerError(span, err)
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `INSERT INTO runner_allocations (
		allocation_id, provider, provider_installation_id, provider_repository_id, runner_class, runner_name, state,
		requested_for_provider_job_id, allocate_by, jit_by, vm_submitted_by, runner_listening_by,
		assignment_by, vm_exit_by, cleanup_by, created_at, updated_at
	) VALUES ($1,'forgejo',0,$2,$3,$4,'pending',$5,$6,$7,$8,$9,$10,$11,$12,$13,$13)`,
		allocationID, job.ProviderRepositoryID, runnerClass, runnerName, providerJobID,
		now.Add(30*time.Second), now.Add(time.Minute), now.Add(2*time.Minute), now.Add(5*time.Minute),
		now.Add(10*time.Minute), now.Add(3*time.Hour), now.Add(3*time.Hour), now); err != nil {
		recordRunnerError(span, err)
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO runner_job_bindings (
		binding_id, allocation_id, provider, provider_job_id, provider_runner_id, runner_name, bound_at, created_at
	) VALUES ($1,$2,'forgejo',$3,0,$4,$5,$5)
	ON CONFLICT (provider, provider_job_id) DO NOTHING`, uuid.New(), allocationID, providerJobID, runnerName, now); err != nil {
		recordRunnerError(span, err)
		return err
	}
	if r.service.Scheduler == nil {
		return ErrRunnerUnavailable
	}
	if _, err := r.service.Scheduler.EnqueueRunnerAllocateTx(ctx, tx, scheduler.RunnerAllocateRequest{
		AllocationID:  allocationID.String(),
		CorrelationID: CorrelationIDFromContext(ctx),
		TraceParent:   traceParent(ctx),
	}); err != nil {
		recordRunnerError(span, err)
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		recordRunnerError(span, err)
		return err
	}
	span.SetAttributes(
		attribute.String("runner.allocation_id", allocationID.String()),
		attribute.String("runner.class", runnerClass),
		attribute.Int("vmresources.vcpus", int(resources.VCPUs)),
		attribute.String("billing.product_id", productID),
	)
	return nil
}

func (r *ForgejoRunner) AllocateRunner(ctx context.Context, allocationID uuid.UUID) error {
	ctx, span := tracer.Start(ctx, "forgejo.runner.allocate")
	defer span.End()
	span.SetAttributes(attribute.String("runner.allocation_id", allocationID.String()))
	allocation, err := r.loadAllocation(ctx, allocationID)
	if err != nil {
		recordRunnerError(span, err)
		return err
	}
	if allocation.State == "vm_submitted" || allocation.State == "assigned" || allocation.State == "cleaned" {
		return nil
	}
	if err := r.setAllocationState(ctx, allocationID, "bootstrap_creating", ""); err != nil {
		recordRunnerError(span, err)
		return err
	}
	registration, err := r.registerEphemeralRunner(ctx, allocation)
	if err != nil {
		_ = r.setAllocationState(ctx, allocationID, "failed", "runner_registration_failed")
		recordRunnerError(span, err)
		return err
	}
	payload, err := json.Marshal(forgejoBootstrapPayload{
		ServerURL:    strings.TrimRight(r.cfg.RunnerBaseURL, "/"),
		RunnerUUID:   registration.UUID,
		RunnerToken:  registration.Token,
		RunnerLabels: forgejoRunnerHostLabels(allocation.Labels, allocation.RunnerClass),
		JobHandle:    allocation.ProviderJobHandle,
	})
	if err != nil {
		recordRunnerError(span, err)
		return err
	}
	if _, err := r.service.PGX.Exec(ctx, `UPDATE runner_allocations
		SET provider_runner_id = $1, state = 'bootstrap_created', updated_at = $2
		WHERE provider = 'forgejo' AND allocation_id = $3`, registration.ID, time.Now().UTC(), allocationID); err != nil {
		recordRunnerError(span, err)
		return err
	}
	attemptID := uuid.New()
	executionID, attemptID, err := r.service.Submit(WithCorrelationID(ctx, CorrelationIDFromContext(ctx)), allocation.OrgID, fmt.Sprintf("forgejo-actions:%d", allocation.ProviderRepositoryID), SubmitRequest{
		Kind:                   KindDirect,
		SourceKind:             SourceKindForgejoAction,
		WorkloadKind:           WorkloadKindRunner,
		SourceRef:              allocation.RepositoryFullName,
		RunnerClass:            allocation.RunnerClass,
		ExternalProvider:       RunnerProviderForgejo,
		ExternalTaskID:         strconv.FormatInt(allocation.RequestedJobID, 10),
		Provider:               RunnerProviderForgejo,
		ProductID:              allocation.ProductID,
		IdempotencyKey:         "forgejo-runner:" + allocationID.String(),
		RunCommand:             forgejoRunnerCommand(),
		MaxWallSeconds:         uint64((3 * time.Hour).Seconds()),
		Resources:              allocation.Resources,
		AttemptID:              attemptID,
		RunnerAllocationID:     allocationID,
		RunnerBootstrapKind:    RunnerBootstrapForgejoOneJob,
		RunnerBootstrapPayload: string(payload),
	})
	if err != nil {
		_ = r.deleteRunner(ctx, allocation, registration.ID)
		_ = r.setAllocationState(ctx, allocationID, "failed", "execution_submit_failed")
		recordRunnerError(span, err)
		return err
	}
	span.SetAttributes(
		attribute.Int64("forgejo.runner_id", registration.ID),
		attribute.String("execution.id", executionID.String()),
		attribute.String("attempt.id", attemptID.String()),
	)
	return nil
}

func (r *ForgejoRunner) BindJob(ctx context.Context, providerJobID int64) error {
	var allocationID uuid.UUID
	var status string
	err := r.service.PGX.QueryRow(ctx, `SELECT a.allocation_id, j.status
		FROM runner_allocations a
		JOIN runner_jobs j ON j.provider = a.provider AND j.provider_job_id = a.requested_for_provider_job_id
		WHERE a.provider = 'forgejo' AND j.provider_job_id = $1
		ORDER BY a.created_at DESC
		LIMIT 1`, providerJobID).Scan(&allocationID, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	state := "assigned"
	if status == "completed" || status == "success" || status == "failure" || status == "cancelled" || status == "skipped" {
		state = "job_completed"
	}
	now := time.Now().UTC()
	if _, err := r.service.PGX.Exec(ctx, `UPDATE runner_allocations SET state = $1, assignment_by = COALESCE(assignment_by, $2), cleanup_by = $3, updated_at = $2 WHERE provider = 'forgejo' AND allocation_id = $4 AND state <> 'cleaned'`, state, now, now.Add(30*time.Minute), allocationID); err != nil {
		return err
	}
	if state == "job_completed" && r.service.Scheduler != nil {
		_, _ = r.service.Scheduler.EnqueueRunnerCleanup(ctx, schedulerCleanupRequest(ctx, allocationID))
	}
	return nil
}

func (r *ForgejoRunner) CleanupRunner(ctx context.Context, allocationID uuid.UUID) error {
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
	if allocation.ProviderRunnerID != 0 {
		if err := r.deleteRunner(ctx, allocation, allocation.ProviderRunnerID); err != nil {
			return err
		}
	}
	now := time.Now().UTC()
	if _, err := r.service.PGX.Exec(ctx, `UPDATE runner_allocations SET state = 'cleaned', cleanup_by = $1, updated_at = $1 WHERE provider = 'forgejo' AND allocation_id = $2`, now, allocationID); err != nil {
		return err
	}
	_, _ = r.service.PGX.Exec(ctx, `DELETE FROM runner_bootstrap_configs WHERE allocation_id = $1`, allocationID)
	return nil
}

func (r *ForgejoRunner) ConsumeBootstrapConfig(ctx context.Context, token string) (string, error) {
	return r.service.ConsumeRunnerBootstrapConfig(ctx, token, RunnerBootstrapForgejoOneJob)
}

func (r *ForgejoRunner) loadRepository(ctx context.Context, providerRepositoryID int64) (forgejoRepositoryRecord, error) {
	var repo forgejoRepositoryRecord
	var sourceRepo *uuid.UUID
	err := r.service.PGX.QueryRow(ctx, `SELECT provider_repository_id, org_id, source_repository_id, provider_owner, provider_repo, repository_full_name
		FROM runner_provider_repositories
		WHERE provider = 'forgejo' AND provider_repository_id = $1 AND active`, providerRepositoryID).
		Scan(&repo.ProviderRepositoryID, &repo.OrgID, &sourceRepo, &repo.ProviderOwner, &repo.ProviderRepo, &repo.RepositoryFullName)
	if sourceRepo != nil {
		repo.SourceRepositoryID = *sourceRepo
	}
	return repo, err
}

func (r *ForgejoRunner) activeRunnerClasses(ctx context.Context) ([]string, error) {
	rows, err := r.service.PGX.Query(ctx, `SELECT runner_class FROM runner_classes WHERE active ORDER BY runner_class`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var runnerClass string
		if err := rows.Scan(&runnerClass); err != nil {
			return nil, err
		}
		out = append(out, runnerClass)
	}
	return out, rows.Err()
}

func (r *ForgejoRunner) syncRepositoryJobsOnce(ctx context.Context, repo forgejoRepositoryRecord, runnerClasses []string) (int, error) {
	seen := map[int64]struct{}{}
	for _, runnerClass := range runnerClasses {
		for _, labels := range forgejoRunnerSearchLabelSets(runnerClass) {
			jobs, err := r.listRunnerJobs(ctx, repo, labels)
			if err != nil {
				return len(seen), err
			}
			for _, job := range jobs {
				if _, ok := seen[job.ID]; ok {
					continue
				}
				seen[job.ID] = struct{}{}
				if err := r.upsertJobAndMaybeEnqueue(ctx, repo, job); err != nil {
					return len(seen), err
				}
			}
		}
	}
	return len(seen), nil
}

func (r *ForgejoRunner) upsertJobAndMaybeEnqueue(ctx context.Context, repo forgejoRepositoryRecord, job forgejoActionRunJob) error {
	labels, err := json.Marshal(job.RunsOn)
	if err != nil {
		return err
	}
	tx, err := r.service.PGX.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	now := time.Now().UTC()
	_, err = tx.Exec(ctx, `INSERT INTO runner_jobs (
		provider, provider_job_id, provider_installation_id, provider_repository_id, repository_full_name,
		provider_run_id, provider_task_id, provider_job_handle, job_name, status, labels_json, updated_at, created_at
	) VALUES ('forgejo',$1,0,$2,$3,$4,$5,$6,$7,$8,$9,$10,$10)
	ON CONFLICT (provider, provider_job_id) DO UPDATE SET
		provider_repository_id = EXCLUDED.provider_repository_id,
		repository_full_name = EXCLUDED.repository_full_name,
		provider_task_id = EXCLUDED.provider_task_id,
		provider_job_handle = EXCLUDED.provider_job_handle,
		job_name = EXCLUDED.job_name,
		status = EXCLUDED.status,
		labels_json = EXCLUDED.labels_json,
		updated_at = EXCLUDED.updated_at`,
		job.ID, repo.ProviderRepositoryID, repo.RepositoryFullName, job.TaskID, job.TaskID, job.Handle, job.Name, strings.TrimSpace(job.Status), string(labels), now)
	if err != nil {
		return err
	}
	if forgejoJobWantsCapacity(job.Status) && r.service.Scheduler != nil {
		if _, err := r.service.Scheduler.EnqueueRunnerCapacityReconcileTx(ctx, tx, scheduler.RunnerCapacityReconcileRequest{
			Provider:             RunnerProviderForgejo,
			ProviderRepositoryID: repo.ProviderRepositoryID,
			ProviderJobID:        job.ID,
			CorrelationID:        CorrelationIDFromContext(ctx),
			TraceParent:          traceParent(ctx),
		}); err != nil {
			return err
		}
	}
	if forgejoJobIsTerminal(job.Status) && r.service.Scheduler != nil {
		if _, err := r.service.Scheduler.EnqueueRunnerJobBindTx(ctx, tx, scheduler.RunnerJobBindRequest{
			Provider:      RunnerProviderForgejo,
			ProviderJobID: job.ID,
			CorrelationID: CorrelationIDFromContext(ctx),
			TraceParent:   traceParent(ctx),
		}); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (r *ForgejoRunner) listRunnerJobs(ctx context.Context, repo forgejoRepositoryRecord, runnerClass string) ([]forgejoActionRunJob, error) {
	values := url.Values{}
	values.Set("labels", runnerClass)
	path := fmt.Sprintf("/api/v1/repos/%s/%s/actions/runners/jobs?%s", url.PathEscape(repo.ProviderOwner), url.PathEscape(repo.ProviderRepo), values.Encode())
	var jobs []forgejoActionRunJob
	if err := r.forgejoJSON(ctx, http.MethodGet, path, nil, &jobs, http.StatusOK); err != nil {
		return nil, err
	}
	return jobs, nil
}

func (r *ForgejoRunner) loadQueuedJob(ctx context.Context, providerJobID int64) (forgejoQueuedJob, error) {
	row := r.service.PGX.QueryRow(ctx, `SELECT
		j.provider_job_id, j.provider_repository_id, j.provider_task_id, j.provider_job_handle,
		j.repository_full_name, j.job_name, j.head_sha, j.head_branch, j.labels_json, p.org_id
		FROM runner_jobs j
		JOIN runner_provider_repositories p ON p.provider = j.provider AND p.provider_repository_id = j.provider_repository_id AND p.active
		WHERE j.provider = 'forgejo' AND j.provider_job_id = $1 AND j.status IN ('waiting', 'queued')`, providerJobID)
	var job forgejoQueuedJob
	var labelsRaw []byte
	if err := row.Scan(&job.ProviderJobID, &job.ProviderRepositoryID, &job.ProviderTaskID, &job.ProviderJobHandle, &job.RepositoryFullName, &job.JobName, &job.HeadSHA, &job.HeadBranch, &labelsRaw, &job.OrgID); err != nil {
		return forgejoQueuedJob{}, err
	}
	_ = json.Unmarshal(labelsRaw, &job.Labels)
	return job, nil
}

func (r *ForgejoRunner) runnerClassForLabels(ctx context.Context, labels []string) (string, apiwire.VMResources, string, error) {
	for _, label := range labels {
		label = strings.TrimSpace(label)
		if label == "" {
			continue
		}
		resources, productID, ok, err := r.service.runnerClassResources(ctx, label)
		if err != nil {
			return "", apiwire.VMResources{}, "", err
		}
		if ok {
			return label, resources, productID, nil
		}
	}
	return "", apiwire.VMResources{}, "", nil
}

func (r *ForgejoRunner) activeAllocationForJob(ctx context.Context, providerJobID int64) (uuid.UUID, error) {
	var allocationID uuid.UUID
	err := r.service.PGX.QueryRow(ctx, `SELECT allocation_id FROM runner_allocations
		WHERE provider = 'forgejo'
		  AND requested_for_provider_job_id = $1
		  AND state NOT IN ('failed', 'cleaned')
		ORDER BY created_at DESC
		LIMIT 1`, providerJobID).Scan(&allocationID)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, nil
	}
	return allocationID, err
}

func (r *ForgejoRunner) loadAllocation(ctx context.Context, allocationID uuid.UUID) (forgejoAllocation, error) {
	row := r.service.PGX.QueryRow(ctx, `SELECT
		a.allocation_id, a.provider_repository_id, a.runner_class, a.runner_name,
		a.provider_runner_id, a.requested_for_provider_job_id, COALESCE(j.provider_task_id, 0),
		COALESCE(j.provider_job_handle, ''), COALESCE(j.job_name, ''), COALESCE(j.head_sha, ''),
		COALESCE(j.head_branch, ''), COALESCE(a.execution_id, '00000000-0000-0000-0000-000000000000'::uuid),
		COALESCE(a.attempt_id, '00000000-0000-0000-0000-000000000000'::uuid), a.state,
		p.org_id, p.provider_owner, p.provider_repo, p.repository_full_name, COALESCE(j.labels_json, '[]'::jsonb),
		c.product_id, c.vcpus, c.memory_mib, c.rootfs_gib
		FROM runner_allocations a
		JOIN runner_provider_repositories p ON p.provider = a.provider AND p.provider_repository_id = a.provider_repository_id
		JOIN runner_classes c ON c.runner_class = a.runner_class
		LEFT JOIN runner_jobs j ON j.provider = a.provider AND j.provider_job_id = a.requested_for_provider_job_id
		WHERE a.provider = 'forgejo' AND a.allocation_id = $1`, allocationID)
	var out forgejoAllocation
	var vcpus, memoryMiB, rootfsGiB int
	var labelsRaw []byte
	if err := row.Scan(&out.AllocationID, &out.ProviderRepositoryID, &out.RunnerClass, &out.RunnerName, &out.ProviderRunnerID, &out.RequestedJobID, &out.ProviderTaskID, &out.ProviderJobHandle, &out.JobName, &out.HeadSHA, &out.HeadBranch, &out.ExecutionID, &out.AttemptID, &out.State, &out.OrgID, &out.ProviderOwner, &out.ProviderRepo, &out.RepositoryFullName, &labelsRaw, &out.ProductID, &vcpus, &memoryMiB, &rootfsGiB); err != nil {
		return forgejoAllocation{}, err
	}
	_ = json.Unmarshal(labelsRaw, &out.Labels)
	out.Resources = apiwire.VMResources{VCPUs: uint32(vcpus), MemoryMiB: uint32(memoryMiB), RootDiskGiB: uint32(rootfsGiB), KernelImage: apiwire.KernelImageDefault}
	return out, nil
}

func (r *ForgejoRunner) setAllocationState(ctx context.Context, allocationID uuid.UUID, state, reason string) error {
	_, err := r.service.PGX.Exec(ctx, `UPDATE runner_allocations SET state = $1, failure_reason = $2, updated_at = $3 WHERE provider = 'forgejo' AND allocation_id = $4`, state, strings.TrimSpace(reason), time.Now().UTC(), allocationID)
	return err
}

func (r *ForgejoRunner) ensureWebhook(ctx context.Context, owner, repo string) error {
	targetURL := strings.TrimRight(r.cfg.WebhookBaseURL, "/") + forgejoWebhookPath
	var hooks []forgejoHook
	path := fmt.Sprintf("/api/v1/repos/%s/%s/hooks", url.PathEscape(owner), url.PathEscape(repo))
	if err := r.forgejoJSON(ctx, http.MethodGet, path, nil, &hooks, http.StatusOK); err != nil {
		return err
	}
	for _, hook := range hooks {
		if hook.Config != nil && strings.TrimSpace(hook.Config["url"]) == targetURL {
			body := forgejoHookEditRequestBody(targetURL, r.cfg.WebhookSecret)
			path := fmt.Sprintf("/api/v1/repos/%s/%s/hooks/%d", url.PathEscape(owner), url.PathEscape(repo), hook.ID)
			return r.forgejoJSON(ctx, http.MethodPatch, path, body, nil, http.StatusOK)
		}
	}
	body := forgejoHookCreateRequestBody(targetURL, r.cfg.WebhookSecret)
	return r.forgejoJSON(ctx, http.MethodPost, path, body, nil, http.StatusCreated)
}

func (r *ForgejoRunner) registerEphemeralRunner(ctx context.Context, allocation forgejoAllocation) (forgejoRunnerRegistrationResponse, error) {
	body := map[string]any{
		"name":        allocation.RunnerName,
		"description": "Forge Metal one-job runner " + allocation.AllocationID.String(),
		"ephemeral":   true,
	}
	path := fmt.Sprintf("/api/v1/repos/%s/%s/actions/runners", url.PathEscape(allocation.ProviderOwner), url.PathEscape(allocation.ProviderRepo))
	var resp forgejoRunnerRegistrationResponse
	if err := r.forgejoJSON(ctx, http.MethodPost, path, body, &resp, http.StatusCreated, http.StatusOK); err != nil {
		return forgejoRunnerRegistrationResponse{}, err
	}
	if resp.ID == 0 || strings.TrimSpace(resp.UUID) == "" || strings.TrimSpace(resp.Token) == "" {
		return forgejoRunnerRegistrationResponse{}, fmt.Errorf("forgejo runner registration response missing id, uuid, or token")
	}
	return resp, nil
}

func (r *ForgejoRunner) deleteRunner(ctx context.Context, allocation forgejoAllocation, runnerID int64) error {
	path := fmt.Sprintf("/api/v1/repos/%s/%s/actions/runners/%d", url.PathEscape(allocation.ProviderOwner), url.PathEscape(allocation.ProviderRepo), runnerID)
	return r.forgejoJSON(ctx, http.MethodDelete, path, nil, nil, http.StatusNoContent, http.StatusNotFound)
}

func (r *ForgejoRunner) forgejoJSON(ctx context.Context, method, path string, body any, out any, expected ...int) error {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(r.cfg.APIBaseURL, "/")+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "token "+r.cfg.Token)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "forge-metal-sandbox-rental")
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
			if out != nil && resp.Body != nil && resp.StatusCode != http.StatusNoContent {
				return json.NewDecoder(resp.Body).Decode(out)
			}
			return nil
		}
	}
	detail, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	return fmt.Errorf("forgejo api %s %s: status %d: %s", method, path, resp.StatusCode, strings.TrimSpace(string(detail)))
}

func (r *ForgejoRunner) deriveBootstrapFetchToken(allocationID, attemptID uuid.UUID) string {
	mac := hmac.New(sha256.New, []byte("forge-metal-forgejo-bootstrap:"+r.cfg.BootstrapSecret))
	mac.Write([]byte(allocationID.String()))
	mac.Write([]byte(":"))
	mac.Write([]byte(attemptID.String()))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func verifyForgejoSignature(secret string, payload []byte, signature string) error {
	secret = strings.TrimSpace(secret)
	signature = strings.TrimSpace(signature)
	signature = strings.TrimPrefix(signature, "sha256=")
	if secret == "" {
		return fmt.Errorf("%w: missing secret", ErrForgejoWebhookSignatureInvalid)
	}
	got, err := hex.DecodeString(signature)
	if err != nil {
		return fmt.Errorf("%w: decode signature: %v", ErrForgejoWebhookSignatureInvalid, err)
	}
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(payload)
	if !hmac.Equal(got, mac.Sum(nil)) {
		return ErrForgejoWebhookSignatureInvalid
	}
	return nil
}

func forgejoHookCreateRequestBody(targetURL, secret string) map[string]any {
	body := forgejoHookEditRequestBody(targetURL, secret)
	body["type"] = "forgejo"
	return body
}

func forgejoHookEditRequestBody(targetURL, secret string) map[string]any {
	return map[string]any{
		"config": map[string]string{
			"url":          targetURL,
			"content_type": "json",
			"http_method":  "post",
			"secret":       secret,
		},
		"events": []string{"push", "pull_request", "schedule", "workflow_dispatch", "action_run_failure", "action_run_recover", "action_run_success"},
		"active": true,
	}
}

func forgejoEventTriggersSync(eventName string) bool {
	switch strings.TrimSpace(eventName) {
	case "push", "pull_request", "schedule", "workflow_dispatch", "action_run_failure", "action_run_recover", "action_run_success":
		return true
	default:
		return false
	}
}

func forgejoJobWantsCapacity(status string) bool {
	switch strings.TrimSpace(status) {
	case "waiting", "queued":
		return true
	default:
		return false
	}
}

func forgejoJobIsTerminal(status string) bool {
	switch strings.TrimSpace(status) {
	case "completed", "success", "failure", "cancelled", "skipped":
		return true
	default:
		return false
	}
}

func normalizeRunnerRepositoryRegistration(req RunnerRepositoryRegistration) (RunnerRepositoryRegistration, error) {
	req.Provider = strings.TrimSpace(req.Provider)
	req.ProviderOwner = strings.TrimSpace(req.ProviderOwner)
	req.ProviderRepo = strings.TrimSpace(req.ProviderRepo)
	req.RepositoryFullName = strings.TrimSpace(req.RepositoryFullName)
	if req.Provider != RunnerProviderForgejo || req.OrgID == 0 || req.ProviderRepositoryID <= 0 || req.ProviderOwner == "" || req.ProviderRepo == "" {
		return RunnerRepositoryRegistration{}, ErrRunnerUnavailable
	}
	if req.RepositoryFullName == "" {
		req.RepositoryFullName = req.ProviderOwner + "/" + req.ProviderRepo
	}
	return req, nil
}

func nullableUUID(value uuid.UUID) any {
	if value == uuid.Nil {
		return nil
	}
	return value
}

func forgejoRunnerName(providerJobID int64, allocationID uuid.UUID) string {
	shortID := strings.ReplaceAll(allocationID.String(), "-", "")
	if len(shortID) > 10 {
		shortID = shortID[:10]
	}
	return fmt.Sprintf("forge-metal-fj-%d-%s", providerJobID, shortID)
}

func forgejoRunnerHostLabels(labels []string, runnerClass string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(labels)+1)
	for _, label := range labels {
		label = strings.TrimSpace(label)
		if label == "" || strings.Contains(label, ":") {
			continue
		}
		if _, ok := seen[label]; ok {
			continue
		}
		seen[label] = struct{}{}
		out = append(out, label)
	}
	runnerClass = strings.TrimSpace(runnerClass)
	if runnerClass != "" {
		if _, ok := seen[runnerClass]; !ok {
			out = append(out, runnerClass)
		}
	}
	return out
}

func forgejoRunnerSearchLabelSets(runnerClass string) []string {
	runnerClass = strings.TrimSpace(runnerClass)
	if runnerClass == "" {
		return nil
	}
	// Forgejo matches queued jobs by the full comma-separated runner label set, not by partial overlap.
	return []string{
		runnerClass,
		"self-hosted,linux,x64," + runnerClass,
	}
}

func forgejoRunnerCommand() string {
	return `set -eu
bootstrap_file="$(mktemp)"
token_file="$(mktemp)"
header_file="$(mktemp)"
cleanup() { rm -f "$bootstrap_file" "$token_file" "$header_file"; }
trap cleanup EXIT
printf 'header = "X-Forge-Metal-Runner-Bootstrap: %s"\n' "${FORGE_METAL_RUNNER_BOOTSTRAP_TOKEN:?}" > "$header_file"
if [ -n "${FORGE_METAL_TRACEPARENT:-}" ]; then
  printf 'header = "traceparent: %s"\n' "$FORGE_METAL_TRACEPARENT" >> "$header_file"
fi
curl -fsS --retry 3 --retry-delay 1 --config "$header_file" "${FORGE_METAL_HOST_SERVICE_HTTP_ORIGIN:?}${FORGE_METAL_RUNNER_BOOTSTRAP_PATH:?}" -o "$bootstrap_file"
unset FORGE_METAL_TRACEPARENT
unset FORGE_METAL_RUNNER_BOOTSTRAP_TOKEN
runner_uuid="$(jq -er '.runner_uuid' "$bootstrap_file")"
runner_token="$(jq -er '.runner_token' "$bootstrap_file")"
server_url="$(jq -er '.server_url' "$bootstrap_file")"
job_handle="$(jq -er '.job_handle' "$bootstrap_file")"
printf '%s' "$runner_token" > "$token_file"
chmod 0600 "$token_file"
cd /workspace
set -- forgejo-runner one-job --url "$server_url" --uuid "$runner_uuid" --token-url "file://$token_file"
while IFS= read -r runner_label; do
  [ -n "$runner_label" ] || continue
  set -- "$@" --label "${runner_label}:host"
done <<EOF
$(jq -er '.runner_labels[]' "$bootstrap_file")
EOF
exec "$@" --handle "$job_handle" --wait`
}
