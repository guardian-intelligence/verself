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

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	githubapi "github.com/verself/github-integration-service/internal/api"
	"github.com/verself/github-integration-service/internal/githubintegration"
	"github.com/verself/github-integration-service/migrations"
	iamclient "github.com/verself/iam-service/client"
	verselfotel "github.com/verself/observability/otel"
	sandboxrentalclient "github.com/verself/sandbox-rental-service/client"
	secretsclient "github.com/verself/secrets-service/client"
	auth "github.com/verself/service-runtime/auth"
	"github.com/verself/service-runtime/envconfig"
	"github.com/verself/service-runtime/httpserver"
	workloadauth "github.com/verself/service-runtime/workload"
)

const (
	serviceName       = githubintegration.ServiceName
	serviceVersion    = "0.1.0"
	defaultListenAddr = "127.0.0.1:4272"

	sandboxRunnerSubmissionTimeout = 60 * time.Second
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

	cfg := envconfig.New()
	pgDSN := cfg.RequireString("VERSELF_PG_DSN")
	listenAddr := cfg.String("VERSELF_LISTEN_ADDR", defaultListenAddr)
	chAddress := cfg.String("VERSELF_CLICKHOUSE_ADDRESS", "127.0.0.1:9440")
	chUser := cfg.String("VERSELF_CLICKHOUSE_USER", "github_integration_service")
	chCACertPath := cfg.RequireCredentialPath("clickhouse-ca-cert")
	spiffeEndpoint := cfg.String(workloadauth.EndpointSocketEnv, "")
	authIssuerURL := cfg.RequireURL("VERSELF_AUTH_ISSUER_URL")
	authAudience := cfg.RequireCredential("auth-audience")
	appID := cfg.Int64("GITHUB_APP_ID", 0)
	appSlug := cfg.String("GITHUB_APP_SLUG", "")
	appSetupURL := cfg.String("GITHUB_APP_SETUP_URL", "")
	apiBaseURL := cfg.URL("GITHUB_API_BASE_URL", "https://api.github.com")
	oauthClientID := cfg.String("GITHUB_OAUTH_CLIENT_ID", "")
	oauthAuthorizeURL := cfg.String("GITHUB_OAUTH_AUTHORIZE_URL", "https://github.com/login/oauth/authorize")
	oauthTokenURL := cfg.String("GITHUB_OAUTH_TOKEN_URL", "https://github.com/login/oauth/access_token")
	oauthRedirectURL := cfg.String("GITHUB_OAUTH_REDIRECT_URL", "")
	runnerGroupID := cfg.Int64("GITHUB_RUNNER_GROUP_ID", 1)
	runnerClassPrefix := cfg.String("GITHUB_RUNNER_CLASS_PREFIX", "verself-")
	repositoryRunnerClassActiveLimit := int32FromInt(cfg.Int("GITHUB_REPOSITORY_RUNNER_CLASS_ACTIVE_LIMIT", 15), "GITHUB_REPOSITORY_RUNNER_CLASS_ACTIVE_LIMIT")
	workerInterval := cfg.Duration("GITHUB_WORKER_INTERVAL", 500*time.Millisecond)
	workerBatchSize := int32FromInt(cfg.Int("GITHUB_WORKER_BATCH_SIZE", 16), "GITHUB_WORKER_BATCH_SIZE")
	maxDeliveryTries := int32FromInt(cfg.Int("GITHUB_MAX_DELIVERY_TRIES", 8), "GITHUB_MAX_DELIVERY_TRIES")
	pgMaxConns := cfg.Int("VERSELF_PG_MAX_CONNS", 8)
	pgMinConns := cfg.Int("VERSELF_PG_MIN_CONNS", 1)
	pgMaxLifetime := cfg.Int("VERSELF_PG_CONN_MAX_LIFETIME_SECONDS", 1800)
	pgMaxIdle := cfg.Int("VERSELF_PG_CONN_MAX_IDLE_SECONDS", 300)
	if err := cfg.Err(); err != nil {
		return err
	}

	otelShutdown, logger, err := verselfotel.Init(ctx, verselfotel.Config{ServiceName: serviceName, ServiceVersion: serviceVersion})
	if err != nil {
		return fmt.Errorf("otel init: %w", err)
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := otelShutdown(shutdownCtx); err != nil {
			logger.ErrorContext(context.Background(), "github-integration-service otel shutdown", "error", err)
		}
	}()
	slog.SetDefault(logger)

	pgConfig, err := pgxpool.ParseConfig(pgDSN)
	if err != nil {
		return fmt.Errorf("parse postgres dsn: %w", err)
	}
	pgConfig.MaxConns = int32FromInt(pgMaxConns, "VERSELF_PG_MAX_CONNS")
	pgConfig.MinConns = int32FromInt(pgMinConns, "VERSELF_PG_MIN_CONNS")
	pgConfig.MaxConnLifetime = time.Duration(pgMaxLifetime) * time.Second
	pgConfig.MaxConnIdleTime = time.Duration(pgMaxIdle) * time.Second
	pgPool, err := pgxpool.NewWithConfig(ctx, pgConfig)
	if err != nil {
		return fmt.Errorf("open postgres pool: %w", err)
	}
	defer pgPool.Close()
	if err := pgPool.Ping(ctx); err != nil {
		return fmt.Errorf("ping postgres: %w", err)
	}

	spiffeSource, err := workloadauth.Source(ctx, spiffeEndpoint)
	if err != nil {
		return fmt.Errorf("github integration spiffe workload source: %w", err)
	}
	defer func() {
		if err := spiffeSource.Close(); err != nil {
			logger.ErrorContext(context.Background(), "github-integration-service spiffe source close", "error", err)
		}
	}()
	secretsHTTPClient, err := workloadauth.MTLSClientForService(spiffeSource, workloadauth.ServiceSecrets, nil)
	if err != nil {
		return fmt.Errorf("github integration secrets mtls: %w", err)
	}
	secretsClient, err := secretsclient.NewClient(workloadauth.InternalURL(workloadauth.ServiceSecrets), secretsclient.WithHTTPClient(secretsHTTPClient))
	if err != nil {
		return fmt.Errorf("github integration secrets client: %w", err)
	}
	providerSecrets, err := readRuntimeSecrets(ctx, secretsClient,
		secretsclient.GitHubIntegrationPrivateKeyName,
		secretsclient.GitHubIntegrationWebhookSecretName,
	)
	if err != nil {
		return fmt.Errorf("github integration provider secrets: %w", err)
	}
	privateKey := strings.TrimSpace(providerSecrets[secretsclient.GitHubIntegrationPrivateKeyName])
	webhookSecret := strings.TrimSpace(providerSecrets[secretsclient.GitHubIntegrationWebhookSecretName])
	oauthClientSecret, err := readOptionalRuntimeSecret(ctx, secretsClient, secretsclient.GitHubIntegrationOAuthClientSecretName)
	if err != nil {
		logger.WarnContext(ctx, "github integration oauth client secret unavailable", "error", err)
	}
	iamHTTPClient, err := workloadauth.MTLSClientForService(spiffeSource, workloadauth.ServiceIAM, nil)
	if err != nil {
		return fmt.Errorf("github integration iam mtls: %w", err)
	}
	iamClient, err := iamclient.NewClient(workloadauth.InternalURL(workloadauth.ServiceIAM), iamclient.WithHTTPClient(iamHTTPClient))
	if err != nil {
		return fmt.Errorf("github integration iam client: %w", err)
	}
	sandboxHTTPClient, err := workloadauth.MTLSClientForServiceWithTimeouts(spiffeSource, workloadauth.ServiceSandboxRental, nil, workloadauth.ServiceClientTimeouts{
		ResponseHeader: sandboxRunnerSubmissionTimeout,
		Total:          sandboxRunnerSubmissionTimeout,
	})
	if err != nil {
		return fmt.Errorf("github integration sandbox mtls: %w", err)
	}
	sandboxClient, err := sandboxrentalclient.NewClient(workloadauth.InternalURL(workloadauth.ServiceSandboxRental), sandboxrentalclient.WithHTTPClient(sandboxHTTPClient))
	if err != nil {
		return fmt.Errorf("github integration sandbox client: %w", err)
	}
	chTLSConfig, err := workloadauth.TLSConfigWithX509SourceAndCABundle(ctx, spiffeSource, chCACertPath)
	if err != nil {
		return fmt.Errorf("github integration clickhouse tls: %w", err)
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

	svc, err := githubintegration.NewService(githubintegration.Config{
		AppID:                            appID,
		AppSlug:                          appSlug,
		AppSetupURL:                      appSetupURL,
		PrivateKeyPEM:                    privateKey,
		WebhookSecret:                    webhookSecret,
		APIBaseURL:                       apiBaseURL,
		OAuthClientID:                    oauthClientID,
		OAuthClientSecret:                strings.TrimSpace(oauthClientSecret),
		OAuthAuthorizeURL:                oauthAuthorizeURL,
		OAuthTokenURL:                    oauthTokenURL,
		OAuthRedirectURL:                 oauthRedirectURL,
		UserTokenSecretPrefix:            secretsclient.GitHubIntegrationUserTokenSecretPrefix,
		RunnerGroupID:                    runnerGroupID,
		RunnerClassPrefix:                runnerClassPrefix,
		RepositoryRunnerClassActiveLimit: repositoryRunnerClassActiveLimit,
		WorkerInterval:                   workerInterval,
		WorkerBatchSize:                  workerBatchSize,
		MaxDeliveryTries:                 maxDeliveryTries,
		Logger:                           logger,
		PG:                               pgPool,
		CH:                               chConn,
		Sandbox:                          sandboxClient,
		Secrets:                          secretsClient,
		HTTPClient:                       http.DefaultClient,
	})
	if err != nil {
		return err
	}
	if err := svc.Ready(ctx); err != nil {
		return fmt.Errorf("github integration readiness: %w", err)
	}

	workerCtx, workerCancel := context.WithCancel(ctx)
	defer workerCancel()
	go func() {
		if err := svc.RunWorker(workerCtx); err != nil && workerCtx.Err() == nil {
			logger.ErrorContext(context.Background(), "github integration worker stopped", "error", err)
		}
	}()

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		readyCtx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := svc.Ready(readyCtx); err != nil {
			http.Error(w, "not ready", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ready\n"))
	})
	mux.Handle("/api/v1/github/webhooks", svc.WebhookHandler())
	privateMux := http.NewServeMux()
	githubapi.NewAPI(privateMux, githubapi.Config{
		Version:    serviceVersion,
		ListenAddr: listenAddr,
		Service:    svc,
		Authorizer: iamclient.NewAuthorizer(iamClient),
	})
	authenticated := auth.Middleware(auth.Config{
		IssuerURL: authIssuerURL,
		Audience:  authAudience,
	})(privateMux)
	mux.Handle("/", authenticated)

	server := httpserver.New(listenAddr, otelhttp.NewHandler(mux, serviceName))
	return httpserver.Run(ctx, logger, server)
}

func readRuntimeSecrets(ctx context.Context, client *secretsclient.Client, secretNames ...string) (map[string]string, error) {
	if client == nil {
		return nil, fmt.Errorf("runtime secrets client is required")
	}
	secretCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	values := make(map[string]string, len(secretNames))
	for _, secretName := range secretNames {
		resp, err := client.ReadSecret(secretCtx, secretsclient.ReadSecretRequest{Name: secretsclient.SecretName(secretName)})
		if err != nil {
			return nil, fmt.Errorf("read runtime secret %s: %w", secretName, err)
		}
		if resp.Result == nil {
			return nil, fmt.Errorf("read runtime secret %s: unexpected status %d: %s", secretName, resp.StatusCode, strings.TrimSpace(string(resp.Body)))
		}
		values[secretName] = string(resp.Result.Value)
	}
	return values, nil
}

func readOptionalRuntimeSecret(ctx context.Context, client *secretsclient.Client, secretName string) (string, error) {
	values, err := readRuntimeSecrets(ctx, client, secretName)
	if err != nil {
		return "", err
	}
	return values[secretName], nil
}

func int32FromInt(value int, field string) int32 {
	const (
		minInt32 = -1 << 31
		maxInt32 = 1<<31 - 1
	)
	if value < minInt32 || value > maxInt32 {
		panic(fmt.Sprintf("%s exceeds int32 range: %d", field, value))
	}
	return int32(value)
}
