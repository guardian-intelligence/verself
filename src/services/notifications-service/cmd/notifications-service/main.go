package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	iamclient "github.com/verself/iam-service/client"
	notificationsapi "github.com/verself/notifications-service/internal/api"
	"github.com/verself/notifications-service/internal/notifications"
	"github.com/verself/notifications-service/migrations"
	verselfotel "github.com/verself/observability/otel"
	secretsclient "github.com/verself/secrets-service/client"
	auth "github.com/verself/service-runtime/auth"
	"github.com/verself/service-runtime/envconfig"
	"github.com/verself/service-runtime/httpserver"
	workloadauth "github.com/verself/service-runtime/workload"
)

const (
	serviceName               = notifications.ServiceName
	serviceVersion            = "1.0.0"
	requestBodyLimit          = 1 << 20
	platformAlertPollInterval = 15 * time.Second
	platformAlertPollTimeout  = 5 * time.Second
	platformAlertLookback     = 2 * time.Minute
)

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
	return true, migrations.RunCLI(ctx, os.Args[2:], serviceName)
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	otelShutdown, logger, err := verselfotel.Init(ctx, verselfotel.Config{ServiceName: serviceName, ServiceVersion: serviceVersion})
	if err != nil {
		return fmt.Errorf("otel init: %w", err)
	}
	defer func() {
		if err := otelShutdown(context.Background()); err != nil {
			logger.ErrorContext(context.Background(), "notifications-service otel shutdown", "error", err)
		}
	}()
	slog.SetDefault(logger)

	cfg := envconfig.New()
	pgDSN := cfg.RequireString("VERSELF_PG_DSN")
	listenAddr := cfg.String("VERSELF_LISTEN_ADDR", "127.0.0.1:4260")
	internalListenAddr := cfg.String("VERSELF_INTERNAL_LISTEN_ADDR", "127.0.0.1:4261")
	authIssuerURL := cfg.RequireURL("VERSELF_AUTH_ISSUER_URL")
	authAudience := cfg.RequireCredential("auth-audience")
	installationID := cfg.RequireString("VERSELF_INSTALLATION_ID")
	natsURL := cfg.String("NOTIFICATIONS_NATS_URL", notifications.NATSDefaultURL)
	chAddress := cfg.String("VERSELF_CLICKHOUSE_ADDRESS", "127.0.0.1:9440")
	chUser := cfg.String("VERSELF_CLICKHOUSE_USER", "notifications_service")
	chCACertPath := cfg.RequireCredentialPath("clickhouse-ca-cert")
	resendFromAddress := cfg.String("NOTIFICATIONS_RESEND_FROM_ADDRESS", "noreply@notify.verself.sh")
	resendFromName := cfg.String("NOTIFICATIONS_RESEND_FROM_NAME", "verself")
	platformAlertOrgID := cfg.RequireString("NOTIFICATIONS_PLATFORM_ALERT_ORG_ID")
	platformAlertEmail := cfg.RequireString("NOTIFICATIONS_PLATFORM_ALERT_EMAIL")
	pgMaxConns := cfg.Int("VERSELF_PG_MAX_CONNS", 8)
	pgMinConns := cfg.Int("VERSELF_PG_MIN_CONNS", 1)
	pgMaxLifetime := cfg.Int("VERSELF_PG_CONN_MAX_LIFETIME_SECONDS", 1800)
	pgMaxIdle := cfg.Int("VERSELF_PG_CONN_MAX_IDLE_SECONDS", 300)
	spiffeEndpoint := cfg.String(workloadauth.EndpointSocketEnv, "")
	if err := cfg.Err(); err != nil {
		return err
	}

	spiffeSource, err := workloadauth.Source(ctx, spiffeEndpoint)
	if err != nil {
		return fmt.Errorf("notifications spiffe workload source: %w", err)
	}
	defer func() {
		if err := spiffeSource.Close(); err != nil {
			logger.ErrorContext(context.Background(), "notifications-service spiffe source close", "error", err)
		}
	}()
	if _, err := workloadauth.CurrentIDForService(spiffeSource, workloadauth.ServiceNotifications); err != nil {
		return err
	}

	pg, err := openPool(ctx, pgDSN, pgMaxConns, pgMinConns, pgMaxLifetime, pgMaxIdle)
	if err != nil {
		return fmt.Errorf("open notifications postgres: %w", err)
	}
	defer pg.Close()

	chTLSConfig, err := workloadauth.TLSConfigWithX509SourceAndCABundle(ctx, spiffeSource, chCACertPath)
	if err != nil {
		return fmt.Errorf("notifications clickhouse tls: %w", err)
	}
	chConn, err := clickhouse.Open(&clickhouse.Options{
		Addr: []string{chAddress},
		Auth: clickhouse.Auth{
			Database: "verself",
			Username: chUser,
		},
		TLS: chTLSConfig,
	})
	if err != nil {
		return fmt.Errorf("open clickhouse: %w", err)
	}
	defer func() { _ = chConn.Close() }()
	chPingCtx, chPingCancel := context.WithTimeout(ctx, 5*time.Second)
	defer chPingCancel()
	if err := chConn.Ping(chPingCtx); err != nil {
		return fmt.Errorf("ping clickhouse: %w", err)
	}

	bus, err := notifications.NewNATSBus(ctx, natsURL, spiffeSource, logger)
	if err != nil {
		return fmt.Errorf("open notifications nats bus: %w", err)
	}
	defer bus.Close()

	svc := &notifications.Service{
		PG:        pg,
		CH:        chConn,
		Publisher: bus,
	}
	if err := svc.Ready(ctx); err != nil {
		return fmt.Errorf("notifications readiness: %w", err)
	}
	iamHTTPClient, err := workloadauth.MTLSClientForService(spiffeSource, workloadauth.ServiceIAM, nil)
	if err != nil {
		return fmt.Errorf("notifications iam mtls: %w", err)
	}
	iamClient, err := iamclient.NewClient(workloadauth.InternalURL(workloadauth.ServiceIAM), iamclient.WithHTTPClient(iamHTTPClient))
	if err != nil {
		return fmt.Errorf("notifications iam client: %w", err)
	}
	secretsHTTPClient, err := workloadauth.MTLSClientForService(spiffeSource, workloadauth.ServiceSecrets, nil)
	if err != nil {
		return fmt.Errorf("notifications secrets mtls: %w", err)
	}
	secrets, err := secretsclient.NewClientWithResponses(workloadauth.InternalURL(workloadauth.ServiceSecrets), secretsclient.WithHTTPClient(secretsHTTPClient))
	if err != nil {
		return fmt.Errorf("notifications secrets client: %w", err)
	}
	resendAPIKey, err := readRuntimeSecret(ctx, secrets, secretsclient.NotificationsResendAPIKeyName)
	if err != nil {
		return fmt.Errorf("notifications resend provider secret: %w", err)
	}
	emailSender, err := notifications.NewResendSender(resendAPIKey, resendFromAddress, resendFromName, &http.Client{Timeout: 5 * time.Second})
	if err != nil {
		return fmt.Errorf("notifications email sender: %w", err)
	}
	svc.Email = emailSender
	runtime, err := notifications.NewRuntime(pg, svc, logger)
	if err != nil {
		return fmt.Errorf("create notifications river runtime: %w", err)
	}
	if err := runtime.Start(ctx); err != nil {
		return err
	}
	if err := runtime.EnqueueMaintenance(ctx, 5*time.Second); err != nil {
		return fmt.Errorf("enqueue initial notifications maintenance: %w", err)
	}
	defer func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := runtime.Stop(stopCtx); err != nil {
			logger.ErrorContext(context.Background(), "notifications river runtime stop", "error", err)
		}
	}()

	bgCtx, bgCancel := context.WithCancel(ctx)
	defer bgCancel()
	go runBackgroundLoop(bgCtx, logger, runtime)
	go runCrossServiceFailureAlertLoop(bgCtx, logger, svc, notifications.CrossServiceFailureAlertConfig{
		OrgID:    platformAlertOrgID,
		Email:    platformAlertEmail,
		Lookback: platformAlertLookback,
		Limit:    100,
	})
	go func() {
		if err := bus.RunConsumer(bgCtx, svc); err != nil && !errors.Is(err, context.Canceled) {
			logger.ErrorContext(context.Background(), "notifications nats consumer stopped", "error", err)
			stop()
		}
	}()

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
	notificationsapi.NewAPI(privateMux, notificationsapi.Config{Version: serviceVersion, ListenAddr: listenAddr, Service: svc, Authorizer: iamclient.NewAuthorizer(iamClient), InstallationID: installationID})
	authenticated := auth.Middleware(auth.Config{
		IssuerURL: authIssuerURL,
		Audience:  authAudience,
	})(privateMux)
	rootMux.Handle("/", authenticated)

	internalPeerIDs, err := workloadauth.PeerIDsForSource(
		spiffeSource,
		workloadauth.ServiceBilling,
		workloadauth.ServiceGrafana,
		workloadauth.ServiceGovernance,
		workloadauth.ServiceIAM,
		workloadauth.ServiceProjects,
		workloadauth.ServiceSandboxRental,
		workloadauth.ServiceSourceCodeHosting,
	)
	if err != nil {
		return err
	}
	internalTLSConfig, err := workloadauth.MTLSServerConfigForAny(spiffeSource, internalPeerIDs...)
	if err != nil {
		return fmt.Errorf("notifications internal tls: %w", err)
	}
	internalMux := http.NewServeMux()
	notificationsapi.NewInternalAPI(internalMux, serviceVersion, "https://"+internalListenAddr, notificationsapi.InternalConfig{
		Service:            svc,
		PlatformAlertOrgID: platformAlertOrgID,
		PlatformAlertEmail: platformAlertEmail,
	})
	internalAllowlist, err := workloadauth.ServerPeerAllowlistMiddleware(internalPeerIDs, internalMux)
	if err != nil {
		return fmt.Errorf("notifications internal allowlist: %w", err)
	}

	public := httpserver.New(listenAddr, otelhttp.NewHandler(limitRequestBodies(rootMux, requestBodyLimit), serviceName))
	internal := httpserver.New(internalListenAddr, otelhttp.NewHandler(limitRequestBodies(internalAllowlist, requestBodyLimit), serviceName+"-internal"))
	internal.TLSConfig = internalTLSConfig
	return httpserver.RunPair(ctx, logger, public, internal)
}

func readRuntimeSecret(ctx context.Context, client *secretsclient.ClientWithResponses, secretName string) (string, error) {
	if client == nil {
		return "", fmt.Errorf("runtime secrets client is required")
	}
	secretCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	resp, err := client.ReadSecretWithResponse(secretCtx, secretName, nil)
	if err != nil {
		return "", fmt.Errorf("read runtime secret %s: %w", secretName, err)
	}
	if resp.JSON200 == nil {
		return "", fmt.Errorf("read runtime secret %s: unexpected status %d: %s", secretName, resp.StatusCode(), strings.TrimSpace(string(resp.Body)))
	}
	value := strings.TrimSpace(resp.JSON200.Value)
	if value == "" {
		return "", fmt.Errorf("runtime secret %s is empty", secretName)
	}
	return value, nil
}

func openPool(ctx context.Context, dsn string, maxConns, minConns, maxLifetimeSeconds, maxIdleSeconds int) (*pgxpool.Pool, error) {
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, err
	}
	config.MaxConns = int32FromInt(maxConns, "NOTIFICATIONS_PG_MAX_CONNS")
	config.MinConns = int32FromInt(minConns, "NOTIFICATIONS_PG_MIN_CONNS")
	config.MaxConnLifetime = time.Duration(maxLifetimeSeconds) * time.Second
	config.MaxConnIdleTime = time.Duration(maxIdleSeconds) * time.Second
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

func int32FromInt(value int, field string) int32 {
	const (
		minInt32 = -1 << 31
		maxInt32 = 1<<31 - 1
	)
	if value < minInt32 || value > maxInt32 {
		panic(fmt.Sprintf("%s exceeds int32 range: %d", field, value))
	}
	return int32(value) // #nosec G115 -- value is checked against the int32 range above.
}

func runBackgroundLoop(ctx context.Context, logger *slog.Logger, runtime *notifications.Runtime) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := runtime.EnqueueMaintenance(ctx, 5*time.Second); err != nil {
				logger.WarnContext(ctx, "notifications maintenance enqueue", "error", err)
			}
		}
	}
}

func runCrossServiceFailureAlertLoop(ctx context.Context, logger *slog.Logger, svc *notifications.Service, cfg notifications.CrossServiceFailureAlertConfig) {
	poll := func() {
		pollCtx, cancel := context.WithTimeout(ctx, platformAlertPollTimeout)
		defer cancel()
		count, err := svc.AlertCrossServiceFailures(pollCtx, cfg)
		if err != nil {
			logger.ErrorContext(ctx, "notifications cross-service failure alert poll", "error", err)
			return
		}
		if count > 0 {
			logger.WarnContext(ctx, "notifications cross-service failure alerts triggered", "count", count)
		}
	}
	poll()
	ticker := time.NewTicker(platformAlertPollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			poll()
		}
	}
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
