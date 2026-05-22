package githubintegration

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	chdriver "github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/verself/github-integration-service/internal/store"
	sandboxrentalclient "github.com/verself/sandbox-rental-service/client"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
)

const (
	ServiceName      = "github-integration-service"
	providerGitHub   = "github"
	githubAPIVersion = "2026-03-10"

	defaultAPIBaseURL       = "https://api.github.com"
	defaultRunnerWorkFolder = "_work"
	defaultRunnerPrefix     = "verself-"
	maxWebhookBytes         = 1 << 20
)

var (
	ErrConfiguration         = errors.New("github integration configuration invalid")
	ErrWebhookRejected       = errors.New("github webhook rejected")
	ErrWebhookSignature      = errors.New("github webhook signature invalid")
	ErrDeliveryReplay        = errors.New("github delivery replay with different payload")
	ErrSandboxRejected       = errors.New("sandbox-rental-service rejected github provider evidence")
	ErrUnsupportedWebhook    = errors.New("github webhook event is unsupported")
	ErrRunnerClassUnresolved = errors.New("github runner class is unresolved")
)

var tracer = otel.Tracer("github-integration-service")

type Config struct {
	AppID             int64
	PrivateKeyPEM     string
	WebhookSecret     string
	APIBaseURL        string
	RunnerGroupID     int64
	RunnerClassPrefix string
	WorkerInterval    time.Duration
	WorkerBatchSize   int32
	MaxDeliveryTries  int32
	Logger            *slog.Logger
	PG                *pgxpool.Pool
	CH                chdriver.Conn
	Sandbox           *sandboxrentalclient.Client
	HTTPClient        *http.Client
}

type Service struct {
	cfg        Config
	privateKey *rsa.PrivateKey
	queries    *store.Queries
	client     *http.Client

	tokenMu sync.Mutex
	tokens  map[int64]githubInstallationToken
}

type githubInstallationToken struct {
	Token     string
	ExpiresAt time.Time
}

func NewService(cfg Config) (*Service, error) {
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = http.DefaultClient
	}
	if cfg.APIBaseURL == "" {
		cfg.APIBaseURL = defaultAPIBaseURL
	}
	if cfg.WorkerInterval <= 0 {
		cfg.WorkerInterval = 500 * time.Millisecond
	}
	if cfg.WorkerBatchSize <= 0 {
		cfg.WorkerBatchSize = 16
	}
	if cfg.MaxDeliveryTries <= 0 {
		cfg.MaxDeliveryTries = 8
	}
	if cfg.RunnerClassPrefix == "" {
		cfg.RunnerClassPrefix = defaultRunnerPrefix
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	if cfg.PG == nil {
		return nil, fmt.Errorf("%w: postgres pool is required", ErrConfiguration)
	}
	if cfg.Sandbox == nil {
		return nil, fmt.Errorf("%w: sandbox client is required", ErrConfiguration)
	}
	if cfg.AppID <= 0 {
		return nil, fmt.Errorf("%w: github app id is required", ErrConfiguration)
	}
	if strings.TrimSpace(cfg.PrivateKeyPEM) == "" {
		return nil, fmt.Errorf("%w: github app private key is required", ErrConfiguration)
	}
	if strings.TrimSpace(cfg.WebhookSecret) == "" {
		return nil, fmt.Errorf("%w: github webhook secret is required", ErrConfiguration)
	}
	if cfg.RunnerGroupID <= 0 {
		return nil, fmt.Errorf("%w: github runner group id is required", ErrConfiguration)
	}
	key, err := jwt.ParseRSAPrivateKeyFromPEM([]byte(cfg.PrivateKeyPEM))
	if err != nil {
		return nil, fmt.Errorf("%w: parse github app private key: %v", ErrConfiguration, err)
	}
	return &Service{
		cfg:        cfg,
		privateKey: key,
		queries:    store.New(cfg.PG),
		client:     cfg.HTTPClient,
		tokens:     map[int64]githubInstallationToken{},
	}, nil
}

func (s *Service) Ready(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	if s.cfg.PG != nil {
		if err := s.cfg.PG.Ping(ctx); err != nil {
			return fmt.Errorf("postgres readiness: %w", err)
		}
	}
	if s.cfg.CH != nil {
		if err := s.cfg.CH.Ping(ctx); err != nil {
			return fmt.Errorf("clickhouse readiness: %w", err)
		}
	}
	return nil
}

func (s *Service) WebhookHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		ctx, span := tracer.Start(r.Context(), "github.webhook.receive")
		defer span.End()

		started := time.Now().UTC()
		eventName, ok := singleHeader(r.Header, "X-GitHub-Event")
		if !ok {
			http.Error(w, "missing github event", http.StatusBadRequest)
			return
		}
		deliveryID, ok := singleHeader(r.Header, "X-GitHub-Delivery")
		if !ok {
			http.Error(w, "missing github delivery id", http.StatusBadRequest)
			return
		}
		signature, ok := singleHeader(r.Header, "X-Hub-Signature-256")
		if !ok {
			http.Error(w, "missing github signature", http.StatusBadRequest)
			return
		}
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxWebhookBytes))
		if err != nil {
			http.Error(w, "invalid webhook body", http.StatusBadRequest)
			return
		}
		payloadSHA := sha256Hex(body)
		receivedAt := time.Now().UTC()
		s.writeEvent(ctx, githubEvent{
			ObservedAt:  receivedAt,
			EventName:   "github.webhook.received",
			Result:      "received",
			DeliveryID:  deliveryID,
			Action:      eventName,
			StartedAt:   started,
			CompletedAt: receivedAt,
			AttributesJSON: mustJSON(map[string]string{
				"payload_sha256": payloadSHA,
			}),
		})
		span.SetAttributes(
			attribute.String("github.webhook.event", eventName),
			attribute.String("github.webhook.delivery_id", deliveryID),
			attribute.String("github.webhook.payload_sha256", payloadSHA),
		)
		if err := verifyGitHubSignature(s.cfg.WebhookSecret, body, signature); err != nil {
			s.recordRejectedDelivery(ctx, deliveryID, eventName, "", payloadSHA, "signature_invalid", started)
			s.writeEvent(ctx, githubEvent{
				ObservedAt: started,
				EventName:  "github.webhook.rejected",
				Result:     "failed",
				Reason:     "signature_invalid",
				DeliveryID: deliveryID,
			})
			http.Error(w, "invalid signature", http.StatusUnauthorized)
			return
		}

		meta, err := parseWebhookMetadata(body)
		if err != nil {
			s.recordRejectedDelivery(ctx, deliveryID, eventName, "", payloadSHA, "payload_invalid", started)
			http.Error(w, "invalid webhook payload", http.StatusBadRequest)
			return
		}
		meta.EventName = eventName
		meta.DeliveryID = deliveryID
		meta.PayloadSHA256 = payloadSHA
		row, err := s.queries.RecordWebhookDelivery(ctx, store.RecordWebhookDeliveryParams{
			DeliveryID:             deliveryID,
			EventName:              eventName,
			Action:                 meta.Action,
			PayloadSha256:          payloadSHA,
			PayloadJson:            body,
			ProviderInstallationID: meta.InstallationID,
			ProviderRepositoryID:   meta.RepositoryID,
			RepositoryFullName:     meta.RepositoryFullName,
			ProviderRunID:          meta.RunID,
			ProviderRunAttempt:     meta.RunAttempt,
			ProviderJobID:          meta.JobID,
			ReceivedAt:             pgTime(started),
			VerifiedAt:             pgTime(time.Now().UTC()),
		})
		if err != nil {
			status := http.StatusInternalServerError
			if errors.Is(err, pgx.ErrNoRows) {
				status = http.StatusConflict
				err = ErrDeliveryReplay
			}
			s.writeEvent(ctx, githubEventFromMetadata(meta, "github.webhook.received", "failed", err.Error(), started, time.Now().UTC()))
			http.Error(w, "delivery rejected", status)
			return
		}
		result := "accepted"
		if row.State == "processed" || row.State == "ignored" {
			result = "duplicate"
		}
		s.writeEvent(ctx, githubEventFromMetadata(meta, "github.webhook.verified", result, "", started, time.Now().UTC()))
		if row.State == "verified" || row.State == "retryable" {
			s.writeEvent(ctx, githubEventFromMetadata(meta, "github.delivery.enqueued", "succeeded", "", started, time.Now().UTC()))
		}
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte("accepted\n"))
	})
}

func (s *Service) RunWorker(ctx context.Context) error {
	if s == nil {
		return ErrConfiguration
	}
	ticker := time.NewTicker(s.cfg.WorkerInterval)
	defer ticker.Stop()
	for {
		if err := s.ProcessReadyDeliveries(ctx); err != nil && s.cfg.Logger != nil {
			s.cfg.Logger.WarnContext(ctx, "github delivery processing failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func (s *Service) ProcessReadyDeliveries(ctx context.Context) error {
	rows, err := s.queries.LockReadyDeliveries(ctx, store.LockReadyDeliveriesParams{
		LockedAt:   pgTime(time.Now().UTC()),
		LimitCount: s.cfg.WorkerBatchSize,
	})
	if err != nil {
		return err
	}
	for _, row := range rows {
		if err := s.processLockedDelivery(ctx, row); err != nil && s.cfg.Logger != nil {
			s.cfg.Logger.WarnContext(ctx, "github delivery processing failed", "delivery_id", row.DeliveryID, "error", err)
		}
	}
	return nil
}

func (s *Service) processLockedDelivery(ctx context.Context, row store.LockReadyDeliveriesRow) error {
	started := time.Now().UTC()
	meta := webhookMetadata{
		EventName:          row.EventName,
		DeliveryID:         row.DeliveryID,
		InstallationID:     row.ProviderInstallationID,
		RepositoryID:       row.ProviderRepositoryID,
		RepositoryFullName: row.RepositoryFullName,
		RunID:              row.ProviderRunID,
		RunAttempt:         row.ProviderRunAttempt,
		JobID:              row.ProviderJobID,
		Action:             row.Action,
	}
	err := s.handleDelivery(ctx, row)
	if err == nil {
		if markErr := s.queries.MarkDeliveryProcessed(ctx, store.MarkDeliveryProcessedParams{DeliveryID: row.DeliveryID, ProcessedAt: pgTime(time.Now().UTC())}); markErr != nil {
			return markErr
		}
		s.writeEvent(ctx, githubEventFromMetadata(meta, "github.delivery.processed", "succeeded", "", started, time.Now().UTC()))
		return nil
	}
	if errors.Is(err, ErrUnsupportedWebhook) {
		if markErr := s.queries.MarkDeliveryIgnored(ctx, store.MarkDeliveryIgnoredParams{
			DeliveryID:    row.DeliveryID,
			FailureReason: err.Error(),
			ProcessedAt:   pgTime(time.Now().UTC()),
		}); markErr != nil {
			return markErr
		}
		s.writeEvent(ctx, githubEventFromMetadata(meta, "github.delivery.ignored", "ignored", err.Error(), started, time.Now().UTC()))
		return nil
	}
	if row.AttemptCount >= s.cfg.MaxDeliveryTries {
		if markErr := s.queries.MarkDeliveryFailed(ctx, store.MarkDeliveryFailedParams{
			DeliveryID:    row.DeliveryID,
			FailureReason: truncate(err.Error(), 1024),
			FailedAt:      pgTime(time.Now().UTC()),
		}); markErr != nil {
			return markErr
		}
		s.writeEvent(ctx, githubEventFromMetadata(meta, "github.delivery.failed", "failed", err.Error(), started, time.Now().UTC()))
		return err
	}
	delay := retryDelay(row.AttemptCount)
	if markErr := s.queries.MarkDeliveryRetryable(ctx, store.MarkDeliveryRetryableParams{
		DeliveryID:    row.DeliveryID,
		FailureReason: truncate(err.Error(), 1024),
		NextAttemptAt: pgTime(time.Now().UTC().Add(delay)),
		UpdatedAt:     pgTime(time.Now().UTC()),
	}); markErr != nil {
		return markErr
	}
	s.writeEvent(ctx, githubEventFromMetadata(meta, "github.delivery.retryable", "retryable", err.Error(), started, time.Now().UTC()))
	return err
}

func (s *Service) handleDelivery(ctx context.Context, row store.LockReadyDeliveriesRow) error {
	if row.EventName != "workflow_job" {
		return fmt.Errorf("%w: %s", ErrUnsupportedWebhook, row.EventName)
	}
	var event workflowJobWebhook
	if err := json.Unmarshal(row.PayloadJson, &event); err != nil {
		return err
	}
	if event.WorkflowJob.ID <= 0 || event.Repository.ID <= 0 || event.Installation.ID <= 0 {
		return fmt.Errorf("%w: missing workflow job identity", ErrWebhookRejected)
	}
	event.Action = firstNonEmpty(event.Action, row.Action)
	if event.WorkflowJob.RunAttempt == 0 {
		event.WorkflowJob.RunAttempt = 1
	}
	if event.Repository.FullName == "" {
		return fmt.Errorf("%w: repository full_name missing", ErrWebhookRejected)
	}
	now := time.Now().UTC()
	if err := s.persistWorkflowJob(ctx, event, row.DeliveryID, false, now); err != nil {
		return err
	}
	meta := metadataFromWorkflowJob(row.DeliveryID, event)
	switch event.Action {
	case "queued":
		s.writeEvent(ctx, githubEventFromMetadata(meta, "github.job.demand.recorded", "succeeded", "", now, now))
		return s.submitQueuedJob(ctx, event, row.DeliveryID)
	case "in_progress", "completed":
		return s.refreshRunAndJobs(ctx, event.Installation.ID, event.Repository.ID, event.Repository.FullName, event.WorkflowJob.RunID, event.WorkflowJob.RunAttempt, row.DeliveryID)
	default:
		return nil
	}
}

func (s *Service) submitQueuedJob(ctx context.Context, event workflowJobWebhook, deliveryID string) error {
	started := time.Now().UTC()
	runnerClass, err := s.runnerClassForLabels(event.WorkflowJob.Labels)
	if err != nil {
		meta := metadataFromWorkflowJob(deliveryID, event)
		s.writeEvent(ctx, githubEventFromMetadata(meta, "github.job.demand.ignored", "ignored", err.Error(), started, time.Now().UTC()))
		return nil
	}
	runnerName, err := githubRunnerName(event.WorkflowJob.ID)
	if err != nil {
		return err
	}
	owner, _, ok := strings.Cut(event.Repository.FullName, "/")
	if !ok || owner == "" {
		return fmt.Errorf("%w: repository full_name must be owner/name", ErrWebhookRejected)
	}
	workflow := workflowObservation{
		ProviderInstallationID: event.Installation.ID,
		ProviderRepositoryID:   event.Repository.ID,
		ProviderRunID:          event.WorkflowJob.RunID,
		ProviderRunAttempt:     event.WorkflowJob.RunAttempt,
		RepositoryFullName:     event.Repository.FullName,
		HeadSHA:                event.WorkflowJob.HeadSHA,
		HeadBranch:             event.WorkflowJob.HeadBranch,
		HeadRepositoryFullName: event.Repository.FullName,
	}
	if event.WorkflowJob.RunID > 0 {
		run, err := s.fetchWorkflowRun(ctx, event.Installation.ID, event.Repository.FullName, event.WorkflowJob.RunID)
		if err != nil {
			return err
		}
		workflow = workflowObservationFromRun(event.Installation.ID, event.Repository.ID, event.Repository.FullName, run)
		if workflow.ProviderRunAttempt == 0 {
			workflow.ProviderRunAttempt = event.WorkflowJob.RunAttempt
		}
		if workflow.ProviderRunAttempt == 0 {
			workflow.ProviderRunAttempt = 1
		}
		if workflow.HeadRepositoryFullName == "" {
			workflow.HeadRepositoryFullName = workflow.RepositoryFullName
		}
		if err := s.persistWorkflowRun(ctx, workflow, deliveryID, true, time.Now().UTC()); err != nil {
			return err
		}
		if err := s.observeSandboxWorkflowRun(ctx, workflow); err != nil {
			return err
		}
	}
	cacheManifest, err := s.cacheManifestForWorkflow(ctx, event.Installation.ID, event.Repository.FullName, workflow)
	if err != nil {
		return err
	}
	if cacheManifest != nil {
		s.writeEvent(ctx, githubEventFromMetadata(metadataFromWorkflowJob(deliveryID, event), "github.cache_manifest.fetched", "succeeded", "", started, time.Now().UTC()))
	}
	jit, err := s.createJITConfig(ctx, event.Installation.ID, owner, runnerName, runnerClass)
	if err != nil {
		return err
	}
	runnerID := jit.Runner.ID
	if runnerID == 0 {
		return fmt.Errorf("github jit config response missing runner id")
	}
	runnerName = firstNonEmpty(jit.Runner.Name, runnerName)
	jitHash := sha256Hex([]byte(jit.EncodedJITConfig))
	now := time.Now().UTC()
	if err := s.queries.UpsertRunnerRegistration(ctx, store.UpsertRunnerRegistrationParams{
		ProviderJobID:          event.WorkflowJob.ID,
		ProviderInstallationID: event.Installation.ID,
		ProviderRepositoryID:   event.Repository.ID,
		RunnerID:               runnerID,
		RunnerName:             runnerName,
		RunnerClass:            runnerClass,
		JitConfigSha256:        jitHash,
		State:                  "jit_created",
		UpdatedAt:              pgTime(now),
	}); err != nil {
		return err
	}
	meta := metadataFromWorkflowJob(deliveryID, event)
	meta.RunnerID = runnerID
	meta.RunnerName = runnerName
	meta.RunnerClass = runnerClass
	s.writeEvent(ctx, githubEventFromMetadata(meta, "github.runner.registration.created", "succeeded", "", started, now))

	req := sandboxrentalclient.InternalSubmitRunnerJobRequest{Body: sandboxrentalclient.InternalSubmitRunnerJobInputBody{
		Observation:      sandboxObservationFromWebhook(event, deliveryID, runnerID, runnerName),
		RunnerName:       sandboxrentalclient.RunnerName(runnerName),
		RunnerID:         decimalPtr(runnerID),
		BootstrapKind:    sandboxrentalclient.RunnerBootstrapKind("github_jit"),
		BootstrapPayload: sandboxrentalclient.RunnerBootstrapPayload(jit.EncodedJITConfig),
		WorkflowRun:      ptr(sandboxWorkflowObservation(workflow)),
		CacheManifest:    cacheManifest,
	}}
	resp, err := s.cfg.Sandbox.InternalSubmitRunnerJob(ctx, req)
	if err != nil {
		_ = s.deleteRunner(ctx, event.Installation.ID, owner, runnerID)
		_ = s.queries.MarkRunnerRegistrationFailed(ctx, store.MarkRunnerRegistrationFailedParams{
			ProviderJobID: event.WorkflowJob.ID,
			FailureReason: truncate(err.Error(), 1024),
			UpdatedAt:     pgTime(time.Now().UTC()),
		})
		return err
	}
	if resp.Result == nil || resp.StatusCode != http.StatusCreated {
		_ = s.deleteRunner(ctx, event.Installation.ID, owner, runnerID)
		reason := sandboxProblem(resp)
		_ = s.queries.MarkRunnerRegistrationFailed(ctx, store.MarkRunnerRegistrationFailedParams{
			ProviderJobID: event.WorkflowJob.ID,
			FailureReason: truncate(reason, 1024),
			UpdatedAt:     pgTime(time.Now().UTC()),
		})
		return fmt.Errorf("%w: %s", ErrSandboxRejected, reason)
	}
	submission, err := sandboxSubmission(resp.Result)
	if err != nil {
		return err
	}
	if !submission.Created && (runnerID != submission.RunnerID || runnerName != submission.RunnerName) {
		_ = s.deleteRunner(ctx, event.Installation.ID, owner, runnerID)
		runnerID = submission.RunnerID
		runnerName = firstNonEmpty(submission.RunnerName, runnerName)
		meta.RunnerID = runnerID
		meta.RunnerName = runnerName
		s.writeEvent(ctx, githubEventFromMetadata(meta, "github.runner.registration.reused", "succeeded", "", started, time.Now().UTC()))
	}
	if err := s.queries.MarkRunnerRegistrationSubmitted(ctx, store.MarkRunnerRegistrationSubmittedParams{
		ProviderJobID:       event.WorkflowJob.ID,
		SandboxAllocationID: pgUUID(submission.AllocationID),
		SandboxExecutionID:  pgUUID(submission.ExecutionID),
		SandboxAttemptID:    pgUUID(submission.AttemptID),
		RunnerID:            runnerID,
		RunnerName:          runnerName,
		UpdatedAt:           pgTime(time.Now().UTC()),
	}); err != nil {
		return err
	}
	meta.AllocationID = submission.AllocationID
	meta.ExecutionID = submission.ExecutionID
	meta.AttemptID = submission.AttemptID
	s.writeEvent(ctx, githubEventFromMetadata(meta, "github.sandbox.submit.requested", "succeeded", "", started, time.Now().UTC()))
	return nil
}

func (s *Service) cacheManifestForWorkflow(ctx context.Context, installationID int64, repositoryFullName string, workflow workflowObservation) (*sandboxrentalclient.RunnerCacheManifest, error) {
	ref := githubCacheManifestRef(workflow)
	content, ok, err := s.fetchRepositoryFile(ctx, installationID, repositoryFullName, githubCacheManifestPath, ref)
	if err != nil || !ok {
		return nil, err
	}
	return &sandboxrentalclient.RunnerCacheManifest{
		SourceKind:    "manifest",
		SourcePath:    githubCacheManifestPath,
		SourceSHA:     ref,
		ContentBase64: sandboxrentalclient.RunnerCacheManifestContentBase64(base64.StdEncoding.EncodeToString(content)),
	}, nil
}

func githubCacheManifestRef(workflow workflowObservation) string {
	if workflow.PullRequestNumber != 0 {
		if ref := strings.TrimSpace(workflow.BaseSHA); ref != "" {
			return ref
		}
		if ref := strings.TrimSpace(workflow.BaseBranch); ref != "" {
			return ref
		}
	}
	if ref := strings.TrimSpace(workflow.HeadSHA); ref != "" {
		return ref
	}
	if ref := strings.TrimSpace(workflow.HeadBranch); ref != "" {
		return ref
	}
	return "main"
}

func (s *Service) refreshRunAndJobs(ctx context.Context, installationID, repositoryID int64, repositoryFullName string, runID, runAttempt int64, deliveryID string) error {
	started := time.Now().UTC()
	s.writeEvent(ctx, githubEvent{
		ObservedAt:             started,
		EventName:              "github.provider.refresh.started",
		Result:                 "started",
		DeliveryID:             deliveryID,
		ProviderInstallationID: uint64FromInt64(installationID),
		ProviderRepositoryID:   uint64FromInt64(repositoryID),
		ProviderRunID:          uint64FromInt64(runID),
		ProviderRunAttempt:     uint64FromInt64(runAttempt),
		RepositoryFullName:     repositoryFullName,
		StartedAt:              started,
		CompletedAt:            started,
	})
	run, err := s.fetchWorkflowRun(ctx, installationID, repositoryFullName, runID)
	if err != nil {
		return err
	}
	workflow := workflowObservationFromRun(installationID, repositoryID, repositoryFullName, run)
	if workflow.ProviderRunAttempt == 0 {
		workflow.ProviderRunAttempt = runAttempt
	}
	if workflow.ProviderRunAttempt == 0 {
		workflow.ProviderRunAttempt = 1
	}
	if workflow.HeadRepositoryFullName == "" {
		workflow.HeadRepositoryFullName = workflow.RepositoryFullName
	}
	if err := s.persistWorkflowRun(ctx, workflow, deliveryID, true, time.Now().UTC()); err != nil {
		return err
	}
	if err := s.observeSandboxWorkflowRun(ctx, workflow); err != nil {
		return err
	}
	jobs, err := s.fetchWorkflowRunJobs(ctx, installationID, repositoryID, repositoryFullName, runID, workflow.ProviderRunAttempt)
	if err != nil {
		return err
	}
	for _, job := range jobs {
		if job.RunAttempt == 0 {
			job.RunAttempt = workflow.ProviderRunAttempt
		}
		if err := s.persistWorkflowJobFromAPI(ctx, installationID, repositoryID, repositoryFullName, job, deliveryID, time.Now().UTC()); err != nil {
			return err
		}
		obs := sandboxObservationFromAPI(installationID, repositoryID, repositoryFullName, job, deliveryID)
		if err := s.observeSandboxJob(ctx, obs, workflow); err != nil {
			return err
		}
		meta := metadataFromAPIJob(deliveryID, installationID, repositoryID, repositoryFullName, job)
		if job.RunnerID != 0 || job.RunnerName != "" {
			s.writeEvent(ctx, githubEventFromMetadata(meta, "github.runner.assignment.observed", "succeeded", "", started, time.Now().UTC()))
		}
		if job.Status == "completed" {
			if err := s.insertTerminalEvidence(ctx, job, deliveryID); err != nil {
				return err
			}
			s.writeEvent(ctx, githubEventFromMetadata(meta, "github.job.terminal.observed", "succeeded", job.Conclusion, started, time.Now().UTC()))
			if err := s.cleanupRunnerForJob(ctx, installationID, repositoryFullName, job.ID); err != nil {
				return err
			}
			s.writeEvent(ctx, githubEventFromMetadata(meta, "github.terminal_evidence.emitted", "succeeded", job.Conclusion, started, time.Now().UTC()))
		}
	}
	s.writeEvent(ctx, githubEvent{
		ObservedAt:             time.Now().UTC(),
		EventName:              "github.provider.refresh.completed",
		Result:                 "succeeded",
		DeliveryID:             deliveryID,
		ProviderInstallationID: uint64FromInt64(installationID),
		ProviderRepositoryID:   uint64FromInt64(repositoryID),
		ProviderRunID:          uint64FromInt64(runID),
		ProviderRunAttempt:     uint64FromInt64(workflow.ProviderRunAttempt),
		RepositoryFullName:     repositoryFullName,
		StartedAt:              started,
		CompletedAt:            time.Now().UTC(),
	})
	return nil
}

func (s *Service) observeSandboxWorkflowRun(ctx context.Context, workflow workflowObservation) error {
	resp, err := s.cfg.Sandbox.InternalObserveRunnerWorkflowRun(ctx, sandboxrentalclient.InternalObserveRunnerWorkflowRunRequest{Body: sandboxrentalclient.InternalObserveRunnerWorkflowRunInputBody{
		WorkflowRun: sandboxWorkflowObservation(workflow),
	}})
	if err != nil {
		return err
	}
	if resp.Result == nil || resp.StatusCode != http.StatusAccepted {
		return fmt.Errorf("%w: %s", ErrSandboxRejected, sandboxProblem(resp))
	}
	return nil
}

func (s *Service) observeSandboxJob(ctx context.Context, obs sandboxrentalclient.RunnerJobObservation, workflow workflowObservation) error {
	resp, err := s.cfg.Sandbox.InternalObserveRunnerJob(ctx, sandboxrentalclient.InternalObserveRunnerJobRequest{Body: sandboxrentalclient.InternalObserveRunnerJobInputBody{
		Observation: obs,
		WorkflowRun: ptr(sandboxWorkflowObservation(workflow)),
	}})
	if err != nil {
		return err
	}
	if resp.Result == nil || resp.StatusCode != http.StatusAccepted {
		return fmt.Errorf("%w: %s", ErrSandboxRejected, sandboxProblem(resp))
	}
	return nil
}

func (s *Service) cleanupRunnerForJob(ctx context.Context, installationID int64, repositoryFullName string, jobID int64) error {
	reg, err := s.queries.GetRunnerRegistrationForJob(ctx, store.GetRunnerRegistrationForJobParams{ProviderJobID: jobID})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	owner, _, ok := strings.Cut(repositoryFullName, "/")
	if !ok || owner == "" {
		return fmt.Errorf("%w: repository full_name must be owner/name", ErrWebhookRejected)
	}
	if reg.RunnerID != 0 {
		if err := s.deleteRunner(ctx, installationID, owner, reg.RunnerID); err != nil {
			return err
		}
	} else if reg.RunnerName != "" {
		if err := s.deleteRunnerByName(ctx, installationID, owner, reg.RunnerName); err != nil {
			return err
		}
	}
	return s.queries.MarkRunnerRegistrationCleaned(ctx, store.MarkRunnerRegistrationCleanedParams{
		ProviderJobID: jobID,
		UpdatedAt:     pgTime(time.Now().UTC()),
	})
}

func (s *Service) recordRejectedDelivery(ctx context.Context, deliveryID, eventName, action, payloadSHA, reason string, at time.Time) {
	if s == nil || s.queries == nil || strings.TrimSpace(deliveryID) == "" {
		return
	}
	_ = s.queries.MarkDeliveryRejected(ctx, store.MarkDeliveryRejectedParams{
		DeliveryID:    deliveryID,
		EventName:     firstNonEmpty(eventName, "unknown"),
		Action:        action,
		FailureReason: reason,
		PayloadSha256: firstNonEmpty(payloadSHA, sha256Hex(nil)),
		PayloadJson:   []byte(`{}`),
		ReceivedAt:    pgTime(at),
	})
}

func (s *Service) runnerClassForLabels(labels []string) (string, error) {
	prefix := strings.TrimSpace(s.cfg.RunnerClassPrefix)
	for _, label := range labels {
		label = strings.TrimSpace(label)
		if label == "" {
			continue
		}
		if prefix != "" && strings.HasPrefix(label, prefix) {
			return label, nil
		}
	}
	return "", ErrRunnerClassUnresolved
}

func retryDelay(attempt int32) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	if attempt > 6 {
		attempt = 6
	}
	return time.Duration(1<<uint(attempt-1)) * time.Second
}

func singleHeader(header http.Header, name string) (string, bool) {
	values := header.Values(name)
	if len(values) != 1 {
		return "", false
	}
	value := strings.TrimSpace(values[0])
	return value, value != ""
}

func verifyGitHubSignature(secret string, payload []byte, signature string) error {
	secret = strings.TrimSpace(secret)
	signature = strings.TrimSpace(signature)
	if secret == "" {
		return fmt.Errorf("%w: missing secret", ErrWebhookSignature)
	}
	const prefix = "sha256="
	if !strings.HasPrefix(signature, prefix) {
		return fmt.Errorf("%w: missing sha256 prefix", ErrWebhookSignature)
	}
	got, err := hex.DecodeString(strings.TrimPrefix(signature, prefix))
	if err != nil {
		return fmt.Errorf("%w: decode signature: %v", ErrWebhookSignature, err)
	}
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(payload)
	if !hmac.Equal(got, mac.Sum(nil)) {
		return ErrWebhookSignature
	}
	return nil
}

func githubRunnerName(jobID int64) (string, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return fmt.Sprintf("verself-%d-%s", jobID, hex.EncodeToString(b[:])[:10]), nil
}

func createAppJWT(appID int64, key *rsa.PrivateKey) (string, error) {
	now := time.Now().UTC()
	claims := jwt.RegisteredClaims{
		Issuer:    fmt.Sprintf("%d", appID),
		IssuedAt:  jwt.NewNumericDate(now.Add(-60 * time.Second)),
		ExpiresAt: jwt.NewNumericDate(now.Add(9 * time.Minute)),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	return token.SignedString(key)
}

func sha256Hex(payload []byte) string {
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

func pgTime(t time.Time) pgtype.Timestamptz {
	if t.IsZero() {
		return pgtype.Timestamptz{}
	}
	return pgtype.Timestamptz{Time: t.UTC(), Valid: true}
}

func pgUUID(id uuid.UUID) pgtype.UUID {
	if id == uuid.Nil {
		return pgtype.UUID{}
	}
	return pgtype.UUID{Bytes: id, Valid: true}
}

type sandboxSubmissionResult struct {
	AllocationID uuid.UUID
	ExecutionID  uuid.UUID
	AttemptID    uuid.UUID
	RunnerName   string
	RunnerID     int64
	Created      bool
}

func sandboxSubmission(out *sandboxrentalclient.InternalSubmitRunnerJobOutputBody) (sandboxSubmissionResult, error) {
	allocationID, err := uuid.Parse(string(out.AllocationID))
	if err != nil {
		return sandboxSubmissionResult{}, err
	}
	executionID, err := uuid.Parse(string(out.ExecutionID))
	if err != nil {
		return sandboxSubmissionResult{}, err
	}
	attemptID, err := uuid.Parse(string(out.AttemptID))
	if err != nil {
		return sandboxSubmissionResult{}, err
	}
	result := sandboxSubmissionResult{
		AllocationID: allocationID,
		ExecutionID:  executionID,
		AttemptID:    attemptID,
		RunnerName:   strings.TrimSpace(string(out.RunnerName)),
		Created:      out.Created,
	}
	if out.RunnerID != nil {
		runnerID, err := strconv.ParseInt(string(*out.RunnerID), 10, 64)
		if err != nil {
			return sandboxSubmissionResult{}, err
		}
		result.RunnerID = runnerID
	}
	return result, nil
}

func decimalPtr(value int64) *sandboxrentalclient.DecimalUint64 {
	if value <= 0 {
		return nil
	}
	out := sandboxrentalclient.DecimalUint64(fmt.Sprintf("%d", value))
	return &out
}

func stringPtr[T ~string](value string) *T {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	out := T(value)
	return &out
}

func ptr[T any](value T) *T {
	return &value
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func truncate(value string, limit int) string {
	value = strings.TrimSpace(value)
	if limit <= 0 || len(value) <= limit {
		return value
	}
	return value[:limit]
}
