package main

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	_ "github.com/lib/pq"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	auth "github.com/forge-metal/auth-middleware"
	"github.com/forge-metal/identity-service/internal/api"
	"github.com/forge-metal/identity-service/internal/identity"
	"github.com/forge-metal/identity-service/internal/zitadel"
	fmotel "github.com/forge-metal/otel"
)

const (
	serviceName      = "identity-service"
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
			logger.ErrorContext(context.Background(), "identity-service otel shutdown", "error", err)
		}
	}()

	pgDSN := requireCredential("pg-dsn")
	zitadelAdminToken := requireCredential("zitadel-admin-token")
	zitadelActionSigningKey := requireCredential("zitadel-action-signing-key")
	governanceAuditToken := credentialOr("governance-internal-audit-token", "")

	listenAddr := envOr("IDENTITY_LISTEN_ADDR", "127.0.0.1:4248")
	governanceAuditURL := envOr("IDENTITY_GOVERNANCE_AUDIT_URL", "")
	authIssuerURL := requireEnv("IDENTITY_AUTH_ISSUER_URL")
	authAudience := requireEnv("IDENTITY_AUTH_AUDIENCE")
	authJWKSURL := envOr("IDENTITY_AUTH_JWKS_URL", "")
	projectID := requireEnv("IDENTITY_AUTH_PROJECT_ID")
	zitadelBaseURL := requireEnv("IDENTITY_ZITADEL_BASE_URL")
	zitadelHostHeader := requireEnv("IDENTITY_ZITADEL_HOST")

	pg, err := sql.Open("postgres", pgDSN)
	if err != nil {
		return fmt.Errorf("open postgres: %w", err)
	}
	defer func() {
		if err := pg.Close(); err != nil {
			logger.ErrorContext(context.Background(), "identity-service postgres close", "error", err)
		}
	}()
	if err := pg.Ping(); err != nil {
		return fmt.Errorf("ping postgres: %w", err)
	}

	zitadelClient, err := zitadel.New(zitadel.Config{
		BaseURL:    zitadelBaseURL,
		HostHeader: zitadelHostHeader,
		AdminToken: zitadelAdminToken,
		HTTPClient: &http.Client{
			Transport: otelhttp.NewTransport(http.DefaultTransport),
			Timeout:   5 * time.Second,
		},
	})
	if err != nil {
		return err
	}
	identityService := &identity.Service{
		Store:     identity.SQLStore{DB: pg},
		Directory: zitadelClient,
		ProjectID: projectID,
	}
	api.ConfigureAuditSink(governanceAuditURL, governanceAuditToken)

	rootMux := http.NewServeMux()
	rootMux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	rootMux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		if err := pg.PingContext(r.Context()); err != nil {
			http.Error(w, "not ready", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	api.RegisterZitadelActionRoutes(rootMux, identityService, zitadelActionSigningKey)

	privateMux := http.NewServeMux()
	api.NewAPI(privateMux, api.Config{Version: serviceVersion, ListenAddr: listenAddr, Service: identityService})
	protected := auth.Middleware(auth.Config{
		IssuerURL: authIssuerURL,
		Audience:  authAudience,
		ProjectID: projectID,
		JWKSURL:   authJWKSURL,
	})(privateMux)
	rootMux.Handle("/", protected)

	rootHandler := limitRequestBodies(rootMux, requestBodyLimit)
	srv := &http.Server{
		Addr:              listenAddr,
		Handler:           otelhttp.NewHandler(rootHandler, serviceName),
		ReadHeaderTimeout: 2 * time.Second,
		ReadTimeout:       5 * time.Second,
		WriteTimeout:      5 * time.Second,
		IdleTimeout:       30 * time.Second,
		MaxHeaderBytes:    maxHeaderBytes,
	}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			logger.ErrorContext(context.Background(), "identity-service shutdown", "error", err)
		}
	}()

	logger.Info("identity-service: listening", "addr", listenAddr)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("identity-service listen: %w", err)
	}
	return nil
}

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
	value, err := loadCredential(name)
	if err != nil {
		fmt.Fprintf(os.Stderr, "required credential %s: %v\n", name, err)
		os.Exit(1)
	}
	if value == "" {
		fmt.Fprintf(os.Stderr, "required credential %s is empty\n", name)
		os.Exit(1)
	}
	return value
}

func credentialOr(name, fallback string) string {
	value, err := loadCredential(name)
	if err != nil || value == "" {
		return fallback
	}
	return value
}

func requireEnv(key string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		fmt.Fprintf(os.Stderr, "required env %s is empty\n", key)
		os.Exit(1)
	}
	return value
}

func envOr(key, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
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
	case http.MethodPost, http.MethodPut, http.MethodPatch:
		return strings.HasPrefix(r.URL.Path, "/api/")
	default:
		return false
	}
}
