package proof

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	workloadauth "github.com/forge-metal/auth-middleware/workload"
	"github.com/forge-metal/temporal-platform/internal/temporallog"
	"github.com/spiffe/go-spiffe/v2/spiffeid"
	"github.com/spiffe/go-spiffe/v2/spiffetls/tlsconfig"
	"github.com/spiffe/go-spiffe/v2/workloadapi"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	otelpropagation "go.opentelemetry.io/otel/propagation"
	"go.temporal.io/api/serviceerror"
	workflowservice "go.temporal.io/api/workflowservice/v1"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/client"
	sdkotel "go.temporal.io/sdk/contrib/opentelemetry"
	sdkinterceptor "go.temporal.io/sdk/interceptor"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/worker"
	"go.temporal.io/sdk/workflow"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/durationpb"
)

const (
	NamespaceName       = "temporal-proof"
	DeniedNamespaceName = "temporal-denied"
	TaskQueueName       = "proof-heartbeat"
	WorkflowName        = "ProofHeartbeat"
)

var tracer = otel.Tracer("github.com/forge-metal/temporal-platform/internal/proof")

type Config struct {
	HostPort            string
	Namespace           string
	ServerID            spiffeid.ID
	GovernanceURL       string
	GovernanceServerID  spiffeid.ID
	ServiceVersion      string
	NamespaceRetention  time.Duration
	WorkflowRunTimeout  time.Duration
	WorkflowTaskTimeout time.Duration
}

type StartResult struct {
	WorkflowID string    `json:"workflow_id"`
	RunID      string    `json:"run_id"`
	Namespace  string    `json:"namespace"`
	StartedAt  time.Time `json:"started_at"`
	SleepFor   string    `json:"sleep_for"`
}

type WorkflowResult struct {
	WorkflowID    string `json:"workflow_id"`
	RunID         string `json:"run_id"`
	AuditEventID  string `json:"audit_event_id"`
	AuditSequence uint64 `json:"audit_sequence"`
}

type WorkflowInput struct {
	RunID            string        `json:"run_id"`
	WorkflowID       string        `json:"workflow_id"`
	SleepFor         time.Duration `json:"sleep_for"`
	GovernanceURL    string        `json:"governance_url"`
	GovernanceServer string        `json:"governance_server_spiffe_id"`
	ServiceVersion   string        `json:"service_version"`
}

type AuditActivityInput struct {
	RunID            string `json:"run_id"`
	WorkflowID       string `json:"workflow_id"`
	GovernanceURL    string `json:"governance_url"`
	GovernanceServer string `json:"governance_server_spiffe_id"`
	ServiceVersion   string `json:"service_version"`
}

type AuditActivityResult struct {
	EventID  string `json:"event_id"`
	Sequence uint64 `json:"sequence"`
}

type auditEventResponse struct {
	EventID  string `json:"event_id"`
	Sequence uint64 `json:"sequence"`
}

type auditRecord struct {
	OrgID             string         `json:"org_id"`
	SourceProductArea string         `json:"source_product_area"`
	ServiceName       string         `json:"service_name"`
	ServiceVersion    string         `json:"service_version,omitempty"`
	ActorType         string         `json:"actor_type"`
	ActorID           string         `json:"actor_id"`
	ActorSPIFFEID     string         `json:"actor_spiffe_id,omitempty"`
	OperationID       string         `json:"operation_id"`
	AuditEvent        string         `json:"audit_event"`
	OperationDisplay  string         `json:"operation_display,omitempty"`
	OperationType     string         `json:"operation_type"`
	EventCategory     string         `json:"event_category"`
	RiskLevel         string         `json:"risk_level"`
	TargetKind        string         `json:"target_kind"`
	TargetID          string         `json:"target_id,omitempty"`
	Permission        string         `json:"permission,omitempty"`
	Action            string         `json:"action,omitempty"`
	OrgScope          string         `json:"org_scope,omitempty"`
	Result            string         `json:"result"`
	Payload           map[string]any `json:"payload,omitempty"`
}

type Activities struct {
	GovernanceURL      string
	GovernanceServerID spiffeid.ID
	Source             *workloadapi.X509Source
	ServiceVersion     string
}

func LoadConfigFromEnv() (Config, error) {
	cfg := Config{
		HostPort:            envOr("FM_TEMPORAL_FRONTEND_ADDRESS", "127.0.0.1:7233"),
		Namespace:           envOr("FM_TEMPORAL_PROOF_NAMESPACE", NamespaceName),
		GovernanceURL:       envOr("FM_TEMPORAL_PROOF_GOVERNANCE_URL", "https://127.0.0.1:4254/internal/v1/audit/events"),
		ServiceVersion:      envOr("FM_TEMPORAL_PROOF_SERVICE_VERSION", "dev"),
		NamespaceRetention:  envDuration("FM_TEMPORAL_PROOF_NAMESPACE_RETENTION", 24*time.Hour),
		WorkflowRunTimeout:  envDuration("FM_TEMPORAL_PROOF_WORKFLOW_RUN_TIMEOUT", 2*time.Minute),
		WorkflowTaskTimeout: envDuration("FM_TEMPORAL_PROOF_WORKFLOW_TASK_TIMEOUT", 10*time.Second),
	}
	var err error
	cfg.ServerID, err = parseSPIFFEIDEnv("FM_TEMPORAL_SERVER_SPIFFE_ID")
	if err != nil {
		return Config{}, err
	}
	cfg.GovernanceServerID, err = parseSPIFFEIDEnv("FM_TEMPORAL_PROOF_GOVERNANCE_SPIFFE_ID")
	if err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func NewSource(ctx context.Context, socket string) (*workloadapi.X509Source, error) {
	return workloadauth.Source(ctx, socket)
}

func BootstrapNamespaces(ctx context.Context, cfg Config, source *workloadapi.X509Source) error {
	namespaceClient, err := client.NewNamespaceClient(namespaceClientOptions(cfg, source))
	if err != nil {
		return fmt.Errorf("dial namespace client: %w", err)
	}
	defer namespaceClient.Close()

	for _, namespace := range []string{NamespaceName, DeniedNamespaceName} {
		if err := ensureNamespace(ctx, namespaceClient, namespace, cfg.NamespaceRetention); err != nil {
			return err
		}
	}
	return nil
}

func ExpectDeniedNamespaceStart(ctx context.Context, cfg Config, source *workloadapi.X509Source, runID string) error {
	ctx, span := tracer.Start(ctx, "temporal.proof.denied_namespace_check")
	defer span.End()
	span.SetAttributes(attribute.String("forge_metal.proof_run_id", runID))

	proofClient, err := client.Dial(workflowClientOptions(cfg, DeniedNamespaceName, source))
	if err != nil {
		return fmt.Errorf("dial denied namespace client: %w", err)
	}
	defer proofClient.Close()

	workflowID := fmt.Sprintf("denied-%s", runID)
	_, err = proofClient.ExecuteWorkflow(ctx, client.StartWorkflowOptions{
		ID:                       workflowID,
		TaskQueue:                TaskQueueName,
		WorkflowRunTimeout:       cfg.WorkflowRunTimeout,
		WorkflowTaskTimeout:      cfg.WorkflowTaskTimeout,
		WorkflowExecutionTimeout: cfg.WorkflowRunTimeout,
	}, WorkflowName, WorkflowInput{
		RunID:            runID,
		WorkflowID:       workflowID,
		SleepFor:         time.Second,
		GovernanceURL:    cfg.GovernanceURL,
		GovernanceServer: cfg.GovernanceServerID.String(),
		ServiceVersion:   cfg.ServiceVersion,
	})
	if err == nil {
		return errors.New("denied namespace workflow start unexpectedly succeeded")
	}
	var permissionDenied *serviceerror.PermissionDenied
	if !errors.As(err, &permissionDenied) {
		return fmt.Errorf("expected permission denied for denied namespace, got: %w", err)
	}
	span.SetAttributes(attribute.String("temporal.denied_namespace", DeniedNamespaceName))
	return nil
}

func StartWorkflow(ctx context.Context, cfg Config, source *workloadapi.X509Source, runID string, sleepFor time.Duration) (*StartResult, error) {
	ctx, span := tracer.Start(ctx, "temporal.proof.start")
	defer span.End()
	span.SetAttributes(attribute.String("forge_metal.proof_run_id", runID))

	proofClient, err := client.Dial(workflowClientOptions(cfg, cfg.Namespace, source))
	if err != nil {
		return nil, fmt.Errorf("dial proof client: %w", err)
	}
	defer proofClient.Close()

	workflowID := fmt.Sprintf("proof-heartbeat-%s", runID)
	startedAt := time.Now().UTC()
	run, err := proofClient.ExecuteWorkflow(ctx, client.StartWorkflowOptions{
		ID:                                       workflowID,
		TaskQueue:                                TaskQueueName,
		WorkflowRunTimeout:                       cfg.WorkflowRunTimeout,
		WorkflowTaskTimeout:                      cfg.WorkflowTaskTimeout,
		WorkflowExecutionTimeout:                 cfg.WorkflowRunTimeout,
		WorkflowExecutionErrorWhenAlreadyStarted: true,
	}, WorkflowName, WorkflowInput{
		RunID:            runID,
		WorkflowID:       workflowID,
		SleepFor:         sleepFor,
		GovernanceURL:    cfg.GovernanceURL,
		GovernanceServer: cfg.GovernanceServerID.String(),
		ServiceVersion:   cfg.ServiceVersion,
	})
	if err != nil {
		return nil, fmt.Errorf("start proof workflow: %w", err)
	}

	result := &StartResult{
		WorkflowID: workflowID,
		RunID:      run.GetRunID(),
		Namespace:  cfg.Namespace,
		StartedAt:  startedAt,
		SleepFor:   sleepFor.String(),
	}
	span.SetAttributes(
		attribute.String("temporal.namespace", cfg.Namespace),
		attribute.String("temporal.workflow_id", result.WorkflowID),
		attribute.String("temporal.run_id", result.RunID),
	)
	return result, nil
}

func AwaitWorkflow(ctx context.Context, cfg Config, source *workloadapi.X509Source, workflowID string, runID string) (*WorkflowResult, error) {
	ctx, span := tracer.Start(ctx, "temporal.proof.await")
	defer span.End()
	span.SetAttributes(
		attribute.String("temporal.namespace", cfg.Namespace),
		attribute.String("temporal.workflow_id", workflowID),
		attribute.String("temporal.run_id", runID),
	)

	proofClient, err := client.Dial(workflowClientOptions(cfg, cfg.Namespace, source))
	if err != nil {
		return nil, fmt.Errorf("dial proof client: %w", err)
	}
	defer proofClient.Close()

	var result WorkflowResult
	if err := proofClient.GetWorkflow(ctx, workflowID, runID).Get(ctx, &result); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, fmt.Errorf("await proof workflow: %w", err)
	}
	return &result, nil
}

func RunWorker(ctx context.Context, cfg Config, source *workloadapi.X509Source) error {
	proofClient, err := client.Dial(workflowClientOptions(cfg, cfg.Namespace, source))
	if err != nil {
		return fmt.Errorf("dial proof worker client: %w", err)
	}
	defer proofClient.Close()

	workerInstance := worker.New(proofClient, TaskQueueName, worker.Options{})
	workerInstance.RegisterWorkflowWithOptions(ProofHeartbeatWorkflow, workflow.RegisterOptions{Name: WorkflowName})
	activities := &Activities{
		GovernanceURL:      cfg.GovernanceURL,
		GovernanceServerID: cfg.GovernanceServerID,
		Source:             source,
		ServiceVersion:     cfg.ServiceVersion,
	}
	workerInstance.RegisterActivityWithOptions(activities.RecordAuditEvent, activity.RegisterOptions{Name: "RecordAuditEvent"})
	if err := workerInstance.Run(worker.InterruptCh()); err != nil {
		return fmt.Errorf("run proof worker: %w", err)
	}
	return nil
}

func ProofHeartbeatWorkflow(ctx workflow.Context, input WorkflowInput) (*WorkflowResult, error) {
	info := workflow.GetInfo(ctx)
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 15 * time.Second,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    time.Second,
			BackoffCoefficient: 2,
			MaximumInterval:    5 * time.Second,
			MaximumAttempts:    3,
		},
	})

	if err := workflow.Sleep(ctx, input.SleepFor); err != nil {
		return nil, err
	}

	var auditResult AuditActivityResult
	if err := workflow.ExecuteActivity(ctx, "RecordAuditEvent", AuditActivityInput{
		RunID:            input.RunID,
		WorkflowID:       input.WorkflowID,
		GovernanceURL:    input.GovernanceURL,
		GovernanceServer: input.GovernanceServer,
		ServiceVersion:   input.ServiceVersion,
	}).Get(ctx, &auditResult); err != nil {
		return nil, err
	}

	return &WorkflowResult{
		WorkflowID:    info.WorkflowExecution.ID,
		RunID:         info.WorkflowExecution.RunID,
		AuditEventID:  auditResult.EventID,
		AuditSequence: auditResult.Sequence,
	}, nil
}

func (a *Activities) RecordAuditEvent(ctx context.Context, input AuditActivityInput) (*AuditActivityResult, error) {
	ctx, span := tracer.Start(ctx, "temporal.proof.audit_activity")
	defer span.End()
	span.SetAttributes(
		attribute.String("forge_metal.proof_run_id", input.RunID),
		attribute.String("temporal.workflow_id", input.WorkflowID),
	)

	httpClient, err := workloadauth.MTLSClient(a.Source, a.GovernanceServerID, nil)
	if err != nil {
		return nil, fmt.Errorf("build governance mTLS client: %w", err)
	}

	currentSVID, err := a.Source.GetX509SVID()
	if err != nil {
		return nil, fmt.Errorf("load proof x509-svid: %w", err)
	}
	record := auditRecord{
		OrgID:             "forge-metal",
		SourceProductArea: "temporal-proof",
		ServiceName:       "temporal-proof",
		ServiceVersion:    firstNonEmpty(input.ServiceVersion, a.ServiceVersion),
		ActorType:         "workload",
		ActorID:           currentSVID.ID.String(),
		ActorSPIFFEID:     currentSVID.ID.String(),
		OperationID:       input.RunID,
		AuditEvent:        "temporal.proof.heartbeat.completed",
		OperationDisplay:  WorkflowName,
		OperationType:     "workflow",
		EventCategory:     "system",
		RiskLevel:         "low",
		TargetKind:        "temporal-workflow",
		TargetID:          input.WorkflowID,
		Permission:        "temporal.workflow.execute",
		Action:            "complete",
		OrgScope:          "platform",
		Result:            "success",
		Payload: map[string]any{
			"workflow_id": input.WorkflowID,
			"run_id":      input.RunID,
		},
	}

	body, err := json.Marshal(record)
	if err != nil {
		return nil, fmt.Errorf("marshal governance audit payload: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, input.GovernanceURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build governance audit request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")

	response, err := httpClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("post governance audit event: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusAccepted {
		return nil, fmt.Errorf("governance audit status %d", response.StatusCode)
	}

	var parsed auditEventResponse
	if err := json.NewDecoder(response.Body).Decode(&parsed); err != nil {
		return nil, fmt.Errorf("decode governance audit response: %w", err)
	}
	span.SetAttributes(attribute.Int64("forge_metal.audit_sequence", int64(parsed.Sequence)))
	return &AuditActivityResult{
		EventID:  parsed.EventID,
		Sequence: parsed.Sequence,
	}, nil
}

func MarshalJSON(value any) ([]byte, error) {
	return json.MarshalIndent(value, "", "  ")
}

func ensureNamespace(ctx context.Context, namespaceClient client.NamespaceClient, name string, retention time.Duration) error {
	_, err := namespaceClient.Describe(ctx, name)
	if err == nil {
		return nil
	}
	var notFound *serviceerror.NamespaceNotFound
	if !errors.As(err, &notFound) {
		return fmt.Errorf("describe namespace %s: %w", name, err)
	}
	if err := namespaceClient.Register(ctx, &workflowservice.RegisterNamespaceRequest{
		Namespace:                        name,
		WorkflowExecutionRetentionPeriod: durationpb.New(retention),
	}); err != nil {
		var alreadyExists *serviceerror.NamespaceAlreadyExists
		if errors.As(err, &alreadyExists) {
			return nil
		}
		return fmt.Errorf("register namespace %s: %w", name, err)
	}
	return nil
}

func namespaceClientOptions(cfg Config, source *workloadapi.X509Source) client.Options {
	return baseClientOptions(cfg, "", source)
}

func workflowClientOptions(cfg Config, namespace string, source *workloadapi.X509Source) client.Options {
	return baseClientOptions(cfg, namespace, source)
}

func baseClientOptions(cfg Config, namespace string, source *workloadapi.X509Source) client.Options {
	interceptor := mustTracingInterceptor()
	return client.Options{
		HostPort:  cfg.HostPort,
		Namespace: namespace,
		Logger:    temporallog.New(slog.Default()),
		ConnectionOptions: client.ConnectionOptions{
			TLS: temporalTLSConfig(source, cfg.ServerID),
			DialOptions: []grpc.DialOption{
				grpc.WithStatsHandler(otelgrpc.NewClientHandler()),
			},
		},
		Interceptors: []sdkinterceptor.ClientInterceptor{interceptor},
	}
}

func temporalTLSConfig(source *workloadapi.X509Source, serverID spiffeid.ID) *tls.Config {
	return tlsconfig.MTLSClientConfig(source, source, tlsconfig.AuthorizeID(serverID))
}

func mustTracingInterceptor() sdkinterceptor.Interceptor {
	interceptor, err := sdkotel.NewTracingInterceptor(sdkotel.TracerOptions{
		Tracer:            otel.GetTracerProvider().Tracer("temporal-proof-sdk"),
		TextMapPropagator: otelpropagation.NewCompositeTextMapPropagator(otelpropagation.TraceContext{}, otelpropagation.Baggage{}),
	})
	if err != nil {
		panic(err)
	}
	return interceptor
}

func parseSPIFFEIDEnv(name string) (spiffeid.ID, error) {
	raw := strings.TrimSpace(envOr(name, ""))
	if raw == "" {
		return spiffeid.ID{}, fmt.Errorf("%s is required", name)
	}
	id, err := spiffeid.FromString(raw)
	if err != nil {
		return spiffeid.ID{}, fmt.Errorf("parse %s: %w", name, err)
	}
	return id, nil
}

func envOr(name string, fallback string) string {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback
	}
	return raw
}

func envDuration(name string, fallback time.Duration) time.Duration {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(raw)
	if err != nil {
		return fallback
	}
	return parsed
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}
