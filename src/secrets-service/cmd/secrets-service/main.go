package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	auth "github.com/forge-metal/auth-middleware"
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

	pgDSN := requireCredential("pg-dsn")
	envelopeKeyText := requireCredential("envelope-key")
	internalInjectionToken := requireCredential("internal-injection-token")
	governanceAuditToken := credentialOr("governance-internal-audit-token", "")

	envelopeKey, err := secrets.DecodeEnvelopeKey(envelopeKeyText)
	if err != nil {
		return err
	}
	codec, err := secrets.NewEnvelopeCodec(envelopeKey)
	if err != nil {
		return err
	}

	listenAddr := envOr("SECRETS_LISTEN_ADDR", "127.0.0.1:4251")
	governanceAuditURL := envOr("SECRETS_GOVERNANCE_AUDIT_URL", "")
	authIssuerURL := requireEnv("SECRETS_AUTH_ISSUER_URL")
	authAudience := requireEnv("SECRETS_AUTH_AUDIENCE")
	authProjectID := requireEnv("SECRETS_AUTH_PROJECT_ID")
	authJWKSURL := envOr("SECRETS_AUTH_JWKS_URL", "")

	pg, err := openPool(ctx, pgDSN, envInt("SECRETS_PG_MAX_CONNS", 8))
	if err != nil {
		return fmt.Errorf("open postgres: %w", err)
	}
	defer pg.Close()

	svc := &secrets.Service{
		PG:             pg,
		Codec:          codec,
		Logger:         logger,
		ServiceVersion: serviceVersion,
		Environment:    envOr("SECRETS_ENVIRONMENT", "single-node"),
	}
	if err := svc.Ready(ctx); err != nil {
		return fmt.Errorf("secrets readiness: %w", err)
	}
	secretsapi.ConfigureAuditSink(governanceAuditURL, governanceAuditToken)

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
	secretsapi.RegisterInternalRoutes(rootMux, svc, internalInjectionToken)

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

	server := &http.Server{
		Addr:              listenAddr,
		Handler:           otelhttp.NewHandler(limitRequestBodies(rootMux, requestBodyLimit), serviceName),
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
		if err := server.Shutdown(shutdownCtx); err != nil {
			logger.ErrorContext(context.Background(), "secrets-service shutdown", "error", err)
		}
	}()

	logger.InfoContext(ctx, "secrets-service listening", "addr", listenAddr)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("secrets-service listen: %w", err)
	}
	return nil
}

func openPool(ctx context.Context, dsn string, maxConns int) (*pgxpool.Pool, error) {
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, err
	}
	config.MaxConns = int32(maxConns)
	config.MinConns = 1
	config.MaxConnLifetime = 30 * time.Minute
	config.MaxConnIdleTime = 5 * time.Minute
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, err
	}
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, err
	}
	return pool, nil
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
		return strings.HasPrefix(r.URL.Path, "/api/")
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

func envInt(name string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		panic("invalid integer env " + name + ": " + err.Error())
	}
	return parsed
}

func requireCredential(name string) string {
	value := credentialOr(name, "")
	if value == "" {
		panic("missing required credential " + name)
	}
	return value
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
