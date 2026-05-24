package githubintegration

import (
	"context"
	"crypto/hmac"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
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
	secretsclient "github.com/verself/secrets-service/client"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
)

const (
	ServiceName      = "github-integration-service"
	providerGitHub   = "github"
	githubAPIVersion = "2026-03-10"

	defaultAPIBaseURL                       = "https://api.github.com"
	defaultRunnerWorkFolder                 = "_work"
	defaultRunnerPrefix                     = "verself-"
	defaultRepositoryRunnerClassActiveLimit = 15
	runnerAssignmentCorrectionSwap          = "pairwise_swap"
	runnerAssignmentCorrectionTransfer      = "single_rebind"
	maxWebhookBytes                         = 1 << 20
)

var (
	ErrConfiguration         = errors.New("github integration configuration invalid")
	ErrWebhookRejected       = errors.New("github webhook rejected")
	ErrWebhookSignature      = errors.New("github webhook signature invalid")
	ErrDeliveryReplay        = errors.New("github delivery replay with different payload")
	ErrSandboxRejected       = errors.New("sandbox-rental-service rejected github provider evidence")
	ErrUnsupportedWebhook    = errors.New("github webhook event is unsupported")
	ErrRepositoryNotEnabled  = errors.New("github repository is not enabled")
	ErrRunnerClassUnresolved = errors.New("github runner class is unresolved")
	ErrIdempotencyMismatch   = errors.New("idempotency key reused with a different payload")
)

var tracer = otel.Tracer("github-integration-service")

type Config struct {
	AppID                            int64
	AppSlug                          string
	AppSetupURL                      string
	PrivateKeyPEM                    string
	WebhookSecret                    string
	APIBaseURL                       string
	OAuthClientID                    string
	OAuthClientSecret                string
	OAuthAuthorizeURL                string
	OAuthTokenURL                    string
	OAuthRedirectURL                 string
	UserTokenSecretPrefix            string
	RunnerGroupID                    int64
	RunnerClassPrefix                string
	RepositoryRunnerClassActiveLimit int32
	WorkerInterval                   time.Duration
	WorkerBatchSize                  int32
	MaxDeliveryTries                 int32
	Logger                           *slog.Logger
	PG                               *pgxpool.Pool
	CH                               chdriver.Conn
	Sandbox                          *sandboxrentalclient.Client
	Secrets                          *secretsclient.Client
	HTTPClient                       *http.Client
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

type runtimeBinding struct {
	OrgID                 string
	InstallationBindingID uuid.UUID
	RepositoryBindingID   uuid.UUID
}

func NewService(cfg Config) (*Service, error) {
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = http.DefaultClient
	}
	if cfg.APIBaseURL == "" {
		cfg.APIBaseURL = defaultAPIBaseURL
	}
	if cfg.OAuthAuthorizeURL == "" {
		cfg.OAuthAuthorizeURL = "https://github.com/login/oauth/authorize"
	}
	if cfg.OAuthTokenURL == "" {
		cfg.OAuthTokenURL = "https://github.com/login/oauth/access_token"
	}
	if cfg.UserTokenSecretPrefix == "" {
		cfg.UserTokenSecretPrefix = "github-integration-service.github-user-token."
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
	if cfg.RepositoryRunnerClassActiveLimit <= 0 {
		cfg.RepositoryRunnerClassActiveLimit = defaultRepositoryRunnerClassActiveLimit
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
			writeWebhookProblem(w, newWebhookProblemSet(webhookProblem{
				Type:      "urn:verself:problem:provider_webhook:invalid_request",
				Code:      "provider_webhook.method_not_allowed",
				Title:     "Method not allowed",
				Detail:    "GitHub webhooks must use POST.",
				Status:    http.StatusMethodNotAllowed,
				Phase:     "method_validation",
				Retryable: false,
			}))
			return
		}
		ctx, span := tracer.Start(r.Context(), "github.webhook.receive")
		defer span.End()

		started := time.Now().UTC()
		var requestProblems webhookProblemSet
		eventName := requiredWebhookHeader(r.Header, "X-GitHub-Event", &requestProblems)
		deliveryID := requiredWebhookHeader(r.Header, "X-GitHub-Delivery", &requestProblems)
		signature := requiredWebhookHeader(r.Header, "X-Hub-Signature-256", &requestProblems)
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxWebhookBytes))
		payloadSHA := sha256Hex(body)
		if err != nil {
			requestProblems.add(providerWebhookBodyProblem("GitHub webhook body could not be read within the configured request budget."))
		}
		if !requestProblems.empty() {
			if strings.TrimSpace(deliveryID) != "" {
				s.recordRejectedDelivery(ctx, deliveryID, firstNonEmpty(eventName, "unknown"), "", payloadSHA, requestProblems, started)
			}
			s.writeEvent(ctx, githubEvent{
				ObservedAt:  time.Now().UTC(),
				EventName:   "github.webhook.rejected",
				Result:      "failed",
				Reason:      requestProblems.reason(),
				DeliveryID:  deliveryID,
				Action:      eventName,
				StartedAt:   started,
				CompletedAt: time.Now().UTC(),
				AttributesJSON: mustJSON(map[string]string{
					"payload_sha256": payloadSHA,
				}),
			})
			writeWebhookProblem(w, requestProblems)
			return
		}
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
			problems := newWebhookProblemSet(providerWebhookSignatureProblem())
			s.recordRejectedDelivery(ctx, deliveryID, eventName, "", payloadSHA, problems, started)
			s.writeEvent(ctx, githubEvent{
				ObservedAt: started,
				EventName:  "github.webhook.rejected",
				Result:     "failed",
				Reason:     problems.reason(),
				DeliveryID: deliveryID,
			})
			writeWebhookProblem(w, problems)
			return
		}

		meta, err := parseWebhookMetadata(body)
		if err != nil {
			problems := newWebhookProblemSet(providerWebhookPayloadProblem("GitHub webhook payload could not be parsed."))
			s.recordRejectedDelivery(ctx, deliveryID, eventName, "", payloadSHA, problems, started)
			s.writeEvent(ctx, githubEvent{
				ObservedAt: started,
				EventName:  "github.webhook.rejected",
				Result:     "failed",
				Reason:     problems.reason(),
				DeliveryID: deliveryID,
				Action:     eventName,
			})
			writeWebhookProblem(w, problems)
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
			problems := newWebhookProblemSet(providerWebhookInboxProblem())
			if errors.Is(err, pgx.ErrNoRows) {
				problems = newWebhookProblemSet(providerWebhookReplayProblem())
			}
			s.writeEvent(ctx, githubEventFromMetadata(meta, "github.webhook.received", "failed", problems.reason(), started, time.Now().UTC()))
			writeWebhookProblem(w, problems)
			return
		}
		result := "accepted"
		if row.State != "accepted" && row.State != "retryable" {
			result = "duplicate"
		}
		s.writeEvent(ctx, githubEventFromMetadata(meta, "github.webhook.verified", result, "", started, time.Now().UTC()))
		if row.State == "accepted" || row.State == "retryable" {
			s.writeEvent(ctx, githubEventFromMetadata(meta, "github.delivery.enqueued", "succeeded", "", started, time.Now().UTC()))
		}
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(map[string]map[string]string{
			"accepted": {
				"status":      result,
				"delivery_id": deliveryID,
			},
		})
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
		if err := s.ProcessQueuedJobs(ctx); err != nil && s.cfg.Logger != nil {
			s.cfg.Logger.WarnContext(ctx, "github queued job reconciliation failed", "error", err)
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

func (s *Service) ProcessQueuedJobs(ctx context.Context) error {
	rows, err := s.queries.ListQueuedWorkflowJobsForRunnerSubmission(ctx, store.ListQueuedWorkflowJobsForRunnerSubmissionParams{
		LimitCount:        s.cfg.WorkerBatchSize,
		RunnerClassPrefix: pgText(s.cfg.RunnerClassPrefix),
	})
	if err != nil {
		return err
	}
	var firstErr error
	for _, row := range rows {
		if err := s.processQueuedJob(ctx, row); err != nil {
			firstErr = errors.Join(firstErr, err)
		}
	}
	return firstErr
}

func (s *Service) processQueuedJob(ctx context.Context, row store.ListQueuedWorkflowJobsForRunnerSubmissionRow) error {
	unlock, locked, err := s.tryQueuedJobLock(ctx, row.ProviderJobID)
	if err != nil || !locked {
		return err
	}
	defer unlock()
	event, err := workflowJobWebhookFromQueuedRow(row)
	if err != nil {
		return err
	}
	runnerClass, err := s.runnerClassForLabels(event.WorkflowJob.Labels)
	if err != nil {
		return nil
	}
	deliveryID := fmt.Sprintf("reconcile:%d:%d", event.WorkflowJob.RunID, event.WorkflowJob.ID)
	started := time.Now().UTC()
	meta := metadataFromWorkflowJob(deliveryID, event)
	meta.RunnerClass = runnerClass
	s.writeEvent(ctx, githubEventFromMetadata(meta, "github.job.demand.reconciled", "started", row.RegistrationState, started, started))
	if err := s.submitQueuedJob(ctx, event, deliveryID); err != nil {
		s.writeEvent(ctx, githubEventFromMetadata(meta, "github.job.demand.reconcile_failed", "failed", err.Error(), started, time.Now().UTC()))
		return fmt.Errorf("submit queued github job %d: %w", event.WorkflowJob.ID, err)
	}
	s.writeEvent(ctx, githubEventFromMetadata(meta, "github.job.demand.reconciled", "succeeded", row.RegistrationState, started, time.Now().UTC()))
	return nil
}

func (s *Service) tryRunnerClassLock(ctx context.Context, providerRepositoryID int64, runnerClass string) (func(), bool, error) {
	key1, key2 := runnerClassLockKey(providerRepositoryID, runnerClass)
	return s.tryAdvisoryLockPair(ctx, key1, key2)
}

func (s *Service) tryQueuedJobLock(ctx context.Context, providerJobID int64) (func(), bool, error) {
	return s.tryAdvisoryLock(ctx, providerJobID)
}

func (s *Service) tryAdvisoryLock(ctx context.Context, key int64) (func(), bool, error) {
	conn, err := s.cfg.PG.Acquire(ctx)
	if err != nil {
		return nil, false, err
	}
	release := func() { conn.Release() }
	var locked bool
	if err := conn.QueryRow(ctx, "SELECT pg_try_advisory_lock($1)", key).Scan(&locked); err != nil {
		release()
		return nil, false, err
	}
	if !locked {
		release()
		return nil, false, nil
	}
	unlock := func() {
		_, _ = conn.Exec(context.Background(), "SELECT pg_advisory_unlock($1)", key)
		release()
	}
	return unlock, true, nil
}

func (s *Service) tryAdvisoryLockPair(ctx context.Context, key1, key2 int32) (func(), bool, error) {
	conn, err := s.cfg.PG.Acquire(ctx)
	if err != nil {
		return nil, false, err
	}
	release := func() { conn.Release() }
	var locked bool
	if err := conn.QueryRow(ctx, "SELECT pg_try_advisory_lock($1, $2)", key1, key2).Scan(&locked); err != nil {
		release()
		return nil, false, err
	}
	if !locked {
		release()
		return nil, false, nil
	}
	unlock := func() {
		_, _ = conn.Exec(context.Background(), "SELECT pg_advisory_unlock($1, $2)", key1, key2)
		release()
	}
	return unlock, true, nil
}

func runnerClassLockKey(providerRepositoryID int64, runnerClass string) (int32, int32) {
	sum := sha256.Sum256([]byte("github-runner-class:" + strconv.FormatInt(providerRepositoryID, 10) + ":" + runnerClass))
	return bigEndianInt32(sum[0:4]), bigEndianInt32(sum[4:8])
}

func bigEndianInt32(raw []byte) int32 {
	const maxPgAdvisoryLockKey = uint32(1<<31 - 1)
	return int32(binary.BigEndian.Uint32(raw) & maxPgAdvisoryLockKey)
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
	if errors.Is(err, ErrUnsupportedWebhook) || errors.Is(err, ErrRepositoryNotEnabled) {
		problems := problemSetForDeliveryError(err, false)
		if markErr := s.updateDeliveryWithProblems(ctx, row.DeliveryID, problems, func(q *store.Queries) error {
			return q.MarkDeliveryIgnored(ctx, store.MarkDeliveryIgnoredParams{
				DeliveryID:  row.DeliveryID,
				ProcessedAt: pgTime(time.Now().UTC()),
			})
		}); markErr != nil {
			return markErr
		}
		s.writeEvent(ctx, githubEventFromMetadata(meta, "github.delivery.ignored", "ignored", problems.reason(), started, time.Now().UTC()))
		return nil
	}
	terminalFailure := terminalDeliveryError(err)
	if terminalFailure || row.AttemptCount >= s.cfg.MaxDeliveryTries {
		problems := problemSetForDeliveryError(err, false)
		if !terminalFailure {
			problems.add(providerWebhookAttemptsExhaustedProblem())
		}
		if markErr := s.updateDeliveryWithProblems(ctx, row.DeliveryID, problems, func(q *store.Queries) error {
			return q.MarkDeliveryFailed(ctx, store.MarkDeliveryFailedParams{
				DeliveryID: row.DeliveryID,
				FailedAt:   pgTime(time.Now().UTC()),
			})
		}); markErr != nil {
			return markErr
		}
		s.writeEvent(ctx, githubEventFromMetadata(meta, "github.delivery.failed", "failed", problems.reason(), started, time.Now().UTC()))
		return err
	}
	delay := retryDelay(row.AttemptCount)
	problems := problemSetForDeliveryError(err, true)
	if markErr := s.updateDeliveryWithProblems(ctx, row.DeliveryID, problems, func(q *store.Queries) error {
		return q.MarkDeliveryRetryable(ctx, store.MarkDeliveryRetryableParams{
			DeliveryID:    row.DeliveryID,
			NextAttemptAt: pgTime(time.Now().UTC().Add(delay)),
			UpdatedAt:     pgTime(time.Now().UTC()),
		})
	}); markErr != nil {
		return markErr
	}
	s.writeEvent(ctx, githubEventFromMetadata(meta, "github.delivery.retryable", "retryable", problems.reason(), started, time.Now().UTC()))
	return err
}

func terminalDeliveryError(err error) bool {
	return errors.Is(err, ErrWebhookRejected)
}

func (s *Service) updateDeliveryWithProblems(ctx context.Context, deliveryID string, problems webhookProblemSet, update func(*store.Queries) error) error {
	if s == nil || s.cfg.PG == nil {
		return ErrConfiguration
	}
	if problems.empty() {
		problems.add(providerWebhookProcessingProblem("Webhook delivery processing failed.", false))
	}
	tx, err := s.cfg.PG.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	q := s.queries.WithTx(tx)
	if err := appendWebhookDeliveryProblems(ctx, q, deliveryID, problems); err != nil {
		return err
	}
	if err := update(q); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func appendWebhookDeliveryProblems(ctx context.Context, q *store.Queries, deliveryID string, problems webhookProblemSet) error {
	for _, problem := range problems.problems {
		if problem.ObservedAt.IsZero() {
			problem.ObservedAt = time.Now().UTC()
		}
		if err := q.AppendWebhookDeliveryProblem(ctx, store.AppendWebhookDeliveryProblemParams{
			DeliveryID:  deliveryID,
			Phase:       problem.Phase,
			ProblemType: problem.Type,
			ProblemCode: problem.Code,
			Title:       problem.Title,
			Detail:      problem.Detail,
			Status:      problem.Status,
			Retryable:   problem.Retryable,
			Pointer:     problem.Pointer,
			ObservedAt:  pgTime(problem.ObservedAt),
		}); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) handleDelivery(ctx context.Context, row store.LockReadyDeliveriesRow) error {
	switch row.EventName {
	case "workflow_job":
		return s.handleWorkflowJobDelivery(ctx, row)
	case "installation":
		return s.handleInstallationDelivery(ctx, row)
	case "installation_repositories":
		return s.handleInstallationRepositoriesDelivery(ctx, row)
	case "github_app_authorization":
		return s.handleGitHubAppAuthorizationDelivery(ctx, row)
	case "repository":
		return s.handleRepositoryDelivery(ctx, row)
	default:
		return fmt.Errorf("%w: %s", ErrUnsupportedWebhook, row.EventName)
	}
}

func (s *Service) handleWorkflowJobDelivery(ctx context.Context, row store.LockReadyDeliveriesRow) error {
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
	binding, err := s.lookupRuntimeBinding(ctx, event.Installation.ID, event.Repository.ID)
	if err != nil {
		return err
	}
	event.OrgID = binding.OrgID
	event.InstallationBindingID = binding.InstallationBindingID
	event.RepositoryBindingID = binding.RepositoryBindingID
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
		return s.refreshRunAndJobs(ctx, runtimeBinding{
			OrgID:                 event.OrgID,
			InstallationBindingID: event.InstallationBindingID,
			RepositoryBindingID:   event.RepositoryBindingID,
		}, event.Installation.ID, event.Repository.ID, event.Repository.FullName, event.WorkflowJob.RunID, event.WorkflowJob.RunAttempt, row.DeliveryID)
	default:
		return nil
	}
}

func (s *Service) handleInstallationDelivery(ctx context.Context, row store.LockReadyDeliveriesRow) error {
	var event installationWebhook
	if err := json.Unmarshal(row.PayloadJson, &event); err != nil {
		return err
	}
	event.Action = firstNonEmpty(event.Action, row.Action)
	now := time.Now().UTC()
	if event.Installation.ID <= 0 {
		return fmt.Errorf("%w: missing installation id", ErrWebhookRejected)
	}
	installationState := ""
	if event.Action == "deleted" {
		installationState = "deleted"
	}
	if event.Action == "suspend" {
		installationState = "suspended"
	}
	if err := s.persistInstallationDetailsWithState(ctx, event.Installation, row.DeliveryID, now, installationState); err != nil {
		return err
	}
	if event.Action == "deleted" || event.Action == "suspend" {
		if err := s.queries.MarkInstallationBindingsRevokedByProvider(ctx, store.MarkInstallationBindingsRevokedByProviderParams{
			RevokedAt:              pgTime(now),
			ProviderInstallationID: event.Installation.ID,
		}); err != nil {
			return err
		}
	}
	for _, repo := range event.Repositories {
		if err := s.persistRepositoryDetails(ctx, event.Installation.ID, repo, now); err != nil {
			return err
		}
	}
	s.writeEvent(ctx, githubEvent{
		EventName:              "github.installation.webhook.processed",
		Result:                 "succeeded",
		DeliveryID:             row.DeliveryID,
		Action:                 event.Action,
		ProviderInstallationID: uint64FromInt64(event.Installation.ID),
		StartedAt:              now,
		CompletedAt:            time.Now().UTC(),
		AttributesJSON: mustJSON(map[string]string{
			"account_login": event.Installation.Account.Login,
		}),
	})
	return nil
}

func (s *Service) handleInstallationRepositoriesDelivery(ctx context.Context, row store.LockReadyDeliveriesRow) error {
	var event installationRepositoriesWebhook
	if err := json.Unmarshal(row.PayloadJson, &event); err != nil {
		return err
	}
	event.Action = firstNonEmpty(event.Action, row.Action)
	now := time.Now().UTC()
	if event.Installation.ID <= 0 {
		return fmt.Errorf("%w: missing installation id", ErrWebhookRejected)
	}
	if err := s.persistInstallationDetails(ctx, event.Installation, row.DeliveryID, now); err != nil {
		return err
	}
	for _, repo := range event.RepositoriesAdded {
		if err := s.persistRepositoryDetails(ctx, event.Installation.ID, repo, now); err != nil {
			return err
		}
	}
	for _, repo := range event.RepositoriesRemoved {
		if err := s.persistRepositoryDetails(ctx, event.Installation.ID, repo, now); err != nil {
			return err
		}
		if err := s.queries.UpsertInstallationRepository(ctx, store.UpsertInstallationRepositoryParams{
			ProviderInstallationID: event.Installation.ID,
			ProviderRepositoryID:   repo.ID,
			State:                  "removed",
			ObservedFromApiAt:      pgTime(now),
			UpdatedAt:              pgTime(now),
		}); err != nil {
			return err
		}
		if err := s.queries.MarkRepositoryBindingUnavailableByProvider(ctx, store.MarkRepositoryBindingUnavailableByProviderParams{
			UpdatedAt:              pgTime(now),
			ProviderInstallationID: event.Installation.ID,
			ProviderRepositoryID:   repo.ID,
		}); err != nil {
			return err
		}
	}
	s.writeEvent(ctx, githubEvent{
		EventName:              "github.installation.repositories.webhook.processed",
		Result:                 "succeeded",
		DeliveryID:             row.DeliveryID,
		Action:                 event.Action,
		ProviderInstallationID: uint64FromInt64(event.Installation.ID),
		StartedAt:              now,
		CompletedAt:            time.Now().UTC(),
		AttributesJSON: mustJSON(map[string]string{
			"repositories_added":   strconv.Itoa(len(event.RepositoriesAdded)),
			"repositories_removed": strconv.Itoa(len(event.RepositoriesRemoved)),
		}),
	})
	return nil
}

func (s *Service) handleGitHubAppAuthorizationDelivery(ctx context.Context, row store.LockReadyDeliveriesRow) error {
	var event githubAppAuthorizationWebhook
	if err := json.Unmarshal(row.PayloadJson, &event); err != nil {
		return err
	}
	event.Action = firstNonEmpty(event.Action, row.Action)
	now := time.Now().UTC()
	if event.Action == "revoked" && event.Sender.ID > 0 {
		if err := s.queries.RevokeUserAuthorizationsByGitHubUser(ctx, store.RevokeUserAuthorizationsByGitHubUserParams{
			RevokedAt:      pgTime(now),
			ProviderUserID: event.Sender.ID,
		}); err != nil {
			return err
		}
	}
	s.writeEvent(ctx, githubEvent{
		EventName:   "github.user_authorization.webhook.processed",
		Result:      "succeeded",
		DeliveryID:  row.DeliveryID,
		Action:      event.Action,
		StartedAt:   now,
		CompletedAt: time.Now().UTC(),
		AttributesJSON: mustJSON(map[string]string{
			"provider_user_id": strconv.FormatInt(event.Sender.ID, 10),
			"github_login":     event.Sender.Login,
		}),
	})
	return nil
}

func (s *Service) handleRepositoryDelivery(ctx context.Context, row store.LockReadyDeliveriesRow) error {
	var event repositoryWebhook
	if err := json.Unmarshal(row.PayloadJson, &event); err != nil {
		return err
	}
	event.Action = firstNonEmpty(event.Action, row.Action)
	now := time.Now().UTC()
	if event.Repository.ID <= 0 {
		return fmt.Errorf("%w: missing repository id", ErrWebhookRejected)
	}
	if event.Installation.ID > 0 {
		if err := s.persistInstallationDetails(ctx, event.Installation, row.DeliveryID, now); err != nil {
			return err
		}
	}
	repositoryState := ""
	if event.Action == "deleted" {
		event.Repository.Archived = true
		repositoryState = "deleted"
	}
	if event.Action == "transferred" {
		repositoryState = "transferred"
	}
	if err := s.persistRepositoryDetailsWithState(ctx, event.Installation.ID, event.Repository, now, repositoryState); err != nil {
		return err
	}
	if event.Action == "deleted" || event.Action == "transferred" {
		if err := s.queries.MarkRepositoryBindingUnavailableByProvider(ctx, store.MarkRepositoryBindingUnavailableByProviderParams{
			UpdatedAt:              pgTime(now),
			ProviderInstallationID: event.Installation.ID,
			ProviderRepositoryID:   event.Repository.ID,
		}); err != nil {
			return err
		}
	}
	s.writeEvent(ctx, githubEvent{
		EventName:              "github.repository.webhook.processed",
		Result:                 "succeeded",
		DeliveryID:             row.DeliveryID,
		Action:                 event.Action,
		ProviderInstallationID: uint64FromInt64(event.Installation.ID),
		ProviderRepositoryID:   uint64FromInt64(event.Repository.ID),
		RepositoryFullName:     event.Repository.FullName,
		StartedAt:              now,
		CompletedAt:            time.Now().UTC(),
	})
	return nil
}

func (s *Service) lookupRuntimeBinding(ctx context.Context, installationID, repositoryID int64) (runtimeBinding, error) {
	row, err := s.queries.LookupRuntimeBinding(ctx, store.LookupRuntimeBindingParams{
		ProviderInstallationID: installationID,
		ProviderRepositoryID:   repositoryID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return runtimeBinding{}, fmt.Errorf("%w: provider_installation_id=%d provider_repository_id=%d", ErrRepositoryNotEnabled, installationID, repositoryID)
	}
	if err != nil {
		return runtimeBinding{}, err
	}
	return runtimeBinding{
		OrgID:                 row.OrgID,
		InstallationBindingID: uuidFromPG(row.InstallationBindingID),
		RepositoryBindingID:   uuidFromPG(row.RepositoryBindingID),
	}, nil
}

func (s *Service) submitQueuedJob(ctx context.Context, event workflowJobWebhook, deliveryID string) error {
	started := time.Now().UTC()
	runnerClass, err := s.runnerClassForLabels(event.WorkflowJob.Labels)
	if err != nil {
		meta := metadataFromWorkflowJob(deliveryID, event)
		s.writeEvent(ctx, githubEventFromMetadata(meta, "github.job.demand.ignored", "ignored", err.Error(), started, time.Now().UTC()))
		return nil
	}
	runnerName, err := githubRunnerName(event.Repository.ID, event.WorkflowJob.RunID, event.WorkflowJob.RunAttempt, event.WorkflowJob.ID)
	if err != nil {
		return err
	}
	owner, _, ok := strings.Cut(event.Repository.FullName, "/")
	if !ok || owner == "" {
		return fmt.Errorf("%w: repository full_name must be owner/name", ErrWebhookRejected)
	}
	workflow := workflowObservation{
		OrgID:                  event.OrgID,
		InstallationBindingID:  event.InstallationBindingID,
		RepositoryBindingID:    event.RepositoryBindingID,
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
		workflow.OrgID = event.OrgID
		workflow.InstallationBindingID = event.InstallationBindingID
		workflow.RepositoryBindingID = event.RepositoryBindingID
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
		job, ok, err := s.fetchWorkflowRunJob(ctx, event.Installation.ID, event.Repository.ID, event.Repository.FullName, event.WorkflowJob.RunID, workflow.ProviderRunAttempt, event.WorkflowJob.ID)
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("%w: workflow job %d not found in provider run %d attempt %d", ErrWebhookRejected, event.WorkflowJob.ID, event.WorkflowJob.RunID, workflow.ProviderRunAttempt)
		}
		if err := s.persistWorkflowJobFromAPI(ctx, runtimeBinding{
			OrgID:                 event.OrgID,
			InstallationBindingID: event.InstallationBindingID,
			RepositoryBindingID:   event.RepositoryBindingID,
		}, event.Installation.ID, event.Repository.ID, event.Repository.FullName, job, deliveryID, time.Now().UTC()); err != nil {
			return err
		}
		if job.Status != "queued" {
			if err := s.observeSandboxJob(ctx, sandboxObservationFromAPI(event.Installation.ID, event.Repository.ID, event.Repository.FullName, job, deliveryID), workflow); err != nil {
				return err
			}
			meta := metadataFromAPIJob(deliveryID, event.Installation.ID, event.Repository.ID, event.Repository.FullName, job)
			s.writeEvent(ctx, githubEventFromMetadata(meta, "github.job.demand.ignored", "ignored", "provider_status:"+job.Status, started, time.Now().UTC()))
			return nil
		}
		event.WorkflowJob = workflowJobPayloadFromAPI(job)
	}
	cacheManifest, err := s.cacheManifestForWorkflow(ctx, event.Installation.ID, event.Repository.FullName, workflow)
	if err != nil {
		return err
	}
	if cacheManifest != nil {
		s.writeEvent(ctx, githubEventFromMetadata(metadataFromWorkflowJob(deliveryID, event), "github.cache_manifest.fetched", "succeeded", "", started, time.Now().UTC()))
	}
	shape, err := buildGitHubJobShape(event, workflow, runnerClass, cacheManifestContentSHA(cacheManifest))
	if err != nil {
		return err
	}
	if err := s.queries.UpsertJobShape(ctx, store.UpsertJobShapeParams{
		JobShapeID:             shape.JobShapeID,
		OrgID:                  event.OrgID,
		InstallationBindingID:  pgUUID(event.InstallationBindingID),
		RepositoryBindingID:    pgUUID(event.RepositoryBindingID),
		ProviderInstallationID: event.Installation.ID,
		ProviderRepositoryID:   event.Repository.ID,
		RepositoryFullName:     event.Repository.FullName,
		WorkflowPath:           shape.Shape.WorkflowPath,
		WorkflowName:           shape.Shape.WorkflowName,
		JobName:                shape.Shape.JobName,
		MatrixKey:              shape.Shape.MatrixKey,
		RunnerClass:            shape.Shape.RunnerClass,
		RunnerLabelsJson:       shape.LabelsJSON,
		CacheManifestSha256:    shape.Shape.CacheManifestSHA256,
		TrustClass:             shape.Shape.TrustClass,
		CanonicalJson:          shape.CanonicalJSON,
		UpdatedAt:              pgTime(time.Now().UTC()),
	}); err != nil {
		return err
	}
	demand, err := s.queries.EnsureProviderDemand(ctx, store.EnsureProviderDemandParams{
		DemandID:               pgUUID(uuid.New()),
		ProviderJobID:          event.WorkflowJob.ID,
		OrgID:                  event.OrgID,
		InstallationBindingID:  pgUUID(event.InstallationBindingID),
		RepositoryBindingID:    pgUUID(event.RepositoryBindingID),
		ProviderInstallationID: event.Installation.ID,
		ProviderRepositoryID:   event.Repository.ID,
		RepositoryFullName:     event.Repository.FullName,
		ProviderRunID:          event.WorkflowJob.RunID,
		ProviderRunAttempt:     event.WorkflowJob.RunAttempt,
		JobShapeID:             shape.JobShapeID,
		TrustClass:             shape.Shape.TrustClass,
		RunnerClass:            runnerClass,
		RunnerName:             runnerName,
		LastDeliveryID:         deliveryID,
		UpdatedAt:              pgTime(time.Now().UTC()),
	})
	if err != nil {
		return err
	}
	meta := metadataFromWorkflowJob(deliveryID, event)
	meta.RunnerClass = runnerClass
	meta.JobShapeID = demand.JobShapeID
	meta.TrustClass = demand.TrustClass
	if demand.State == "sandbox_submitted" {
		meta.RunnerID = demand.RunnerID
		meta.RunnerName = demand.RunnerName
		meta.AllocationID = uuidFromPG(demand.SandboxAllocationID)
		meta.ExecutionID = uuidFromPG(demand.SandboxExecutionID)
		meta.AttemptID = uuidFromPG(demand.SandboxAttemptID)
		s.writeEvent(ctx, githubEventFromMetadata(meta, "github.sandbox.submit.reused", "succeeded", "provider_demand:"+demand.State, started, time.Now().UTC()))
		return nil
	}
	unlockRunnerClass, locked, err := s.tryRunnerClassLock(ctx, event.Repository.ID, runnerClass)
	if err != nil {
		return err
	}
	if !locked {
		s.writeEvent(ctx, githubEventFromMetadata(meta, "github.job.demand.reconcile_deferred", "deferred", "runner_class_lock_busy", started, time.Now().UTC()))
		return nil
	}
	defer unlockRunnerClass()
	claim, err := s.queries.ClaimProviderDemandForJIT(ctx, store.ClaimProviderDemandForJITParams{
		ClaimedAt:                        pgTime(time.Now().UTC()),
		ProviderJobID:                    event.WorkflowJob.ID,
		RepositoryRunnerClassActiveLimit: int64(s.cfg.RepositoryRunnerClassActiveLimit),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		reason := "provider_demand:" + demand.State
		active, activeErr := s.queries.CountActiveRunnerRegistrationsForRunnerClass(ctx, store.CountActiveRunnerRegistrationsForRunnerClassParams{
			ProviderRepositoryID: event.Repository.ID,
			RunnerClass:          runnerClass,
		})
		if activeErr != nil {
			return activeErr
		}
		if active >= int64(s.cfg.RepositoryRunnerClassActiveLimit) {
			reason = fmt.Sprintf("runner_class_capacity_full:%d/%d", active, s.cfg.RepositoryRunnerClassActiveLimit)
		}
		s.writeEvent(ctx, githubEventFromMetadata(meta, "github.job.demand.reconcile_deferred", "deferred", reason, started, time.Now().UTC()))
		return nil
	}
	if err != nil {
		return err
	}
	jit, err := s.createJITConfig(ctx, event.Installation.ID, owner, claim.RunnerName, runnerClass)
	if err != nil {
		_ = s.queries.MarkProviderDemandFailed(ctx, store.MarkProviderDemandFailedParams{
			ProviderJobID: event.WorkflowJob.ID,
			State:         "jit_failed",
			FailureReason: truncate(err.Error(), 1024),
			UpdatedAt:     pgTime(time.Now().UTC()),
		})
		return err
	}
	runnerID := jit.Runner.ID
	if runnerID == 0 {
		return fmt.Errorf("github jit config response missing runner id")
	}
	runnerName = firstNonEmpty(jit.Runner.Name, claim.RunnerName)
	jitHash := sha256Hex([]byte(jit.EncodedJITConfig))
	now := time.Now().UTC()
	if err := s.queries.MarkProviderDemandJITCreated(ctx, store.MarkProviderDemandJITCreatedParams{
		ProviderJobID:   event.WorkflowJob.ID,
		RunnerID:        runnerID,
		RunnerName:      runnerName,
		JitConfigSha256: jitHash,
		UpdatedAt:       pgTime(now),
	}); err != nil {
		return err
	}
	if err := s.queries.UpsertRunnerRegistration(ctx, store.UpsertRunnerRegistrationParams{
		ProviderJobID:          event.WorkflowJob.ID,
		DemandID:               claim.DemandID,
		OrgID:                  event.OrgID,
		InstallationBindingID:  pgUUID(event.InstallationBindingID),
		RepositoryBindingID:    pgUUID(event.RepositoryBindingID),
		ProviderInstallationID: event.Installation.ID,
		ProviderRepositoryID:   event.Repository.ID,
		RunnerID:               runnerID,
		RunnerName:             runnerName,
		RunnerClass:            runnerClass,
		JitConfigSha256:        jitHash,
		State:                  "jit_created",
		UpdatedAt:              pgTime(now),
	}); err != nil {
		// A runner name collision here usually means GitHub rebound a previous
		// runner to a different queued job. Delete this newly minted runner and
		// return demand to a retryable state so the job does not stay queued.
		_ = s.deleteRunner(ctx, event.Installation.ID, owner, runnerID)
		_ = s.queries.MarkProviderDemandFailed(ctx, store.MarkProviderDemandFailedParams{
			ProviderJobID: event.WorkflowJob.ID,
			State:         "jit_failed",
			FailureReason: truncate(err.Error(), 1024),
			UpdatedAt:     pgTime(time.Now().UTC()),
		})
		return err
	}
	meta.RunnerID = runnerID
	meta.RunnerName = runnerName
	meta.RunnerClass = runnerClass
	s.writeEvent(ctx, githubEventFromMetadata(meta, "github.runner.registration.created", "succeeded", "", started, now))

	req := sandboxrentalclient.InternalSubmitRunnerJobRequest{Body: sandboxrentalclient.InternalSubmitRunnerJobInputBody{
		Observation:      sandboxObservationFromWebhook(event, deliveryID),
		RunnerName:       sandboxrentalclient.RunnerName(runnerName),
		RunnerID:         decimalPtr(runnerID),
		BootstrapKind:    sandboxrentalclient.RunnerBootstrapKind("github_jit"),
		BootstrapPayload: sandboxrentalclient.RunnerBootstrapPayload(jit.EncodedJITConfig),
		WorkflowRun:      ptr(sandboxWorkflowObservation(workflow)),
		CacheManifest:    cacheManifest,
	}}
	outboxHash, err := s.recordSandboxSubmitOutbox(ctx, event, runnerName, runnerID, runnerClass, jitHash, shape.JobShapeID)
	if err != nil {
		return err
	}
	resp, err := s.cfg.Sandbox.InternalSubmitRunnerJob(ctx, req)
	if err != nil {
		_ = s.deleteRunner(ctx, event.Installation.ID, owner, runnerID)
		_ = s.queries.MarkProviderOutboxFailed(ctx, store.MarkProviderOutboxFailedParams{
			CommandKind:   "sandbox_submit_runner_job",
			CommandSha256: outboxHash,
			State:         "retryable",
			FailureReason: truncate(err.Error(), 1024),
			UpdatedAt:     pgTime(time.Now().UTC()),
		})
		_ = s.queries.MarkProviderDemandFailed(ctx, store.MarkProviderDemandFailedParams{
			ProviderJobID: event.WorkflowJob.ID,
			State:         "sandbox_failed",
			FailureReason: truncate(err.Error(), 1024),
			UpdatedAt:     pgTime(time.Now().UTC()),
		})
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
		_ = s.queries.MarkProviderOutboxFailed(ctx, store.MarkProviderOutboxFailedParams{
			CommandKind:   "sandbox_submit_runner_job",
			CommandSha256: outboxHash,
			State:         "failed",
			FailureReason: truncate(reason, 1024),
			UpdatedAt:     pgTime(time.Now().UTC()),
		})
		_ = s.queries.MarkProviderDemandFailed(ctx, store.MarkProviderDemandFailedParams{
			ProviderJobID: event.WorkflowJob.ID,
			State:         "sandbox_failed",
			FailureReason: truncate(reason, 1024),
			UpdatedAt:     pgTime(time.Now().UTC()),
		})
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
	if err := s.queries.MarkProviderDemandSandboxSubmitted(ctx, store.MarkProviderDemandSandboxSubmittedParams{
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
	if err := s.queries.MarkProviderOutboxProcessed(ctx, store.MarkProviderOutboxProcessedParams{
		CommandKind:        "sandbox_submit_runner_job",
		CommandSha256:      outboxHash,
		SandboxExecutionID: pgUUID(submission.ExecutionID),
		SandboxAttemptID:   pgUUID(submission.AttemptID),
		ProcessedAt:        pgTime(time.Now().UTC()),
	}); err != nil {
		return err
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

func (s *Service) recordSandboxSubmitOutbox(ctx context.Context, event workflowJobWebhook, runnerName string, runnerID int64, runnerClass string, jitHash string, jobShapeID string) (string, error) {
	payload, err := json.Marshal(struct {
		CommandKind            string `json:"command_kind"`
		Provider               string `json:"provider"`
		ProviderInstallationID int64  `json:"provider_installation_id"`
		ProviderRepositoryID   int64  `json:"provider_repository_id"`
		ProviderRunID          int64  `json:"provider_run_id"`
		ProviderRunAttempt     int64  `json:"provider_run_attempt"`
		ProviderJobID          int64  `json:"provider_job_id"`
		RunnerName             string `json:"runner_name"`
		RunnerID               int64  `json:"runner_id"`
		RunnerClass            string `json:"runner_class"`
		JobShapeID             string `json:"job_shape_id"`
		JITConfigSHA256        string `json:"jit_config_sha256"`
	}{
		CommandKind:            "sandbox_submit_runner_job",
		Provider:               providerGitHub,
		ProviderInstallationID: event.Installation.ID,
		ProviderRepositoryID:   event.Repository.ID,
		ProviderRunID:          event.WorkflowJob.RunID,
		ProviderRunAttempt:     event.WorkflowJob.RunAttempt,
		ProviderJobID:          event.WorkflowJob.ID,
		RunnerName:             runnerName,
		RunnerID:               runnerID,
		RunnerClass:            runnerClass,
		JobShapeID:             jobShapeID,
		JITConfigSHA256:        jitHash,
	})
	if err != nil {
		return "", err
	}
	hash := sha256Hex(payload)
	now := time.Now().UTC()
	if err := s.queries.UpsertProviderOutboxCommand(ctx, store.UpsertProviderOutboxCommandParams{
		OutboxID:               pgUUID(uuid.New()),
		CommandKind:            "sandbox_submit_runner_job",
		OrgID:                  event.OrgID,
		InstallationBindingID:  pgUUID(event.InstallationBindingID),
		RepositoryBindingID:    pgUUID(event.RepositoryBindingID),
		ProviderJobID:          event.WorkflowJob.ID,
		ProviderRunID:          event.WorkflowJob.RunID,
		ProviderRunAttempt:     event.WorkflowJob.RunAttempt,
		ProviderInstallationID: event.Installation.ID,
		ProviderRepositoryID:   event.Repository.ID,
		CommandSha256:          hash,
		PayloadJson:            payload,
		NextAttemptAt:          pgTime(now),
		UpdatedAt:              pgTime(now),
	}); err != nil {
		return "", err
	}
	return hash, nil
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

func (s *Service) fetchWorkflowRunJob(ctx context.Context, installationID, repositoryID int64, repositoryFullName string, runID, runAttempt, jobID int64) (githubWorkflowJob, bool, error) {
	jobs, err := s.fetchWorkflowRunJobs(ctx, installationID, repositoryID, repositoryFullName, runID, runAttempt)
	if err != nil {
		return githubWorkflowJob{}, false, err
	}
	for _, job := range jobs {
		if job.ID == jobID {
			return job, true, nil
		}
	}
	return githubWorkflowJob{}, false, nil
}

func workflowJobPayloadFromAPI(job githubWorkflowJob) workflowJobPayload {
	return workflowJobPayload{
		ID:           job.ID,
		RunID:        job.RunID,
		RunAttempt:   job.RunAttempt,
		Name:         job.Name,
		Status:       job.Status,
		Conclusion:   job.Conclusion,
		Labels:       append([]string(nil), job.Labels...),
		RunnerID:     job.RunnerID,
		RunnerName:   job.RunnerName,
		HeadSHA:      job.HeadSHA,
		HeadBranch:   job.HeadBranch,
		WorkflowName: job.WorkflowName,
		StartedAt:    job.StartedAt,
		CompletedAt:  job.CompletedAt,
	}
}

func (s *Service) refreshRunAndJobs(ctx context.Context, binding runtimeBinding, installationID, repositoryID int64, repositoryFullName string, runID, runAttempt int64, deliveryID string) error {
	started := time.Now().UTC()
	s.writeEvent(ctx, githubEvent{
		ObservedAt:             started,
		EventName:              "github.provider.refresh.started",
		Result:                 "started",
		DeliveryID:             deliveryID,
		OrgID:                  binding.OrgID,
		InstallationBindingID:  binding.InstallationBindingID,
		RepositoryBindingID:    binding.RepositoryBindingID,
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
	workflow.OrgID = binding.OrgID
	workflow.InstallationBindingID = binding.InstallationBindingID
	workflow.RepositoryBindingID = binding.RepositoryBindingID
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
		if err := s.persistWorkflowJobFromAPI(ctx, binding, installationID, repositoryID, repositoryFullName, job, deliveryID, time.Now().UTC()); err != nil {
			return err
		}
		if err := s.recordRunnerAssignment(ctx, binding, installationID, repositoryID, repositoryFullName, job, deliveryID, started); err != nil {
			return err
		}
		obs := sandboxObservationFromAPI(installationID, repositoryID, repositoryFullName, job, deliveryID)
		if err := s.observeSandboxJob(ctx, obs, workflow); err != nil {
			return err
		}
		meta := metadataFromAPIJob(deliveryID, installationID, repositoryID, repositoryFullName, job)
		meta.OrgID = binding.OrgID
		meta.InstallationBindingID = binding.InstallationBindingID
		meta.RepositoryBindingID = binding.RepositoryBindingID
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
		OrgID:                  binding.OrgID,
		InstallationBindingID:  binding.InstallationBindingID,
		RepositoryBindingID:    binding.RepositoryBindingID,
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

func (s *Service) recordRunnerAssignment(ctx context.Context, binding runtimeBinding, installationID, repositoryID int64, repositoryFullName string, job githubWorkflowJob, deliveryID string, started time.Time) error {
	runnerName := strings.TrimSpace(job.RunnerName)
	if runnerName == "" {
		return nil
	}
	reg, err := s.queries.GetRunnerRegistrationByRunnerName(ctx, store.GetRunnerRegistrationByRunnerNameParams{RunnerName: runnerName})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if reg.State == "cleaned" || reg.State == "failed" {
		return nil
	}
	if reg.ProviderJobID == job.ID {
		return nil
	}
	now := time.Now().UTC()
	reason := fmt.Sprintf("runner moved from provider job %d to provider job %d", reg.ProviderJobID, job.ID)
	correctionKind, err := s.reassignRunnerRegistration(ctx, reg.ProviderJobID, job.ID, runnerName, reason, now)
	if err != nil {
		return err
	}
	s.writeEvent(ctx, githubEvent{
		ObservedAt:             now,
		EventName:              "github.runner.assignment.mismatch.corrected",
		Result:                 "corrected",
		Reason:                 reason,
		DeliveryID:             deliveryID,
		OrgID:                  binding.OrgID,
		InstallationBindingID:  binding.InstallationBindingID,
		RepositoryBindingID:    binding.RepositoryBindingID,
		ProviderInstallationID: uint64FromInt64(installationID),
		ProviderRepositoryID:   uint64FromInt64(repositoryID),
		ProviderRunID:          uint64FromInt64(job.RunID),
		ProviderRunAttempt:     uint64FromInt64(job.RunAttempt),
		ProviderJobID:          uint64FromInt64(job.ID),
		RepositoryFullName:     repositoryFullName,
		RunnerID:               uint64FromInt64(job.RunnerID),
		RunnerName:             runnerName,
		RunnerClass:            reg.RunnerClass,
		StartedAt:              started,
		CompletedAt:            now,
		AttributesJSON: mustJSON(map[string]string{
			"actual_provider_job_id":  strconv.FormatInt(job.ID, 10),
			"assumed_provider_job_id": strconv.FormatInt(reg.ProviderJobID, 10),
			"correction_kind":         correctionKind,
			"registration_state":      reg.State,
		}),
	})
	return nil
}

func (s *Service) reassignRunnerRegistration(ctx context.Context, fromProviderJobID, toProviderJobID int64, runnerName string, reason string, at time.Time) (string, error) {
	tx, err := s.cfg.PG.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	qtx := store.New(tx)
	target, err := qtx.GetRunnerRegistrationForJob(ctx, store.GetRunnerRegistrationForJobParams{ProviderJobID: toProviderJobID})
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return "", err
	}
	if err == nil && target.RunnerName != "" && target.RunnerName != runnerName && target.State != "cleaned" && target.State != "failed" {
		swappedDemands, err := qtx.SwapProviderDemandRunnerAssignments(ctx, store.SwapProviderDemandRunnerAssignmentsParams{
			FromProviderJobID: fromProviderJobID,
			ToProviderJobID:   toProviderJobID,
			FailureReason:     truncate(reason, 1024),
			UpdatedAt:         pgTime(at),
		})
		if err != nil {
			return "", err
		}
		if swappedDemands != 2 {
			return "", fmt.Errorf("github runner demand swap failed: from_provider_job_id=%d to_provider_job_id=%d rows=%d", fromProviderJobID, toProviderJobID, swappedDemands)
		}
		swappedRegistrations, err := qtx.SwapRunnerRegistrationJobs(ctx, store.SwapRunnerRegistrationJobsParams{
			ToProviderJobID:   toProviderJobID,
			RunnerName:        runnerName,
			UpdatedAt:         pgTime(at),
			FromProviderJobID: fromProviderJobID,
		})
		if err != nil {
			return "", err
		}
		if swappedRegistrations == 0 {
			return "", fmt.Errorf("github runner registration swap failed: from_provider_job_id=%d to_provider_job_id=%d runner_name=%s", fromProviderJobID, toProviderJobID, runnerName)
		}
		return runnerAssignmentCorrectionSwap, tx.Commit(ctx)
	}
	assigned, err := qtx.AssignProviderDemandToRunnerFromDemand(ctx, store.AssignProviderDemandToRunnerFromDemandParams{
		FromProviderJobID: fromProviderJobID,
		ToProviderJobID:   toProviderJobID,
		UpdatedAt:         pgTime(at),
	})
	if err != nil {
		return "", err
	}
	if assigned == 0 {
		return "", fmt.Errorf("github runner assignment target demand missing: from_provider_job_id=%d to_provider_job_id=%d", fromProviderJobID, toProviderJobID)
	}
	transferred, err := qtx.TransferRunnerRegistrationToJob(ctx, store.TransferRunnerRegistrationToJobParams{
		FromProviderJobID: fromProviderJobID,
		ToProviderJobID:   toProviderJobID,
		RunnerName:        runnerName,
		UpdatedAt:         pgTime(at),
	})
	if err != nil {
		return "", err
	}
	if transferred == 0 {
		return "", fmt.Errorf("github runner registration missing for reassignment: from_provider_job_id=%d runner_name=%s", fromProviderJobID, runnerName)
	}
	if _, err := qtx.ResetProviderDemandAfterRunnerReassignment(ctx, store.ResetProviderDemandAfterRunnerReassignmentParams{
		ProviderJobID: fromProviderJobID,
		RunnerName:    replacementGitHubRunnerName(fromProviderJobID),
		FailureReason: truncate(reason, 1024),
		UpdatedAt:     pgTime(at),
	}); err != nil {
		return "", err
	}
	return runnerAssignmentCorrectionTransfer, tx.Commit(ctx)
}

func replacementGitHubRunnerName(jobID int64) string {
	return fmt.Sprintf("verself-%d-%s", jobID, strings.ReplaceAll(uuid.NewString(), "-", "")[:10])
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

func (s *Service) recordRejectedDelivery(ctx context.Context, deliveryID, eventName, action, payloadSHA string, problems webhookProblemSet, at time.Time) {
	if err := s.persistRejectedDelivery(ctx, deliveryID, eventName, action, payloadSHA, problems, at); err != nil && s != nil && s.cfg.Logger != nil {
		s.cfg.Logger.WarnContext(ctx, "github rejected delivery record failed", "delivery_id", deliveryID, "error", err)
	}
}

func (s *Service) persistRejectedDelivery(ctx context.Context, deliveryID, eventName, action, payloadSHA string, problems webhookProblemSet, at time.Time) error {
	if s == nil || s.queries == nil || strings.TrimSpace(deliveryID) == "" {
		return nil
	}
	if problems.empty() {
		problems.add(providerWebhookProcessingProblem("Webhook delivery was rejected.", false))
	}
	if s.cfg.PG == nil {
		return ErrConfiguration
	}
	tx, err := s.cfg.PG.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	q := s.queries.WithTx(tx)
	if _, err := q.MarkDeliveryRejected(ctx, store.MarkDeliveryRejectedParams{
		DeliveryID:    deliveryID,
		EventName:     firstNonEmpty(eventName, "unknown"),
		Action:        action,
		PayloadSha256: firstNonEmpty(payloadSHA, sha256Hex(nil)),
		PayloadJson:   []byte(`{}`),
		ReceivedAt:    pgTime(at),
	}); err != nil {
		return err
	}
	if err := appendWebhookDeliveryProblems(ctx, q, deliveryID, problems); err != nil {
		return err
	}
	return tx.Commit(ctx)
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

func requiredWebhookHeader(header http.Header, name string, problems *webhookProblemSet) string {
	value, ok := singleHeader(header, name)
	if !ok && problems != nil {
		problems.add(providerWebhookHeaderProblem(name))
	}
	return value
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

func githubRunnerName(repositoryID, runID, runAttempt, jobID int64) (string, error) {
	if jobID <= 0 {
		return "", fmt.Errorf("%w: provider job id is required", ErrWebhookRejected)
	}
	sum := sha256.Sum256([]byte(fmt.Sprintf("%d:%d:%d:%d", repositoryID, runID, runAttempt, jobID)))
	return fmt.Sprintf("verself-%d-%s", jobID, hex.EncodeToString(sum[:])[:10]), nil
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

func pgText(value string) pgtype.Text {
	return pgtype.Text{String: value, Valid: true}
}

func pgUUID(id uuid.UUID) pgtype.UUID {
	if id == uuid.Nil {
		return pgtype.UUID{}
	}
	return pgtype.UUID{Bytes: id, Valid: true}
}

func uuidFromPG(id pgtype.UUID) uuid.UUID {
	if !id.Valid {
		return uuid.Nil
	}
	return uuid.UUID(id.Bytes)
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

func workflowJobWebhookFromQueuedRow(row store.ListQueuedWorkflowJobsForRunnerSubmissionRow) (workflowJobWebhook, error) {
	var labels []string
	if len(row.LabelsJson) > 0 {
		if err := json.Unmarshal(row.LabelsJson, &labels); err != nil {
			return workflowJobWebhook{}, err
		}
	}
	var event workflowJobWebhook
	event.Action = "queued"
	event.OrgID = row.OrgID
	event.InstallationBindingID = uuidFromPG(row.InstallationBindingID)
	event.RepositoryBindingID = uuidFromPG(row.RepositoryBindingID)
	event.Installation.ID = row.ProviderInstallationID
	event.Repository.ID = row.ProviderRepositoryID
	event.Repository.FullName = row.RepositoryFullName
	event.WorkflowJob = workflowJobPayload{
		ID:           row.ProviderJobID,
		RunID:        row.ProviderRunID,
		RunAttempt:   row.ProviderRunAttempt,
		Name:         row.JobName,
		Status:       row.Status,
		Conclusion:   row.Conclusion,
		Labels:       labels,
		HeadSHA:      row.HeadSha,
		HeadBranch:   row.HeadBranch,
		WorkflowName: row.WorkflowName,
		StartedAt:    timeFromPG(row.StartedAt),
		CompletedAt:  timeFromPG(row.CompletedAt),
	}
	return event, nil
}

func timeFromPG(value pgtype.Timestamptz) time.Time {
	if !value.Valid {
		return time.Time{}
	}
	return value.Time.UTC()
}
