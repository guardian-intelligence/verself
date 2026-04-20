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
	billingclient "github.com/forge-metal/billing-service/client"
	fmotel "github.com/forge-metal/otel"
	secretsapi "github.com/forge-metal/secrets-service/internal/api"
	"github.com/forge-metal/secrets-service/internal/secrets"
	"github.com/forge-metal/secrets-service/internal/serviceauth"
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

	internalInjectionToken := requireCredential("internal-injection-token")
	governanceAuditToken := credentialOr("governance-internal-audit-token", "")
	serviceAccountSecret := requireCredential("service-account-client-secret")
	billingClientSecret := requireCredential("billing-client-secret")

	listenAddr := envOr("SECRETS_LISTEN_ADDR", "127.0.0.1:4251")
	governanceAuditURL := envOr("SECRETS_GOVERNANCE_AUDIT_URL", "")
	authIssuerURL := requireEnv("SECRETS_AUTH_ISSUER_URL")
	authAudience := requireEnv("SECRETS_AUTH_AUDIENCE")
	authProjectID := requireEnv("SECRETS_AUTH_PROJECT_ID")
	authJWKSURL := envOr("SECRETS_AUTH_JWKS_URL", "")
	openBaoAddr := requireEnv("SECRETS_OPENBAO_ADDR")
	openBaoCACert := credentialPath("openbao-ca-cert")
	serviceAccountClientID := requireEnv("SECRETS_SERVICE_ACCOUNT_CLIENT_ID")
	serviceAccountTokenURL := requireEnv("SECRETS_SERVICE_ACCOUNT_TOKEN_URL")
	serviceAccountTokenHost := envOr("SECRETS_SERVICE_ACCOUNT_TOKEN_HOST", "")
	billingURL := requireEnv("SECRETS_BILLING_URL")
	billingClientID := requireEnv("SECRETS_BILLING_CLIENT_ID")
	billingTokenURL := requireEnv("SECRETS_BILLING_TOKEN_URL")
	billingAudience := requireEnv("SECRETS_BILLING_AUTH_AUDIENCE")

	store, err := secrets.NewBaoStore(ctx, secrets.BaoStoreConfig{
		Address:       openBaoAddr,
		CACertPath:    openBaoCACert,
		KVMountPrefix: envOr("SECRETS_OPENBAO_KV_PREFIX", "kv"),
		TransitPrefix: envOr("SECRETS_OPENBAO_TRANSIT_PREFIX", "transit"),
		JWTAuthPrefix: envOr("SECRETS_OPENBAO_JWT_PREFIX", "jwt"),
		ServiceAccount: secrets.ServiceAccountConfig{
			ClientID:  serviceAccountClientID,
			Secret:    serviceAccountSecret,
			TokenURL:  serviceAccountTokenURL,
			TokenHost: serviceAccountTokenHost,
			ProjectID: authProjectID,
		},
	}, logger)
	if err != nil {
		return fmt.Errorf("openbao store: %w", err)
	}
	billingAuthEditor, err := serviceauth.NewBearerTokenRequestEditor(serviceauth.ClientCredentialsConfig{
		IssuerURL:    authIssuerURL,
		TokenURL:     billingTokenURL,
		ClientID:     billingClientID,
		ClientSecret: billingClientSecret,
		Audience:     billingAudience,
		Transport:    otelhttp.NewTransport(http.DefaultTransport),
		Timeout:      5 * time.Second,
	})
	if err != nil {
		return fmt.Errorf("billing auth: %w", err)
	}
	billingClient, err := billingclient.New(
		billingURL,
		billingclient.WithHTTPClient(&http.Client{Transport: otelhttp.NewTransport(http.DefaultTransport), Timeout: 5 * time.Second}),
		billingclient.WithRequestEditorFn(billingAuthEditor),
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
