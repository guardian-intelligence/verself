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

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/verself/apiwire"
	"github.com/verself/sandbox-rental-service/internal/scheduler"
	"github.com/verself/sandbox-rental-service/internal/store"
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
	ProjectID            uuid.UUID
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
	if err := store.New(tx).UpsertForgejoRunnerRepository(ctx, store.UpsertForgejoRunnerRepositoryParams{
		ProviderRepositoryID: req.ProviderRepositoryID,
		OrgID:                dbOrgID(req.OrgID),
		ProjectID:            req.ProjectID,
		SourceRepositoryID:   uuidPtrFromZero(req.SourceRepositoryID),
		ProviderOwner:        req.ProviderOwner,
		ProviderRepo:         req.ProviderRepo,
		RepositoryFullName:   req.RepositoryFullName,
		UpdatedAt:            pgTime(now),
	}); err != nil {
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
	qtx := store.New(tx)
	if err := qtx.InsertForgejoRunnerAllocation(ctx, store.InsertForgejoRunnerAllocationParams{
		AllocationID:              allocationID,
		ProviderRepositoryID:      job.ProviderRepositoryID,
		RunnerClass:               runnerClass,
		RunnerName:                runnerName,
		RequestedForProviderJobID: providerJobID,
		AllocateBy:                pgTime(now.Add(30 * time.Second)),
		JitBy:                     pgTime(now.Add(time.Minute)),
		VmSubmittedBy:             pgTime(now.Add(2 * time.Minute)),
		RunnerListeningBy:         pgTime(now.Add(5 * time.Minute)),
		AssignmentBy:              pgTime(now.Add(10 * time.Minute)),
		VmExitBy:                  pgTime(now.Add(3 * time.Hour)),
		CleanupBy:                 pgTime(now.Add(3 * time.Hour)),
		CreatedAt:                 pgTime(now),
	}); err != nil {
		recordRunnerError(span, err)
		return err
	}
	if err := qtx.InsertRunnerJobBinding(ctx, store.InsertRunnerJobBindingParams{
		BindingID:        uuid.New(),
		AllocationID:     allocationID,
		Provider:         RunnerProviderForgejo,
		ProviderJobID:    providerJobID,
		ProviderRunnerID: 0,
		RunnerName:       runnerName,
		BoundAt:          pgTime(now),
	}); err != nil {
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
	if err := r.service.storeQueries().UpdateForgejoAllocationBootstrapCreated(ctx, store.UpdateForgejoAllocationBootstrapCreatedParams{
		ProviderRunnerID: registration.ID,
		UpdatedAt:        pgTime(time.Now().UTC()),
		AllocationID:     allocationID,
	}); err != nil {
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
	row, err := r.service.storeQueries().GetForgejoBindAllocation(ctx, store.GetForgejoBindAllocationParams{ProviderJobID: providerJobID})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	state := "assigned"
	if row.Status == "completed" || row.Status == "success" || row.Status == "failure" || row.Status == "cancelled" || row.Status == "skipped" {
		state = "job_completed"
	}
	now := time.Now().UTC()
	if err := r.service.storeQueries().UpdateRunnerAllocationAssignment(ctx, store.UpdateRunnerAllocationAssignmentParams{
		State:        state,
		UpdatedAt:    pgTime(now),
		CleanupBy:    pgTime(now.Add(30 * time.Minute)),
		Provider:     RunnerProviderForgejo,
		AllocationID: row.AllocationID,
	}); err != nil {
		return err
	}
	if state == "job_completed" && r.service.Scheduler != nil {
		_, _ = r.service.Scheduler.EnqueueRunnerCleanup(ctx, schedulerCleanupRequest(ctx, row.AllocationID))
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
	if err := r.service.storeQueries().MarkRunnerAllocationCleaned(ctx, store.MarkRunnerAllocationCleanedParams{
		CleanupBy:    pgTime(now),
		AllocationID: allocationID,
	}); err != nil {
		return err
	}
	_ = r.service.storeQueries().DeleteRunnerBootstrapConfig(ctx, store.DeleteRunnerBootstrapConfigParams{AllocationID: allocationID})
	return nil
}

func (r *ForgejoRunner) ConsumeBootstrapConfig(ctx context.Context, token string) (string, error) {
	return r.service.ConsumeRunnerBootstrapConfig(ctx, token, RunnerBootstrapForgejoOneJob)
}

func (r *ForgejoRunner) loadRepository(ctx context.Context, providerRepositoryID int64) (forgejoRepositoryRecord, error) {
	row, err := r.service.storeQueries().GetForgejoRepository(ctx, store.GetForgejoRepositoryParams{ProviderRepositoryID: providerRepositoryID})
	if err != nil {
		return forgejoRepositoryRecord{}, err
	}
	repo := forgejoRepositoryRecord{
		ProviderRepositoryID: row.ProviderRepositoryID,
		OrgID:                orgIDFromDB(row.OrgID),
		ProjectID:            row.ProjectID,
		ProviderOwner:        row.ProviderOwner,
		ProviderRepo:         row.ProviderRepo,
		RepositoryFullName:   row.RepositoryFullName,
	}
	if row.SourceRepositoryID != nil {
		repo.SourceRepositoryID = *row.SourceRepositoryID
	}
	return repo, nil
}

func (r *ForgejoRunner) activeRunnerClasses(ctx context.Context) ([]string, error) {
	return r.service.storeQueries().ListActiveRunnerClasses(ctx)
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
	if err := store.New(tx).UpsertForgejoRunnerJob(ctx, store.UpsertForgejoRunnerJobParams{
		ProviderJobID:        job.ID,
		ProviderRepositoryID: repo.ProviderRepositoryID,
		RepositoryFullName:   repo.RepositoryFullName,
		ProviderRunID:        job.TaskID,
		ProviderTaskID:       job.TaskID,
		ProviderJobHandle:    job.Handle,
		JobName:              job.Name,
		Status:               strings.TrimSpace(job.Status),
		LabelsJson:           labels,
		UpdatedAt:            pgTime(now),
	}); err != nil {
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
	row, err := r.service.storeQueries().GetForgejoQueuedJob(ctx, store.GetForgejoQueuedJobParams{ProviderJobID: providerJobID})
	if err != nil {
		return forgejoQueuedJob{}, err
	}
	job := forgejoQueuedJob{
		ProviderJobID:        row.ProviderJobID,
		ProviderRepositoryID: row.ProviderRepositoryID,
		ProviderTaskID:       row.ProviderTaskID,
		ProviderJobHandle:    row.ProviderJobHandle,
		RepositoryFullName:   row.RepositoryFullName,
		JobName:              row.JobName,
		HeadSHA:              row.HeadSha,
		HeadBranch:           row.HeadBranch,
		OrgID:                orgIDFromDB(row.OrgID),
	}
	_ = json.Unmarshal(row.LabelsJson, &job.Labels)
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
	allocationID, err := r.service.storeQueries().GetActiveAllocationForRunnerJob(ctx, store.GetActiveAllocationForRunnerJobParams{
		Provider:      RunnerProviderForgejo,
		ProviderJobID: providerJobID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, nil
	}
	return allocationID, err
}

func (r *ForgejoRunner) loadAllocation(ctx context.Context, allocationID uuid.UUID) (forgejoAllocation, error) {
	row, err := r.service.storeQueries().GetForgejoAllocation(ctx, store.GetForgejoAllocationParams{AllocationID: allocationID})
	if err != nil {
		return forgejoAllocation{}, err
	}
	out := forgejoAllocation{
		AllocationID:         row.AllocationID,
		ProviderRepositoryID: row.ProviderRepositoryID,
		RunnerClass:          row.RunnerClass,
		RunnerName:           row.RunnerName,
		ProviderRunnerID:     row.ProviderRunnerID,
		RequestedJobID:       row.RequestedForProviderJobID,
		ProviderTaskID:       row.ProviderTaskID,
		ProviderJobHandle:    row.ProviderJobHandle,
		JobName:              row.JobName,
		HeadSHA:              row.HeadSha,
		HeadBranch:           row.HeadBranch,
		State:                row.State,
		OrgID:                orgIDFromDB(row.OrgID),
		ProviderOwner:        row.ProviderOwner,
		ProviderRepo:         row.ProviderRepo,
		RepositoryFullName:   row.RepositoryFullName,
		ProductID:            row.ProductID,
		Resources:            apiwire.VMResources{VCPUs: uint32(row.Vcpus), MemoryMiB: uint32(row.MemoryMib), RootDiskGiB: uint32(row.RootfsGib), KernelImage: apiwire.KernelImageDefault},
	}
	if row.ExecutionID != nil {
		out.ExecutionID = *row.ExecutionID
	}
	if row.AttemptID != nil {
		out.AttemptID = *row.AttemptID
	}
	_ = json.Unmarshal(row.LabelsJson, &out.Labels)
	return out, nil
}

func (r *ForgejoRunner) setAllocationState(ctx context.Context, allocationID uuid.UUID, state, reason string) error {
	return r.service.storeQueries().SetRunnerAllocationState(ctx, store.SetRunnerAllocationStateParams{
		State:         state,
		FailureReason: strings.TrimSpace(reason),
		UpdatedAt:     pgTime(time.Now().UTC()),
		Provider:      RunnerProviderForgejo,
		AllocationID:  allocationID,
	})
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
		"description": "Verself one-job runner " + allocation.AllocationID.String(),
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
	req.Header.Set("User-Agent", "verself-sandbox-rental")
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
	mac := hmac.New(sha256.New, []byte("verself-forgejo-bootstrap:"+r.cfg.BootstrapSecret))
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
	if req.Provider != RunnerProviderForgejo || req.OrgID == 0 || req.ProjectID == uuid.Nil || req.ProviderRepositoryID <= 0 || req.ProviderOwner == "" || req.ProviderRepo == "" {
		return RunnerRepositoryRegistration{}, ErrRunnerUnavailable
	}
	if req.RepositoryFullName == "" {
		req.RepositoryFullName = req.ProviderOwner + "/" + req.ProviderRepo
	}
	return req, nil
}

func forgejoRunnerName(providerJobID int64, allocationID uuid.UUID) string {
	shortID := strings.ReplaceAll(allocationID.String(), "-", "")
	if len(shortID) > 10 {
		shortID = shortID[:10]
	}
	return fmt.Sprintf("verself-fj-%d-%s", providerJobID, shortID)
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
printf 'header = "X-Verself-Runner-Bootstrap: %s"\n' "${VERSELF_RUNNER_BOOTSTRAP_TOKEN:?}" > "$header_file"
if [ -n "${VERSELF_TRACEPARENT:-}" ]; then
  printf 'header = "traceparent: %s"\n' "$VERSELF_TRACEPARENT" >> "$header_file"
fi
curl -fsS --retry 3 --retry-delay 1 --config "$header_file" "${VERSELF_HOST_SERVICE_HTTP_ORIGIN:?}${VERSELF_RUNNER_BOOTSTRAP_PATH:?}" -o "$bootstrap_file"
unset VERSELF_TRACEPARENT
unset VERSELF_RUNNER_BOOTSTRAP_TOKEN
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
