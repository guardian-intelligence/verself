package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
	"github.com/google/uuid"
	_ "github.com/lib/pq"

	auth "github.com/forge-metal/auth-middleware"
	fastsandbox "github.com/forge-metal/fast-sandbox"

	"github.com/forge-metal/sandbox-rental-service/internal/jobs"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))

	// Secrets via systemd LoadCredential=.
	pgDSN := requireCredential("pg-dsn")
	chPassword := credentialOr("ch-password", "")

	// Non-secret config via Environment=.
	listenAddr := envOr("SANDBOX_LISTEN_ADDR", "127.0.0.1:4243")
	chAddress := envOr("SANDBOX_CH_ADDRESS", "127.0.0.1:9000")
	billingURL := envOr("SANDBOX_BILLING_URL", "http://127.0.0.1:4242")
	authIssuerURL := requireEnv("SANDBOX_AUTH_ISSUER_URL")
	authAudience := requireEnv("SANDBOX_AUTH_AUDIENCE")

	// fast-sandbox config
	fsPool := envOr("SANDBOX_FS_POOL", "forgepool")
	fsGoldenZvol := envOr("SANDBOX_FS_GOLDEN_ZVOL", "golden-zvol")
	fsCIDataset := envOr("SANDBOX_FS_CI_DATASET", "ci")
	fsKernelPath := envOr("SANDBOX_FS_KERNEL_PATH", "/var/lib/ci/vmlinux")
	fsFCBin := envOr("SANDBOX_FS_FC_BIN", "/opt/forge-metal/profile/bin/firecracker")
	fsJailerBin := envOr("SANDBOX_FS_JAILER_BIN", "/opt/forge-metal/profile/bin/jailer")
	fsJailerRoot := envOr("SANDBOX_FS_JAILER_ROOT", "/srv/jailer")
	fsJailerUID := envInt("SANDBOX_FS_JAILER_UID", 65534)
	fsJailerGID := envInt("SANDBOX_FS_JAILER_GID", 65534)
	fsVCPUs := envInt("SANDBOX_FS_VCPUS", 2)
	fsMemoryMiB := envInt("SANDBOX_FS_MEMORY_MIB", 2048)
	fsHostInterface := envOr("SANDBOX_FS_HOST_INTERFACE", "eth0")
	fsGuestPoolCIDR := envOr("SANDBOX_FS_GUEST_POOL_CIDR", "10.100.0.0/24")
	fsNetworkLeaseDir := envOr("SANDBOX_FS_NETWORK_LEASE_DIR", "/var/lib/ci/leases")

	// --- open connections ---

	pg, err := sql.Open("postgres", pgDSN)
	if err != nil {
		return fmt.Errorf("open postgres: %w", err)
	}
	defer pg.Close()
	if err := pg.Ping(); err != nil {
		return fmt.Errorf("ping postgres: %w", err)
	}

	chConn, err := clickhouse.Open(&clickhouse.Options{
		Addr: []string{chAddress},
		Auth: clickhouse.Auth{
			Database: "forge_metal",
			Username: "default",
			Password: chPassword,
		},
	})
	if err != nil {
		return fmt.Errorf("open clickhouse: %w", err)
	}
	defer func() { _ = chConn.Close() }()

	// --- fast-sandbox orchestrator ---

	var opsOpts []fastsandbox.Option
	if opsSocket := os.Getenv("SANDBOX_SMELTER_OPS_SOCKET"); opsSocket != "" {
		logger.Info("using smelter ops socket for privileged operations", "socket", opsSocket)
		opsOpts = append(opsOpts, fastsandbox.WithPrivOps(&fastsandbox.SocketPrivOps{
			SocketPath: opsSocket,
		}))
	}

	orchestrator := fastsandbox.New(fastsandbox.Config{
		Pool:            fsPool,
		GoldenZvol:      fsGoldenZvol,
		CIDataset:       fsCIDataset,
		KernelPath:      fsKernelPath,
		FirecrackerBin:  fsFCBin,
		JailerBin:       fsJailerBin,
		JailerRoot:      fsJailerRoot,
		JailerUID:       fsJailerUID,
		JailerGID:       fsJailerGID,
		VCPUs:           fsVCPUs,
		MemoryMiB:       fsMemoryMiB,
		HostInterface:   fsHostInterface,
		GuestPoolCIDR:   fsGuestPoolCIDR,
		NetworkLeaseDir: fsNetworkLeaseDir,
	}, logger, opsOpts...)

	// --- billing client ---

	billingClient := &jobs.BillingClient{
		BaseURL:    billingURL,
		HTTPClient: &http.Client{Timeout: 10 * time.Second},
		Logger:     logger,
	}

	// --- job service ---

	jobService := &jobs.Service{
		PG:           pg,
		CH:           chConn,
		CHDatabase:   "forge_metal",
		Orchestrator: orchestrator,
		Billing:      billingClient,
		Logger:       logger,
	}

	// --- Huma API ---

	mux := http.NewServeMux()
	api := humago.New(mux, huma.DefaultConfig("Sandbox Rental Service", "1.0.0"))

	registerRoutes(api, jobService)

	authHandler := auth.Middleware(auth.Config{
		IssuerURL: authIssuerURL,
		Audience:  authAudience,
	})(mux)

	// All routes require auth (no webhooks or public ops endpoints).
	srv := &http.Server{
		Addr:              listenAddr,
		Handler:           authHandler,
		ReadHeaderTimeout: 10 * time.Second,
	}

	// --- server lifecycle ---

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			log.Printf("sandbox-rental: shutdown: %v", err)
		}
	}()

	log.Printf("sandbox-rental: listening on %s", listenAddr)
	if err := srv.ListenAndServe(); err != http.ErrServerClosed {
		return fmt.Errorf("sandbox-rental: listen: %w", err)
	}

	return nil
}

// --- Huma route registration ---

func registerRoutes(api huma.API, svc *jobs.Service) {
	huma.Register(api, huma.Operation{
		OperationID:   "submit-job",
		Method:        http.MethodPost,
		Path:          "/api/v1/jobs",
		Summary:       "Submit a new sandbox job",
		DefaultStatus: 201,
	}, submitJob(svc))

	huma.Register(api, huma.Operation{
		OperationID: "get-job",
		Method:      http.MethodGet,
		Path:        "/api/v1/jobs/{job_id}",
		Summary:     "Get job status and result",
	}, getJob(svc))

	huma.Register(api, huma.Operation{
		OperationID: "get-job-logs",
		Method:      http.MethodGet,
		Path:        "/api/v1/jobs/{job_id}/logs",
		Summary:     "Get job log output",
	}, getJobLogs(svc))
}

// --- typed inputs/outputs ---

type submitJobInput struct {
	Body struct {
		RepoURL    string `json:"repo_url" required:"true" doc:"GitHub HTTPS URL of the repository"`
		RunCommand string `json:"run_command,omitempty" doc:"Command to execute (default: echo hello)"`
	}
}

type submitJobOutput struct {
	Body struct {
		JobID  string `json:"job_id"`
		Status string `json:"status"`
	}
}

type jobIDPath struct {
	JobID string `path:"job_id" doc:"Job UUID"`
}

type getJobOutput struct {
	Body jobs.JobRecord
}

type getJobLogsOutput struct {
	Body struct {
		JobID string `json:"job_id"`
		Logs  string `json:"logs"`
	}
}

// --- handler factories ---

func submitJob(svc *jobs.Service) func(context.Context, *submitJobInput) (*submitJobOutput, error) {
	return func(ctx context.Context, input *submitJobInput) (*submitJobOutput, error) {
		identity := auth.FromContext(ctx)
		if identity == nil {
			return nil, huma.Error401Unauthorized("missing identity")
		}

		repoURL := strings.TrimSpace(input.Body.RepoURL)
		if repoURL == "" {
			return nil, huma.Error400BadRequest("repo_url is required")
		}
		if !strings.HasPrefix(repoURL, "https://") {
			return nil, huma.Error400BadRequest("repo_url must be an HTTPS URL")
		}

		orgID, err := strconv.ParseUint(identity.OrgID, 10, 64)
		if err != nil {
			return nil, huma.Error400BadRequest("invalid org_id in token: " + identity.OrgID)
		}

		jobID, err := svc.Submit(ctx, orgID, identity.Subject, repoURL, input.Body.RunCommand)
		if err != nil {
			if strings.Contains(err.Error(), "insufficient balance") {
				return nil, huma.Error402PaymentRequired("insufficient balance")
			}
			return nil, huma.Error500InternalServerError("submit job", err)
		}

		out := &submitJobOutput{}
		out.Body.JobID = jobID.String()
		out.Body.Status = "running"
		return out, nil
	}
}

func getJob(svc *jobs.Service) func(context.Context, *jobIDPath) (*getJobOutput, error) {
	return func(ctx context.Context, input *jobIDPath) (*getJobOutput, error) {
		jobID, err := uuid.Parse(input.JobID)
		if err != nil {
			return nil, huma.Error400BadRequest("invalid job_id: " + err.Error())
		}

		job, err := svc.GetJob(ctx, jobID)
		if err != nil {
			return nil, huma.Error404NotFound("job not found")
		}

		out := &getJobOutput{}
		out.Body = *job
		return out, nil
	}
}

func getJobLogs(svc *jobs.Service) func(context.Context, *jobIDPath) (*getJobLogsOutput, error) {
	return func(ctx context.Context, input *jobIDPath) (*getJobLogsOutput, error) {
		jobID, err := uuid.Parse(input.JobID)
		if err != nil {
			return nil, huma.Error400BadRequest("invalid job_id: " + err.Error())
		}

		logs, err := svc.GetJobLogs(ctx, jobID)
		if err != nil {
			return nil, huma.Error500InternalServerError("get job logs", err)
		}

		out := &getJobLogsOutput{}
		out.Body.JobID = jobID.String()
		out.Body.Logs = logs
		return out, nil
	}
}

// --- credential helpers (systemd LoadCredential=) ---

func loadCredential(name string) (string, error) {
	dir := os.Getenv("CREDENTIALS_DIRECTORY")
	if dir == "" {
		return "", fmt.Errorf("CREDENTIALS_DIRECTORY not set")
	}
	data, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		return "", fmt.Errorf("load credential %s: %w", name, err)
	}
	return strings.TrimSpace(string(data)), nil
}

func requireCredential(name string) string {
	v, err := loadCredential(name)
	if err != nil {
		fmt.Fprintf(os.Stderr, "required credential %s: %v\n", name, err)
		os.Exit(1)
	}
	if v == "" {
		fmt.Fprintf(os.Stderr, "required credential %s is empty\n", name)
		os.Exit(1)
	}
	return v
}

func credentialOr(name, fallback string) string {
	v, err := loadCredential(name)
	if err != nil || v == "" {
		return fallback
	}
	return v
}

// --- env helpers ---

func requireEnv(key string) string {
	value := os.Getenv(key)
	if value == "" {
		fmt.Fprintf(os.Stderr, "required env %s is empty\n", key)
		os.Exit(1)
	}
	return value
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envInt(key string, fallback int) int {
	s := os.Getenv(key)
	if s == "" {
		return fallback
	}
	v, err := strconv.Atoi(s)
	if err != nil {
		fmt.Fprintf(os.Stderr, "invalid %s: %v\n", key, err)
		os.Exit(1)
	}
	return v
}
