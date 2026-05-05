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

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/verself/domain-transfer-objects"
	"github.com/verself/sandbox-rental-service/internal/scheduler"
	"github.com/verself/sandbox-rental-service/internal/store"
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
		ID    int64  `json:"id"`
		Login string `json:"login"`
		Type  string `json:"type"`
	} `json:"account"`
	RepositorySelection string            `json:"repository_selection"`
	Permissions         map[string]string `json:"permissions"`
}

type githubOAuthTokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	Error       string `json:"error"`
	Description string `json:"error_description"`
}

type githubUserInstallationsResponse struct {
	TotalCount    int `json:"total_count"`
	Installations []struct {
		ID int64 `json:"id"`
	} `json:"installations"`
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
	Resources          dto.VMResources
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
		strings.TrimSpace(r.cfg.ClientSecret) != "" &&
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
		if err := r.service.storeQueries().InsertGitHubInstallationState(ctx, store.InsertGitHubInstallationStateParams{
			State:     state,
			OrgID:     dbOrgID(orgID),
			ActorID:   actorID,
			ExpiresAt: pgTime(expiresAt),
			CreatedAt: pgTime(time.Now().UTC()),
		}); err != nil {
			return GitHubInstallationConnect{}, err
		}
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
	rows, err := s.storeQueries().ListGitHubInstallations(ctx, store.ListGitHubInstallationsParams{OrgID: dbOrgID(orgID)})
	if err != nil {
		return nil, err
	}
	out := make([]GitHubInstallationRecord, 0, len(rows))
	for _, row := range rows {
		out = append(out, githubInstallationRecordFromListRow(row))
	}
	return out, nil
}

func (r *GitHubRunner) CompleteInstallation(ctx context.Context, state, code string, installationID int64) (GitHubInstallationRecord, error) {
	if !r.Configured() {
		return GitHubInstallationRecord{}, ErrGitHubRunnerNotConfigured
	}
	state = strings.TrimSpace(state)
	code = strings.TrimSpace(code)
	if state == "" || code == "" || installationID <= 0 {
		return GitHubInstallationRecord{}, ErrGitHubInstallationInvalid
	}
	if r.service == nil || r.service.PGX == nil {
		return GitHubInstallationRecord{}, ErrGitHubInstallationInvalid
	}

	userToken, err := r.exchangeUserAccessToken(ctx, code)
	if err != nil {
		return GitHubInstallationRecord{}, err
	}
	if err := r.verifyUserInstallationAccess(ctx, userToken, installationID); err != nil {
		return GitHubInstallationRecord{}, err
	}
	installation, err := r.fetchInstallation(ctx, installationID)
	if err != nil {
		return GitHubInstallationRecord{}, err
	}
	if !strings.EqualFold(installation.Account.Type, "Organization") {
		return GitHubInstallationRecord{}, ErrGitHubInstallationInvalid
	}
	if installation.Account.ID <= 0 || strings.TrimSpace(installation.Account.Login) == "" {
		return GitHubInstallationRecord{}, ErrGitHubInstallationInvalid
	}
	tx, err := r.service.PGX.Begin(ctx)
	if err != nil {
		return GitHubInstallationRecord{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	qtx := store.New(tx)
	stateRow, err := qtx.LockGitHubInstallationState(ctx, store.LockGitHubInstallationStateParams{State: state})
	if err != nil {
		return GitHubInstallationRecord{}, ErrGitHubInstallationStateInvalid
	}
	orgID := orgIDFromDB(stateRow.OrgID)
	expires := timeFromPG(stateRow.ExpiresAt)
	if time.Now().UTC().After(expires) {
		return GitHubInstallationRecord{}, ErrGitHubInstallationStateInvalid
	}
	now := time.Now().UTC()
	if err := qtx.UpsertGitHubAccount(ctx, store.UpsertGitHubAccountParams{
		AccountID:    installation.Account.ID,
		AccountLogin: installation.Account.Login,
		AccountType:  installation.Account.Type,
		UpdatedAt:    pgTime(now),
	}); err != nil {
		return GitHubInstallationRecord{}, err
	}
	permissionsJSON, err := json.Marshal(installation.Permissions)
	if err != nil {
		return GitHubInstallationRecord{}, err
	}
	if len(permissionsJSON) == 0 || string(permissionsJSON) == "null" {
		permissionsJSON = []byte("{}")
	}
	if err := qtx.UpsertGitHubInstallation(ctx, store.UpsertGitHubInstallationParams{
		InstallationID:      installationID,
		AccountID:           installation.Account.ID,
		RepositorySelection: strings.TrimSpace(installation.RepositorySelection),
		PermissionsJson:     permissionsJSON,
		UpdatedAt:           pgTime(now),
	}); err != nil {
		return GitHubInstallationRecord{}, err
	}
	if err := qtx.UpsertGitHubInstallationConnection(ctx, store.UpsertGitHubInstallationConnectionParams{
		ConnectionID:       uuid.New(),
		InstallationID:     installationID,
		OrgID:              dbOrgID(orgID),
		ConnectedByActorID: stateRow.ActorID,
		UpdatedAt:          pgTime(now),
	}); err != nil {
		return GitHubInstallationRecord{}, err
	}
	if err := qtx.DeleteGitHubInstallationState(ctx, store.DeleteGitHubInstallationStateParams{State: state}); err != nil {
		return GitHubInstallationRecord{}, err
	}
	recordRow, err := qtx.GetGitHubInstallationForOrg(ctx, store.GetGitHubInstallationForOrgParams{
		OrgID:          dbOrgID(orgID),
		InstallationID: installationID,
	})
	if err != nil {
		return GitHubInstallationRecord{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return GitHubInstallationRecord{}, err
	}
	return githubInstallationRecordFromGetRow(recordRow), nil
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
	err = store.New(tx).UpsertGitHubRunnerJob(ctx, store.UpsertGitHubRunnerJobParams{
		ProviderJobID:          event.WorkflowJob.ID,
		ProviderInstallationID: event.Installation.ID,
		ProviderRepositoryID:   event.Repository.ID,
		RepositoryFullName:     event.Repository.FullName,
		ProviderRunID:          event.WorkflowJob.RunID,
		JobName:                event.WorkflowJob.Name,
		HeadSha:                event.WorkflowJob.HeadSHA,
		HeadBranch:             event.WorkflowJob.HeadBranch,
		WorkflowName:           event.WorkflowJob.WorkflowName,
		Status:                 status,
		Conclusion:             event.WorkflowJob.Conclusion,
		LabelsJson:             labels,
		RunnerID:               event.WorkflowJob.RunnerID,
		RunnerName:             event.WorkflowJob.RunnerName,
		StartedAt:              pgOptionalTime(event.WorkflowJob.StartedAt),
		CompletedAt:            pgOptionalTime(event.WorkflowJob.CompletedAt),
		LastWebhookDelivery:    deliveryID,
		UpdatedAt:              pgTime(now),
	})
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
	rows, err := store.New(tx).InsertGitHubRunnerAllocation(ctx, store.InsertGitHubRunnerAllocationParams{
		AllocationID:              allocationID,
		ProviderInstallationID:    job.InstallationID,
		ProviderRepositoryID:      job.RepositoryID,
		RunnerClass:               runnerClass,
		RunnerName:                runnerName,
		RequestedForProviderJobID: job.GitHubJobID,
		AllocateBy:                pgTime(now.Add(30 * time.Second)),
		JitBy:                     pgTime(now.Add(time.Minute)),
		VmSubmittedBy:             pgTime(now.Add(2 * time.Minute)),
		RunnerListeningBy:         pgTime(now.Add(5 * time.Minute)),
		AssignmentBy:              pgTime(now.Add(10 * time.Minute)),
		VmExitBy:                  pgTime(now.Add(3 * time.Hour)),
		CleanupBy:                 pgTime(now.Add(3 * time.Hour)),
		CreatedAt:                 pgTime(now),
	})
	if err != nil {
		return err
	}
	if rows == 0 {
		existing, err := r.activeAllocationForJob(ctx, job.GitHubJobID)
		if err != nil {
			return err
		}
		if existing == uuid.Nil {
			return fmt.Errorf("github runner allocation insert conflicted without active allocation for job %d", job.GitHubJobID)
		}
		span.SetAttributes(attribute.String("github.allocation_id", existing.String()), attribute.Bool("github.capacity.existing_allocation", true))
		return nil
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
	if err := r.service.storeQueries().UpdateGitHubAllocationJITCreated(ctx, store.UpdateGitHubAllocationJITCreatedParams{
		ProviderRunnerID: runnerID,
		RunnerName:       firstNonEmpty(jit.Runner.Name, allocation.RunnerName),
		UpdatedAt:        pgTime(time.Now().UTC()),
		AllocationID:     allocationID,
	}); err != nil {
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
	qtx := store.New(tx)
	if err := qtx.InsertRunnerJobBinding(ctx, store.InsertRunnerJobBindingParams{
		BindingID:        uuid.New(),
		AllocationID:     allocationID,
		Provider:         RunnerProviderGitHub,
		ProviderJobID:    githubJobID,
		ProviderRunnerID: job.runnerID,
		RunnerName:       job.runnerName,
		BoundAt:          pgTime(now),
	}); err != nil {
		return err
	}
	state := "assigned"
	if job.status == "completed" {
		state = "job_completed"
	}
	if err := qtx.UpdateRunnerAllocationAssignment(ctx, store.UpdateRunnerAllocationAssignmentParams{
		State:        state,
		UpdatedAt:    pgTime(now),
		CleanupBy:    pgTime(now.Add(30 * time.Minute)),
		Provider:     RunnerProviderGitHub,
		AllocationID: allocationID,
	}); err != nil {
		return err
	}
	if err := qtx.UpdateRunnerExecutionExternalTaskID(ctx, store.UpdateRunnerExecutionExternalTaskIDParams{
		ExternalTaskID: strconv.FormatInt(githubJobID, 10),
		UpdatedAt:      pgTime(now),
		AllocationID:   allocationID,
	}); err != nil {
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
	allocation, err := r.loadAllocationForCleanup(ctx, allocationID)
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
	if err := r.service.storeQueries().MarkRunnerAllocationCleaned(ctx, store.MarkRunnerAllocationCleanedParams{
		CleanupBy:    pgTime(now),
		AllocationID: allocationID,
	}); err != nil {
		return err
	}
	_ = r.service.storeQueries().DeleteRunnerBootstrapConfig(ctx, store.DeleteRunnerBootstrapConfigParams{AllocationID: allocationID})
	return nil
}

func (r *GitHubRunner) execEnv(ctx context.Context, executionID, attemptID uuid.UUID) map[string]string {
	allocationID, err := r.service.storeQueries().GetGitHubAllocationIDByExecution(ctx, store.GetGitHubAllocationIDByExecutionParams{ExecutionID: &executionID})
	if err != nil {
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
	row, err := r.service.storeQueries().GetGitHubQueuedJob(ctx, store.GetGitHubQueuedJobParams{ProviderJobID: githubJobID})
	if err != nil {
		return githubQueuedJob{}, err
	}
	job := githubQueuedJob{
		GitHubJobID:        row.ProviderJobID,
		InstallationID:     row.ProviderInstallationID,
		RepositoryID:       row.ProviderRepositoryID,
		RepositoryFullName: row.RepositoryFullName,
		RunID:              row.ProviderRunID,
		JobName:            row.JobName,
		HeadSHA:            row.HeadSha,
		HeadBranch:         row.HeadBranch,
		OrgID:              orgIDFromDB(row.OrgID),
		AccountLogin:       row.AccountLogin,
	}
	_ = json.Unmarshal(row.LabelsJson, &job.Labels)
	return job, nil
}

func (r *GitHubRunner) runnerClassForLabels(ctx context.Context, labels []string) (string, dto.VMResources, string, error) {
	for _, label := range labels {
		label = strings.TrimSpace(label)
		if label == "" {
			continue
		}
		resources, productID, ok, err := r.service.runnerClassResources(ctx, label)
		if err != nil {
			return "", dto.VMResources{}, "", err
		}
		if !ok {
			continue
		}
		return label, resources, productID, nil
	}
	return "", dto.VMResources{}, "", nil
}

func (r *GitHubRunner) activeAllocationForJob(ctx context.Context, githubJobID int64) (uuid.UUID, error) {
	allocationID, err := r.service.storeQueries().GetActiveAllocationForRunnerJob(ctx, store.GetActiveAllocationForRunnerJobParams{
		Provider:      RunnerProviderGitHub,
		ProviderJobID: githubJobID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, nil
	}
	return allocationID, err
}

func (r *GitHubRunner) loadAllocation(ctx context.Context, allocationID uuid.UUID) (githubAllocation, error) {
	row, err := r.service.storeQueries().GetGitHubAllocation(ctx, store.GetGitHubAllocationParams{AllocationID: allocationID})
	if err != nil {
		return githubAllocation{}, err
	}
	return githubAllocationFromGetRow(row), nil
}

func (r *GitHubRunner) loadAllocationForCleanup(ctx context.Context, allocationID uuid.UUID) (githubAllocation, error) {
	row, err := r.service.storeQueries().GetGitHubAllocationForCleanup(ctx, store.GetGitHubAllocationForCleanupParams{AllocationID: allocationID})
	if err != nil {
		return githubAllocation{}, err
	}
	return githubAllocationFromCleanupRow(row), nil
}

func (r *GitHubRunner) setAllocationState(ctx context.Context, allocationID uuid.UUID, state, reason string) error {
	return r.service.storeQueries().SetRunnerAllocationState(ctx, store.SetRunnerAllocationStateParams{
		State:         state,
		FailureReason: strings.TrimSpace(reason),
		UpdatedAt:     pgTime(time.Now().UTC()),
		Provider:      RunnerProviderGitHub,
		AllocationID:  allocationID,
	})
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
	row, err := r.service.storeQueries().GetRunnerJobForBinding(ctx, store.GetRunnerJobForBindingParams{
		Provider:      RunnerProviderGitHub,
		ProviderJobID: githubJobID,
	})
	if err != nil {
		return out, err
	}
	out.runnerID = row.RunnerID
	out.runnerName = row.RunnerName
	out.status = row.Status
	return out, nil
}

func (r *GitHubRunner) findAllocationForRunner(ctx context.Context, runnerID int64, runnerName string) (uuid.UUID, error) {
	return r.service.storeQueries().FindAllocationForRunner(ctx, store.FindAllocationForRunnerParams{
		Provider:         RunnerProviderGitHub,
		ProviderRunnerID: runnerID,
		RunnerName:       runnerName,
	})
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
	r.tokens[installationID] = githubInstallationToken(resp)
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

func (r *GitHubRunner) exchangeUserAccessToken(ctx context.Context, code string) (string, error) {
	values := url.Values{}
	values.Set("client_id", r.cfg.ClientID)
	values.Set("client_secret", r.cfg.ClientSecret)
	values.Set("code", code)
	webBase := strings.TrimRight(firstNonEmpty(r.cfg.WebBaseURL, "https://github.com"), "/")
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, webBase+"/login/oauth/access_token", strings.NewReader(values.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", "verself-sandbox-rental")
	resp, err := r.client.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	var out githubOAuthTokenResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&out); err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK || strings.TrimSpace(out.Error) != "" || strings.TrimSpace(out.AccessToken) == "" {
		return "", fmt.Errorf("%w: github user authorization failed: %s", ErrGitHubInstallationInvalid, firstNonEmpty(out.Description, out.Error, resp.Status))
	}
	return out.AccessToken, nil
}

func (r *GitHubRunner) verifyUserInstallationAccess(ctx context.Context, userToken string, installationID int64) error {
	const perPage = 100
	for page := 1; ; page++ {
		var resp githubUserInstallationsResponse
		path := fmt.Sprintf("/user/installations?per_page=%d&page=%d", perPage, page)
		if err := r.githubRequest(ctx, http.MethodGet, path, userToken, nil, &resp, http.StatusOK); err != nil {
			return err
		}
		for _, installation := range resp.Installations {
			if installation.ID == installationID {
				return nil
			}
		}
		if len(resp.Installations) < perPage || page*perPage >= resp.TotalCount {
			break
		}
	}
	return ErrGitHubInstallationInvalid
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
	defer func() { _ = resp.Body.Close() }()
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
mkdir -p _work _diag _temp
exec ./run.sh --jitconfig "$(cat "$jit_file")"`
}

func traceInt64(key string, value int64) attribute.KeyValue {
	return attribute.Int64(key, value)
}

func githubAllocationFromGetRow(row store.GetGitHubAllocationRow) githubAllocation {
	out := githubAllocation{
		AllocationID:       row.AllocationID,
		InstallationID:     row.ProviderInstallationID,
		RepositoryID:       row.ProviderRepositoryID,
		RunnerClass:        row.RunnerClass,
		RunnerName:         row.RunnerName,
		GitHubRunnerID:     row.ProviderRunnerID,
		RequestedJobID:     row.RequestedForProviderJobID,
		RunID:              row.ProviderRunID,
		JobName:            row.JobName,
		HeadSHA:            row.HeadSha,
		HeadBranch:         row.HeadBranch,
		State:              row.State,
		OrgID:              orgIDFromDB(row.OrgID),
		AccountLogin:       row.AccountLogin,
		RepositoryFullName: row.RepositoryFullName,
		ProductID:          row.ProductID,
		Resources:          vmResourcesFromDB(row.Vcpus, row.MemoryMib, row.RootfsGib),
	}
	if row.ExecutionID != nil {
		out.ExecutionID = *row.ExecutionID
	}
	if row.AttemptID != nil {
		out.AttemptID = *row.AttemptID
	}
	return out
}

func githubAllocationFromCleanupRow(row store.GetGitHubAllocationForCleanupRow) githubAllocation {
	out := githubAllocation{
		AllocationID:       row.AllocationID,
		InstallationID:     row.ProviderInstallationID,
		RepositoryID:       row.ProviderRepositoryID,
		RunnerClass:        row.RunnerClass,
		RunnerName:         row.RunnerName,
		GitHubRunnerID:     row.ProviderRunnerID,
		RequestedJobID:     row.RequestedForProviderJobID,
		RunID:              row.ProviderRunID,
		JobName:            row.JobName,
		HeadSHA:            row.HeadSha,
		HeadBranch:         row.HeadBranch,
		State:              row.State,
		OrgID:              orgIDFromDB(row.OrgID),
		AccountLogin:       row.AccountLogin,
		RepositoryFullName: row.RepositoryFullName,
		ProductID:          row.ProductID,
		Resources:          vmResourcesFromDB(row.Vcpus, row.MemoryMib, row.RootfsGib),
	}
	if row.ExecutionID != nil {
		out.ExecutionID = *row.ExecutionID
	}
	if row.AttemptID != nil {
		out.AttemptID = *row.AttemptID
	}
	return out
}

func githubInstallationRecordFromListRow(row store.ListGitHubInstallationsRow) GitHubInstallationRecord {
	return GitHubInstallationRecord{
		InstallationID: row.InstallationID,
		OrgID:          orgIDFromDB(row.OrgID),
		AccountLogin:   row.AccountLogin,
		AccountType:    row.AccountType,
		Active:         row.Active,
		CreatedAt:      timeFromPG(row.CreatedAt),
		UpdatedAt:      timeFromPG(row.UpdatedAt),
	}
}

func githubInstallationRecordFromGetRow(row store.GetGitHubInstallationForOrgRow) GitHubInstallationRecord {
	return GitHubInstallationRecord{
		InstallationID: row.InstallationID,
		OrgID:          orgIDFromDB(row.OrgID),
		AccountLogin:   row.AccountLogin,
		AccountType:    row.AccountType,
		Active:         row.Active,
		CreatedAt:      timeFromPG(row.CreatedAt),
		UpdatedAt:      timeFromPG(row.UpdatedAt),
	}
}
