package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	auth "github.com/forge-metal/auth-middleware"
	workloadauth "github.com/forge-metal/auth-middleware/workload"
	billingclient "github.com/forge-metal/billing-service/client"
	fmotel "github.com/forge-metal/otel"
	secretsapi "github.com/forge-metal/secrets-service/internal/api"
	"github.com/forge-metal/secrets-service/internal/secrets"
)

const (
	serviceName      = "secrets-service"
	serviceVersion   = "1.0.0"
	maxHeaderBytes   = 16 << 10
	requestBodyLimit = 1 << 20
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

	otelShutdown, logger, err := fmotel.Init(ctx, fmotel.Config{ServiceName: serviceName, ServiceVersion: serviceVersion})
	if err != nil {
		return fmt.Errorf("otel init: %w", err)
	}
	defer func() {
		if err := otelShutdown(context.Background()); err != nil {
			logger.ErrorContext(context.Background(), "secrets-service otel shutdown", "error", err)
		}
	}()

	listenAddr := envOr("SECRETS_LISTEN_ADDR", "127.0.0.1:4251")
	internalListenAddr := envOr("SECRETS_INTERNAL_LISTEN_ADDR", "127.0.0.1:4253")
	governanceAuditURL := envOr("SECRETS_GOVERNANCE_AUDIT_URL", "")
	authIssuerURL := requireEnv("SECRETS_AUTH_ISSUER_URL")
	authAudience := requireEnv("SECRETS_AUTH_AUDIENCE")
	authProjectID := requireEnv("SECRETS_AUTH_PROJECT_ID")
	authJWKSURL := envOr("SECRETS_AUTH_JWKS_URL", "")
	openBaoAddr := requireEnv("SECRETS_OPENBAO_ADDR")
	openBaoCACert := credentialPath("openbao-ca-cert")
	billingURL := requireEnv("SECRETS_BILLING_URL")
	secretsSPIFFEID, err := workloadauth.ParseID(requireEnv("SECRETS_SPIFFE_ID"))
	if err != nil {
		return err
	}
	sandboxSPIFFEID, err := workloadauth.ParseID(requireEnv("SECRETS_SANDBOX_SPIFFE_ID"))
	if err != nil {
		return err
	}
	billingSPIFFEID, err := workloadauth.ParseID(requireEnv("SECRETS_BILLING_SPIFFE_ID"))
	if err != nil {
		return err
	}
	governanceSPIFFEID, err := workloadauth.ParseID(requireEnv("SECRETS_GOVERNANCE_SPIFFE_ID"))
	if err != nil {
		return err
	}
	spiffeSource, err := workloadauth.Source(ctx, envOr(workloadauth.EndpointSocketEnv, ""))
	if err != nil {
		return fmt.Errorf("spiffe workload source: %w", err)
	}
	defer func() {
		if err := spiffeSource.Close(); err != nil {
			logger.ErrorContext(context.Background(), "secrets-service spiffe source close", "error", err)
		}
	}()
	workloadJWTSource, err := workloadauth.JWTSource(ctx, envOr(workloadauth.EndpointSocketEnv, ""))
	if err != nil {
		return fmt.Errorf("spiffe jwt source: %w", err)
	}
	defer func() {
		if err := workloadJWTSource.Close(); err != nil {
			logger.ErrorContext(context.Background(), "secrets-service spiffe jwt source close", "error", err)
		}
	}()

	store, err := secrets.NewBaoStore(ctx, secrets.BaoStoreConfig{
		Address:       openBaoAddr,
		CACertPath:    openBaoCACert,
		KVMountPrefix: envOr("SECRETS_OPENBAO_KV_PREFIX", "kv"),
		TransitPrefix: envOr("SECRETS_OPENBAO_TRANSIT_PREFIX", "transit"),
		JWTAuthPrefix: envOr("SECRETS_OPENBAO_JWT_PREFIX", "jwt"),
		WorkloadJWT: secrets.WorkloadJWTConfig{
			Source:     workloadJWTSource,
			Audience:   envOr("SECRETS_OPENBAO_WORKLOAD_AUDIENCE", "openbao"),
			Subject:    secretsSPIFFEID,
			AuthPrefix: envOr("SECRETS_OPENBAO_SPIFFE_JWT_PREFIX", "spiffe-jwt"),
		},
	}, logger)
	if err != nil {
		return fmt.Errorf("openbao store: %w", err)
	}
	billingHTTPClient, err := workloadauth.MTLSClient(spiffeSource, billingSPIFFEID, http.DefaultTransport)
	if err != nil {
		return fmt.Errorf("billing spiffe client: %w", err)
	}
	billingClient, err := billingclient.New(
		billingURL,
		billingclient.WithHTTPClient(billingHTTPClient),
	)
	if err != nil {
		return fmt.Errorf("billing client: %w", err)
	}

	svc := &secrets.Service{
		Store:          store,
		Billing:        billingClient,
		Logger:         logger,
		ServiceVersion: serviceVersion,
		Environment:    envOr("SECRETS_ENVIRONMENT", "single-node"),
	}
	if err := svc.Ready(ctx); err != nil {
		return fmt.Errorf("secrets readiness: %w", err)
	}
	governanceAuditClient, err := workloadauth.MTLSClient(spiffeSource, governanceSPIFFEID, http.DefaultTransport)
	if err != nil {
		return fmt.Errorf("governance spiffe client: %w", err)
	}
	secretsapi.ConfigureAuditSink(governanceAuditURL, governanceAuditClient)
	internalTLSConfig, err := workloadauth.MTLSServerConfig(spiffeSource, sandboxSPIFFEID)
	if err != nil {
		return fmt.Errorf("spiffe internal tls: %w", err)
	}

	rootMux := http.NewServeMux()
	rootMux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})
	rootMux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		readyCtx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := svc.Ready(readyCtx); err != nil {
			http.Error(w, "not ready", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ready\n"))
	})
	privateMux := http.NewServeMux()
	secretsapi.NewAPI(privateMux, serviceVersion, "http://"+listenAddr, svc)
	authenticated := auth.Middleware(auth.Config{
		IssuerURL: authIssuerURL,
		Audience:  authAudience,
		ProjectID: authProjectID,
		JWKSURL:   authJWKSURL,
	})(privateMux)
	protected := secretsapi.CaptureRawBearerToken(authenticated)
	rootMux.Handle("/", protected)
	internalMux := http.NewServeMux()
	secretsapi.RegisterInternalRoutes(internalMux, svc)

	server := &http.Server{
		Addr:              listenAddr,
		Handler:           otelhttp.NewHandler(limitRequestBodies(rootMux, requestBodyLimit), serviceName),
		ReadHeaderTimeout: 2 * time.Second,
		ReadTimeout:       5 * time.Second,
		WriteTimeout:      5 * time.Second,
		IdleTimeout:       30 * time.Second,
		MaxHeaderBytes:    maxHeaderBytes,
	}
	internalServer := &http.Server{
		Addr:              internalListenAddr,
		Handler:           otelhttp.NewHandler(limitRequestBodies(workloadauth.ServerPeerMiddleware(sandboxSPIFFEID, internalMux), requestBodyLimit), serviceName+"-internal"),
		TLSConfig:         internalTLSConfig,
		ReadHeaderTimeout: 2 * time.Second,
		ReadTimeout:       5 * time.Second,
		WriteTimeout:      5 * time.Second,
		IdleTimeout:       30 * time.Second,
		MaxHeaderBytes:    maxHeaderBytes,
	}
	runCtx, cancelRun := context.WithCancel(ctx)
	defer cancelRun()
	go func() {
		<-runCtx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			logger.ErrorContext(context.Background(), "secrets-service shutdown", "error", err)
		}
		if err := internalServer.Shutdown(shutdownCtx); err != nil {
			logger.ErrorContext(context.Background(), "secrets-service internal shutdown", "error", err)
		}
	}()

	logger.InfoContext(ctx, "secrets-service listening", "addr", listenAddr)
	logger.InfoContext(ctx, "secrets-service internal listening", "addr", internalListenAddr)
	errCh := make(chan error, 2)
	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- fmt.Errorf("secrets-service listen: %w", err)
			return
		}
		errCh <- nil
	}()
	go func() {
		if err := internalServer.ListenAndServeTLS("", ""); err != nil && err != http.ErrServerClosed {
			errCh <- fmt.Errorf("secrets-service internal listen: %w", err)
			return
		}
		errCh <- nil
	}()
	var firstErr error
	for range 2 {
		if err := <-errCh; err != nil && firstErr == nil {
			firstErr = err
			cancelRun()
		}
	}
	if firstErr != nil {
		return firstErr
	}
	return nil
}

func limitRequestBodies(next http.Handler, maxBytes int64) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requestMayHaveBody(r) {
			if r.ContentLength > maxBytes {
				http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
				return
			}
			r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
		}
		next.ServeHTTP(w, r)
	})
}

func requestMayHaveBody(r *http.Request) bool {
	switch r.Method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/internal/")
	default:
		return false
	}
}

func requireEnv(name string) string {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		panic("missing required env " + name)
	}
	return value
}

func envOr(name, fallback string) string {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	return value
}

func requireCredential(name string) string {
	value := credentialOr(name, "")
	if value == "" {
		panic("missing required credential " + name)
	}
	return value
}

func credentialPath(name string) string {
	base := os.Getenv("CREDENTIALS_DIRECTORY")
	if base == "" {
		panic("missing CREDENTIALS_DIRECTORY for credential " + name)
	}
	path := filepath.Join(base, name)
	if _, err := os.Stat(path); err != nil {
		panic("missing required credential " + name)
	}
	return path
}

func credentialOr(name, fallback string) string {
	base := os.Getenv("CREDENTIALS_DIRECTORY")
	if base == "" {
		return fallback
	}
	data, err := os.ReadFile(filepath.Join(base, name))
	if err != nil {
		return fallback
	}
	return strings.TrimSpace(string(data))
}
