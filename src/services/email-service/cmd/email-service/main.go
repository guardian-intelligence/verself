package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	iamclient "github.com/verself/iam-service/client"
	verselfotel "github.com/verself/observability/otel"
	secretsclient "github.com/verself/secrets-service/client"
	auth "github.com/verself/service-runtime/auth"
	"github.com/verself/service-runtime/envconfig"
	"github.com/verself/service-runtime/httpserver"
	workloadauth "github.com/verself/service-runtime/workload"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"

	"github.com/verself/email-service/internal/api"
	"github.com/verself/email-service/internal/app"
	"github.com/verself/email-service/internal/mailstore"
	"github.com/verself/email-service/internal/provider"
	"github.com/verself/email-service/internal/sessionproxy"
	emailsync "github.com/verself/email-service/internal/sync"
	"github.com/verself/email-service/migrations"
)

var version = "dev"

type config struct {
	ListenAddr            string
	InternalListenAddr    string
	PGDSN                 string
	StalwartBaseURL       string
	PublicBaseURL         string
	ResendFromName        string
	SyncDiscoveryInterval time.Duration
	SyncReconcileInterval time.Duration
	AuthIssuerURL         string
	AuthAudience          string
	SPIFFEEndpoint        string
	AgentSenderAddress    string
	AgentSenderOrgID      string
	AgentSenderName       string
}

func main() {
	if handled, err := runMigrationCLI(context.Background()); handled {
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func runMigrationCLI(ctx context.Context) (bool, error) {
	if len(os.Args) < 2 || os.Args[1] != "migrate" {
		return false, nil
	}
	return true, migrations.RunCLI(ctx, os.Args[2:], "email-service")
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	otelShutdown, logger, err := verselfotel.Init(ctx, verselfotel.Config{
		ServiceName:    "email-service",
		ServiceVersion: version,
	})
	if err != nil {
		return fmt.Errorf("otel init: %w", err)
	}
	defer func() { _ = otelShutdown(context.Background()) }()
	slog.SetDefault(logger)

	spiffeSource, err := workloadauth.Source(ctx, cfg.SPIFFEEndpoint)
	if err != nil {
		return fmt.Errorf("email-service spiffe source: %w", err)
	}
	defer func() {
		if err := spiffeSource.Close(); err != nil {
			logger.ErrorContext(context.Background(), "email-service spiffe source close", "error", err)
		}
	}()

	secretsHTTPClient, err := workloadauth.MTLSClientForService(spiffeSource, workloadauth.ServiceSecrets, nil)
	if err != nil {
		return fmt.Errorf("email-service secrets mtls: %w", err)
	}
	secretsClient, err := secretsclient.NewClient(workloadauth.InternalURL(workloadauth.ServiceSecrets), secretsclient.WithHTTPClient(secretsHTTPClient))
	if err != nil {
		return fmt.Errorf("email-service secrets client: %w", err)
	}
	iamHTTPClient, err := workloadauth.MTLSClientForService(spiffeSource, workloadauth.ServiceIAM, nil)
	if err != nil {
		return fmt.Errorf("email-service iam mtls: %w", err)
	}
	iamClient, err := iamclient.NewClient(workloadauth.InternalURL(workloadauth.ServiceIAM), iamclient.WithHTTPClient(iamHTTPClient))
	if err != nil {
		return fmt.Errorf("email-service iam client: %w", err)
	}

	transport := otelhttp.NewTransport(http.DefaultTransport, otelhttp.WithSpanNameFormatter(func(_ string, r *http.Request) string {
		return "http " + r.Method + " " + r.URL.Host
	}))
	httpClient := &http.Client{Timeout: 30 * time.Second, Transport: transport}
	streamClient := &http.Client{Transport: transport}

	proxy, err := sessionproxy.New(sessionproxy.Config{
		StalwartBaseURL: cfg.StalwartBaseURL,
		PublicBaseURL:   cfg.PublicBaseURL,
	}, httpClient, logger)
	if err != nil {
		return fmt.Errorf("create session proxy: %w", err)
	}

	runtimeSecrets, err := readRuntimeSecrets(ctx, secretsClient,
		secretsclient.EmailResendAPIKeyName,
		secretsclient.EmailStalwartAdminPasswordName,
	)
	if err != nil {
		return fmt.Errorf("email-service runtime provider secret: %w", err)
	}
	resendAPIKey := requireSecretField(runtimeSecrets, secretsclient.EmailResendAPIKeyName, "email-service resend provider secret")
	adminPassword := requireSecretField(runtimeSecrets, secretsclient.EmailStalwartAdminPasswordName, "email-service stalwart provider secret")

	pgConfig, err := pgxpool.ParseConfig(cfg.PGDSN)
	if err != nil {
		return fmt.Errorf("parse pg-dsn: %w", err)
	}
	pgConfig.MaxConns = 8
	pgPool, err := pgxpool.NewWithConfig(ctx, pgConfig)
	if err != nil {
		return fmt.Errorf("open pg pool: %w", err)
	}
	defer pgPool.Close()

	store := mailstore.New(pgPool)
	if err := seedAgentSender(ctx, store, cfg); err != nil {
		return fmt.Errorf("email-service seed agent sender: %w", err)
	}
	sender, err := provider.NewResend(resendAPIKey, cfg.ResendFromName, &http.Client{Timeout: 10 * time.Second, Transport: transport})
	if err != nil {
		return fmt.Errorf("email-service sender provider: %w", err)
	}
	syncManager := emailsync.New(emailsync.Config{
		StalwartBaseURL:   cfg.StalwartBaseURL,
		AdminPassword:     adminPassword,
		MailboxPasswords:  map[string]string{},
		DiscoveryInterval: cfg.SyncDiscoveryInterval,
		ReconcileInterval: cfg.SyncReconcileInterval,
	}, store, logger, httpClient, streamClient)

	service := app.New(cfg.StalwartBaseURL, cfg.PublicBaseURL, proxy, store, syncManager, sender)

	mux := http.NewServeMux()
	_, protectedAPI := api.NewAPI(mux, version, cfg.ListenAddr, service, iamclient.NewAuthorizer(iamClient))
	authHandler := auth.Middleware(auth.Config{
		IssuerURL: cfg.AuthIssuerURL,
		Audience:  cfg.AuthAudience,
	})(protectedAPI)
	mux.Handle("/api/v1/email/", authHandler)
	service.RegisterRoutes(mux)

	publicServer := httpserver.New(cfg.ListenAddr, otelhttp.NewHandler(mux, "email-service"))
	publicServer.ReadTimeout = 0
	publicServer.WriteTimeout = 0

	internalPeerIDs, err := workloadauth.PeerIDsForSource(
		spiffeSource,
		workloadauth.ServiceAgentMail,
		workloadauth.ServiceBilling,
		workloadauth.ServiceNotifications,
	)
	if err != nil {
		return err
	}
	internalTLSConfig, err := workloadauth.MTLSServerConfigForAny(spiffeSource, internalPeerIDs...)
	if err != nil {
		return fmt.Errorf("email-service internal tls: %w", err)
	}
	internalMux := http.NewServeMux()
	api.RegisterInternalRoutes(internalMux, service)
	internalHandler, err := workloadauth.ServerPeerAllowlistMiddleware(internalPeerIDs, internalMux)
	if err != nil {
		return err
	}
	internalServer := httpserver.New(cfg.InternalListenAddr, otelhttp.NewHandler(internalHandler, "email-service-internal"))
	internalServer.TLSConfig = internalTLSConfig

	service.StartBackground(ctx)
	return httpserver.RunPair(ctx, logger, publicServer, internalServer)
}

func loadConfig() (config, error) {
	l := envconfig.New()
	cfg := config{
		ListenAddr:            l.String("VERSELF_LISTEN_ADDR", "127.0.0.1:4246"),
		InternalListenAddr:    l.String("VERSELF_INTERNAL_LISTEN_ADDR", "127.0.0.1:4247"),
		PGDSN:                 l.RequireString("VERSELF_PG_DSN"),
		StalwartBaseURL:       l.String("EMAIL_SERVICE_STALWART_BASE_URL", "http://127.0.0.1:8090"),
		PublicBaseURL:         l.RequireURL("EMAIL_SERVICE_STALWART_PUBLIC_BASE_URL"),
		ResendFromName:        l.String("EMAIL_SERVICE_RESEND_FROM_NAME", "verself"),
		SyncDiscoveryInterval: l.Duration("EMAIL_SERVICE_SYNC_DISCOVERY_INTERVAL", 2*time.Minute),
		SyncReconcileInterval: l.Duration("EMAIL_SERVICE_SYNC_RECONCILE_INTERVAL", 10*time.Minute),
		AuthIssuerURL:         l.RequireURL("VERSELF_AUTH_ISSUER_URL"),
		AuthAudience:          l.RequireCredential("auth-audience"),
		SPIFFEEndpoint:        l.String(workloadauth.EndpointSocketEnv, ""),
		AgentSenderAddress:    l.String("EMAIL_SERVICE_AGENT_SENDER_ADDRESS", ""),
		AgentSenderOrgID:      l.String("EMAIL_SERVICE_AGENT_SENDER_ORG_ID", ""),
		AgentSenderName:       l.String("EMAIL_SERVICE_AGENT_SENDER_NAME", ""),
	}
	if err := l.Err(); err != nil {
		return config{}, err
	}
	return cfg, nil
}

// agentSenderSubject is the actor subject the agent-mail workload presents; the
// seed grants it send_as so audit rows attribute agent email to a stable subject.
const agentSenderSubject = workloadauth.ServiceAgentMail

// seedAgentSender provisions the configured agent sender address (e.g.
// agents@guardianintelligence.org) under the dogfood org so EnsureOutboundAddress
// accepts it, and records the Resend provider binding for its domain. Opt-in by
// config presence; idempotent so it converges on every boot.
func seedAgentSender(ctx context.Context, store *mailstore.Store, cfg config) error {
	address := strings.TrimSpace(strings.ToLower(cfg.AgentSenderAddress))
	if address == "" {
		return nil
	}
	orgID := strings.TrimSpace(cfg.AgentSenderOrgID)
	if orgID == "" {
		return fmt.Errorf("EMAIL_SERVICE_AGENT_SENDER_ADDRESS set without EMAIL_SERVICE_AGENT_SENDER_ORG_ID")
	}
	at := strings.LastIndexByte(address, '@')
	if at <= 0 || at == len(address)-1 {
		return fmt.Errorf("agent sender address %q is not a valid email", address)
	}
	if err := store.ProvisionAddress(ctx, mailstore.ProvisionedAddress{
		OrgID:           orgID,
		IdentityType:    "system",
		Address:         address,
		DisplayName:     strings.TrimSpace(cfg.AgentSenderName),
		SubjectID:       agentSenderSubject,
		SendAsGrantedBy: "system:agent-sender-seed",
	}); err != nil {
		return err
	}
	return store.UpsertProviderBinding(ctx, mailstore.ProviderBinding{
		Provider:    provider.NameResend,
		Domain:      address[at+1:],
		FromAddress: address,
		Active:      true,
	})
}

func requireSecretField(values map[string]string, field string, label string) string {
	value := strings.TrimSpace(values[field])
	if value == "" {
		fmt.Fprintf(os.Stderr, "%s missing required field %s\n", label, field)
		os.Exit(1)
	}
	return value
}

func readRuntimeSecrets(ctx context.Context, client *secretsclient.Client, secretNames ...string) (map[string]string, error) {
	ctx, span := otel.Tracer("runtime-secrets").Start(ctx, "secrets.runtime.resolve")
	defer span.End()
	span.SetAttributes(attribute.Int("verself.secret_count", len(secretNames)))

	if client == nil {
		err := fmt.Errorf("runtime secrets client is required")
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}
	secretCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	values := make(map[string]string, len(secretNames))
	for _, secretName := range secretNames {
		resp, err := client.ReadSecret(secretCtx, secretsclient.ReadSecretRequest{Name: secretsclient.SecretName(secretName)})
		if err != nil {
			err = fmt.Errorf("read runtime secret %s: %w", secretName, err)
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			return nil, err
		}
		if resp.Result == nil {
			err := fmt.Errorf("read runtime secret %s: unexpected status %d: %s", secretName, resp.StatusCode, strings.TrimSpace(string(resp.Body)))
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			return nil, err
		}
		values[secretName] = string(resp.Result.Value)
	}
	return values, nil
}
