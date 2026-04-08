package main

import (
	"context"
	"database/sql"
	"fmt"
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
	_ "github.com/lib/pq"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	auth "github.com/forge-metal/auth-middleware"
	billingclient "github.com/forge-metal/billing-service/client"
	fmotel "github.com/forge-metal/otel"
	vmorchestrator "github.com/forge-metal/vm-orchestrator"

	sandboxapi "github.com/forge-metal/sandbox-rental-service/internal/api"
	"github.com/forge-metal/sandbox-rental-service/internal/jobs"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	otelShutdown, logger, err := fmotel.Init(ctx, fmotel.Config{ServiceName: "sandbox-rental-service", ServiceVersion: "1.0.0"})
	if err != nil {
		return fmt.Errorf("otel init: %w", err)
	}
	defer otelShutdown(context.Background())
	slog.SetDefault(logger)

	// Secrets via systemd LoadCredential=.
	pgDSN := requireCredential("pg-dsn")
	chPassword := credentialOr("ch-password", "")

	// Non-secret config via Environment=.
	listenAddr := envOr("SANDBOX_LISTEN_ADDR", "127.0.0.1:4243")
	chAddress := envOr("SANDBOX_CH_ADDRESS", "127.0.0.1:9000")
	billingURL := envOr("SANDBOX_BILLING_URL", "http://127.0.0.1:4242")
	authIssuerURL := requireEnv("SANDBOX_AUTH_ISSUER_URL")
	authAudience := requireEnv("SANDBOX_AUTH_AUDIENCE")
	authJWKSURL := envOr("SANDBOX_AUTH_JWKS_URL", "")

	// vm-orchestrator config
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

	// --- vm-orchestrator library ---

	orchestrator := vmorchestrator.New(vmorchestrator.Config{
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
	}, logger)

	// --- billing client ---

	billingClient, err := billingclient.New(billingURL, billingclient.WithHTTPClient(&http.Client{
		Transport: otelhttp.NewTransport(http.DefaultTransport),
		Timeout:   10 * time.Second,
	}))
	if err != nil {
		return fmt.Errorf("create billing client: %w", err)
	}

	// --- job service ---

	jobService := &jobs.Service{
		PG:            pg,
		CH:            chConn,
		CHDatabase:    "forge_metal",
		Orchestrator:  orchestrator,
		Billing:       billingClient,
		BillingVCPUs:  fsVCPUs,
		BillingMemMiB: fsMemoryMiB,
		Logger:        logger,
	}

	// --- Huma API ---

	mux := http.NewServeMux()
	humaAPI := humago.New(mux, huma.DefaultConfig("Sandbox Rental Service", "1.0.0"))

	sandboxapi.RegisterRoutes(humaAPI, jobService, billingClient)

	authHandler := auth.Middleware(auth.Config{
		IssuerURL: authIssuerURL,
		Audience:  authAudience,
		JWKSURL:   authJWKSURL,
	})(mux)

	// All routes require auth (no webhooks or public ops endpoints).
	srv := &http.Server{
		Addr:              listenAddr,
		Handler:           otelhttp.NewHandler(authHandler, "sandbox-rental-service"),
		ReadHeaderTimeout: 10 * time.Second,
	}

	// --- server lifecycle ---

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			logger.ErrorContext(context.Background(), "sandbox-rental: shutdown", "error", err)
		}
	}()

	logger.Info("sandbox-rental: listening", "addr", listenAddr)
	if err := srv.ListenAndServe(); err != http.ErrServerClosed {
		return fmt.Errorf("sandbox-rental: listen: %w", err)
	}

	return nil
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
