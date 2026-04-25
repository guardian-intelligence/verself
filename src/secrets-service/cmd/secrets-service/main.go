package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	auth "github.com/forge-metal/auth-middleware"
	workloadauth "github.com/forge-metal/auth-middleware/workload"
	billingclient "github.com/forge-metal/billing-service/client"
	"github.com/forge-metal/envconfig"
	"github.com/forge-metal/httpserver"
	fmotel "github.com/forge-metal/otel"
	secretsclient "github.com/forge-metal/secrets-service/client"
	secretsapi "github.com/forge-metal/secrets-service/internal/api"
	"github.com/forge-metal/secrets-service/internal/secrets"
)

const (
	serviceName      = "secrets-service"
	serviceVersion   = "1.0.0"
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

	cfg := envconfig.New()
	listenAddr := cfg.String("SECRETS_LISTEN_ADDR", "127.0.0.1:4251")
	internalListenAddr := cfg.String("SECRETS_INTERNAL_LISTEN_ADDR", "127.0.0.1:4253")
	governanceAuditURL := cfg.String("SECRETS_GOVERNANCE_AUDIT_URL", "")
	authIssuerURL := cfg.RequireURL("SECRETS_AUTH_ISSUER_URL")
	authAudience := cfg.RequireString("SECRETS_AUTH_AUDIENCE")
	openBaoAddr := cfg.RequireString("SECRETS_OPENBAO_ADDR")
	openBaoCACert := cfg.RequireCredentialPath("openbao-ca-cert")
	billingURL := cfg.RequireURL("SECRETS_BILLING_URL")
	platformOrgID := cfg.RequireString("SECRETS_PLATFORM_ORG_ID")
	spiffeEndpoint := cfg.String(workloadauth.EndpointSocketEnv, "")
	environment := cfg.String("SECRETS_ENVIRONMENT", "single-node")
	kvPrefix := cfg.String("SECRETS_OPENBAO_KV_PREFIX", "kv")
	transitPrefix := cfg.String("SECRETS_OPENBAO_TRANSIT_PREFIX", "transit")
	jwtPrefix := cfg.String("SECRETS_OPENBAO_JWT_PREFIX", "jwt")
	workloadAudience := cfg.String("SECRETS_OPENBAO_WORKLOAD_AUDIENCE", "openbao")
	spiffeJWTPrefix := cfg.String("SECRETS_OPENBAO_SPIFFE_JWT_PREFIX", "spiffe-jwt")
	if err := cfg.Err(); err != nil {
		return err
	}

	spiffeSource, err := workloadauth.Source(ctx, spiffeEndpoint)
	if err != nil {
		return fmt.Errorf("spiffe workload source: %w", err)
	}
	defer func() {
		if err := spiffeSource.Close(); err != nil {
			logger.ErrorContext(context.Background(), "secrets-service spiffe source close", "error", err)
		}
	}()
	workloadJWTSource, err := workloadauth.JWTSource(ctx, spiffeEndpoint)
	if err != nil {
		return fmt.Errorf("spiffe jwt source: %w", err)
	}
	defer func() {
		if err := workloadJWTSource.Close(); err != nil {
			logger.ErrorContext(context.Background(), "secrets-service spiffe jwt source close", "error", err)
		}
	}()
	secretsSPIFFEID, err := workloadauth.CurrentIDForService(spiffeSource, workloadauth.ServiceSecrets)
	if err != nil {
		return err
	}

	store, err := secrets.NewBaoStore(ctx, secrets.BaoStoreConfig{
		Address:       openBaoAddr,
		CACertPath:    openBaoCACert,
		KVMountPrefix: kvPrefix,
		TransitPrefix: transitPrefix,
		JWTAuthPrefix: jwtPrefix,
		WorkloadJWT: secrets.WorkloadJWTConfig{
			Source:     workloadJWTSource,
			Audience:   workloadAudience,
			Subject:    secretsSPIFFEID,
			AuthPrefix: spiffeJWTPrefix,
		},
	}, logger)
	if err != nil {
		return fmt.Errorf("openbao store: %w", err)
	}
	billingHTTPClient, err := workloadauth.MTLSClientForService(spiffeSource, workloadauth.ServiceBilling, nil)
	if err != nil {
		return fmt.Errorf("secrets billing mtls: %w", err)
	}
	billingClient, err := billingclient.NewClientWithResponses(billingURL, billingclient.WithHTTPClient(billingHTTPClient))
	if err != nil {
		return fmt.Errorf("billing client: %w", err)
	}

	svc := &secrets.Service{
		Store:          store,
		Billing:        billingClient,
		Logger:         logger,
		ServiceVersion: serviceVersion,
		Environment:    environment,
	}
	if err := svc.Ready(ctx); err != nil {
		return fmt.Errorf("secrets readiness: %w", err)
	}
	secretsapi.ConfigureAuditSink(governanceAuditURL, spiffeSource)

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
	})(privateMux)
	protected := secretsapi.CaptureRawBearerToken(authenticated)
	rootMux.Handle("/", protected)
	internalMux := http.NewServeMux()
	internalPeerIDs, err := secretsapi.RegisterInternalRoutes(internalMux, svc, spiffeSource, secretsapi.InternalRoutesConfig{
		PlatformOrgID:  platformOrgID,
		SandboxService: workloadauth.ServiceSandboxRental,
		SourceService:  workloadauth.ServiceSourceCodeHosting,
		RuntimeSecretReadPolicies: []secretsapi.RuntimeSecretPolicy{
			{Service: workloadauth.ServiceBilling, SecretNames: []string{
				secretsclient.BillingStripeSecretKeyName,
				secretsclient.BillingStripeWebhookSecretName,
			}},
			{Service: workloadauth.ServiceSandboxRental, SecretNames: []string{
				secretsclient.SandboxGitHubPrivateKeyName,
				secretsclient.SandboxGitHubWebhookSecretName,
				secretsclient.SandboxGitHubClientSecretName,
			}},
			{Service: workloadauth.ServiceMailbox, SecretNames: []string{
				secretsclient.MailboxResendAPIKeyName,
				secretsclient.MailboxStalwartAdminPasswordName,
			}},
			{Service: workloadauth.ServiceObjectStorage, SecretNames: []string{
				secretsclient.ObjectStorageGarageProxyAccessKeyIDName,
				secretsclient.ObjectStorageGarageProxySecretAccessKeyName,
			}},
			{Service: workloadauth.ServiceObjectStorageAdmin, SecretNames: []string{
				secretsclient.ObjectStorageGarageProxyAccessKeyIDName,
				secretsclient.ObjectStorageGarageProxySecretAccessKeyName,
			}},
		},
		RuntimeSecretWritePolicies: []secretsapi.RuntimeSecretPolicy{
			{Service: workloadauth.ServiceObjectStorageAdmin, SecretNames: []string{
				secretsclient.ObjectStorageGarageProxyAccessKeyIDName,
				secretsclient.ObjectStorageGarageProxySecretAccessKeyName,
			}},
		},
	})
	if err != nil {
		return fmt.Errorf("register secrets internal routes: %w", err)
	}
	internalTLSConfig, err := workloadauth.MTLSServerConfigForAny(spiffeSource, internalPeerIDs...)
	if err != nil {
		return fmt.Errorf("spiffe internal tls: %w", err)
	}
	internalAllowlist, err := workloadauth.ServerPeerAllowlistMiddleware(internalPeerIDs, internalMux)
	if err != nil {
		return fmt.Errorf("secrets internal allowlist: %w", err)
	}

	public := httpserver.New(listenAddr, otelhttp.NewHandler(limitRequestBodies(rootMux, requestBodyLimit), serviceName))
	internal := httpserver.New(internalListenAddr, otelhttp.NewHandler(limitRequestBodies(internalAllowlist, requestBodyLimit), serviceName+"-internal"))
	internal.TLSConfig = internalTLSConfig

	return httpserver.RunPair(ctx, logger, public, internal)
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
