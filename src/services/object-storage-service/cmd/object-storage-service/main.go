package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
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
	"github.com/spiffe/go-spiffe/v2/spiffetls/tlsconfig"
	"github.com/spiffe/go-spiffe/v2/workloadapi"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	iamclient "github.com/verself/iam-service/client"
	objectstorageapi "github.com/verself/object-storage-service/internal/api"
	"github.com/verself/object-storage-service/internal/objectstorage"
	"github.com/verself/object-storage-service/migrations"
	verselfotel "github.com/verself/observability/otel"
	auth "github.com/verself/service-runtime/auth"
	"github.com/verself/service-runtime/envconfig"
	"github.com/verself/service-runtime/httpserver"
	workloadauth "github.com/verself/service-runtime/workload"
)

const serviceVersion = "1.0.0"

type runtimeRole string

const (
	runtimeRoleAdmin runtimeRole = "admin"
	runtimeRoleS3    runtimeRole = "s3"
)

func main() {
	if handled, err := runRecoveryCLI(context.Background(), os.Args[1:]); handled {
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	if handled, err := runMigrationCLI(context.Background(), os.Args[1:]); handled {
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func runMigrationCLI(ctx context.Context, args []string) (bool, error) {
	if len(args) < 1 || args[0] != "migrate" {
		return false, nil
	}
	opts, remaining, err := parseMigrationOptions(args[1:])
	if err != nil {
		return true, err
	}
	cfg, err := loadObjectStorageRuntimeConfig(opts.ResourceGraph, opts.ResourceName)
	if err != nil {
		return true, err
	}
	return true, migrations.RunCLIWithDSN(ctx, remaining, "object-storage-service", cfg.PostgresDSN)
}

func run(args []string) error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	opts, err := parseServiceRunOptions(args)
	if err != nil {
		return err
	}
	runtimeCfg, err := loadObjectStorageRuntimeConfig(opts.ResourceGraph, opts.ResourceName)
	if err != nil {
		return err
	}
	otelShutdown, logger, err := verselfotel.Init(ctx, verselfotel.Config{ServiceName: opts.Role.serviceName(), ServiceVersion: serviceVersion})
	if err != nil {
		return fmt.Errorf("otel init: %w", err)
	}
	defer func() { _ = otelShutdown(context.Background()) }()
	slog.SetDefault(logger)

	shared := envconfig.New()
	secretKeyHex := shared.RequireCredential(runtimeCfg.CredentialKEKName)
	if err := shared.Err(); err != nil {
		return err
	}

	spiffeSource, err := workloadauth.Source(ctx, runtimeCfg.SPIFFEEndpointSocket)
	if err != nil {
		return fmt.Errorf("object-storage spiffe source: %w", err)
	}
	defer func() {
		if err := spiffeSource.Close(); err != nil {
			logger.ErrorContext(context.Background(), opts.Role.serviceName()+" spiffe source close", "error", err)
		}
	}()
	if _, err := workloadauth.CurrentIDForService(spiffeSource, opts.Role.serviceName()); err != nil {
		return err
	}
	pg, err := newPostgres(ctx, runtimeCfg.PostgresDSN)
	if err != nil {
		return err
	}
	defer pg.Close()
	kek, err := decodeHex32(secretKeyHex)
	if err != nil {
		return fmt.Errorf("decode credential kek: %w", err)
	}
	secretBox, err := objectstorage.NewSecretBox(kek)
	if err != nil {
		return err
	}
	writerInstanceID := opts.WriterID
	if strings.TrimSpace(writerInstanceID) == "" {
		writerInstanceID = hostname()
	}
	baseConfig := objectstorage.Config{
		ServiceName:      opts.Role.serviceName(),
		Site:             runtimeCfg.Site,
		ServiceVersion:   serviceVersion,
		WriterInstanceID: writerInstanceID,
		Provider:         runtimeCfg.Provider,
		ProxyRegion:      runtimeCfg.R2Region,
	}
	switch opts.Role {
	case runtimeRoleAdmin:
		return runAdmin(ctx, logger, spiffeSource, pg, secretBox, baseConfig, runtimeCfg, opts)
	case runtimeRoleS3:
		return runS3(ctx, logger, spiffeSource, pg, secretBox, baseConfig, runtimeCfg, opts)
	default:
		return fmt.Errorf("unsupported object-storage role %q", opts.Role)
	}
}

func runAdmin(
	ctx context.Context,
	logger *slog.Logger,
	spiffeSource *workloadapi.X509Source,
	pg *pgxpool.Pool,
	secretBox *objectstorage.SecretBox,
	cfg objectstorage.Config,
	runtimeCfg objectStorageRuntimeConfig,
	opts serviceRunOptions,
) error {
	l := envconfig.New()
	authAudience := l.RequireCredential(runtimeCfg.AuthAudienceCredentialName)
	provider, proxyAccessKeyID, err := newBucketProviderFromConfig(ctx, l, cfg, runtimeCfg)
	if err := l.Err(); err != nil {
		return err
	}
	if err != nil {
		return err
	}

	adminClientIDs, err := workloadauth.PeerIDsForSource(spiffeSource, workloadauth.ServiceObjectStorageAdmin, workloadauth.ServiceDeployment)
	if err != nil {
		return err
	}
	iamHTTPClient, err := workloadauth.MTLSClientForService(spiffeSource, workloadauth.ServiceIAM, nil)
	if err != nil {
		return fmt.Errorf("object-storage iam mtls: %w", err)
	}
	iamClient, err := iamclient.NewClient(workloadauth.InternalURL(workloadauth.ServiceIAM), iamclient.WithHTTPClient(iamHTTPClient))
	if err != nil {
		return fmt.Errorf("object-storage iam client: %w", err)
	}
	cfg.ProxyAccessKeyID = proxyAccessKeyID
	cfg.DeploymentArtifactsBucket = runtimeCfg.DeploymentArtifactsBucket

	objectstorageapi.ConfigureAPIActivitySink(workloadauth.InternalURL(workloadauth.ServiceGovernance), spiffeSource)
	svc := &objectstorage.Service{
		Store:    objectstorage.NewStore(pg),
		Provider: provider,
		Secrets:  secretBox,
		Logger:   logger,
		Config:   cfg,
	}
	svc.SetAPIActivitySink(func(ctx context.Context, record objectstorage.APIActivity) error {
		return objectstorageapi.SendGovernanceAPIActivity(ctx, record)
	})
	if err := svc.AdminReady(ctx); err != nil {
		return fmt.Errorf("object-storage admin readiness: %w", err)
	}
	adminTLSConfig, err := workloadauth.MTLSServerConfigForAny(spiffeSource, adminClientIDs...)
	if err != nil {
		return fmt.Errorf("object-storage admin tls: %w", err)
	}
	adminMux := http.NewServeMux()
	adminMux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok\n")) })
	adminMux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		readyCtx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := svc.AdminReady(readyCtx); err != nil {
			http.Error(w, "not ready", http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte("ready\n"))
	})
	privateMux := http.NewServeMux()
	objectstorageapi.NewAPI(privateMux, objectstorageapi.Config{
		Version:    serviceVersion,
		ListenAddr: opts.AdminAddr,
		Service:    svc,
		Authorizer: iamclient.NewAuthorizer(iamClient),
	})
	protected := auth.Middleware(auth.Config{IssuerURL: runtimeCfg.AuthIssuerURL, Audience: authAudience})(privateMux)
	adminMux.Handle("/", objectStorageAdminHandler(privateMux, protected))
	adminAllowlist, err := workloadauth.ServerPeerAllowlistMiddleware(adminClientIDs, adminMux)
	if err != nil {
		return fmt.Errorf("object-storage admin allowlist: %w", err)
	}
	adminServer := httpserver.New(opts.AdminAddr, otelhttp.NewHandler(adminAllowlist, "object-storage-admin"))
	adminServer.TLSConfig = adminTLSConfig
	return httpserver.Run(ctx, logger, adminServer)
}

func newBucketProviderFromConfig(ctx context.Context, l *envconfig.Loader, cfg objectstorage.Config, runtimeCfg objectStorageRuntimeConfig) (objectstorage.BucketProvider, string, error) {
	providerHTTPClient := &http.Client{
		Transport: otelhttp.NewTransport(http.DefaultTransport),
		Timeout:   3 * time.Second,
	}
	switch cfg.Provider {
	case objectstorage.ProviderCloudflareR2:
		accessKeyID := l.RequireCredential(runtimeCfg.R2AdminAccessKeyIDName)
		secretAccessKey := l.RequireCredential(runtimeCfg.R2AdminSecretAccessKeyName)
		if err := l.Err(); err != nil {
			return nil, "", err
		}
		provider, err := objectstorage.NewR2BucketProvider(runtimeCfg.R2Endpoint, accessKeyID, secretAccessKey, cfg.ProxyRegion, providerHTTPClient)
		if err != nil {
			return nil, "", err
		}
		healthCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()
		if err := provider.Health(healthCtx); err != nil {
			return nil, "", err
		}
		return provider, "", nil
	default:
		return nil, "", fmt.Errorf("unsupported object-storage provider %q", cfg.Provider)
	}
}

func objectStorageAdminHandler(public http.Handler, protected http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isUnauthenticatedObjectStorageAdminPath(r.URL.Path) {
			public.ServeHTTP(w, r)
			return
		}
		protected.ServeHTTP(w, r)
	})
}

func isUnauthenticatedObjectStorageAdminPath(path string) bool {
	if path == "/healthz" || path == "/readyz" {
		return true
	}
	if strings.HasPrefix(path, "/internal/") {
		return true
	}
	return strings.HasPrefix(path, "/openapi")
}

func runS3(
	ctx context.Context,
	logger *slog.Logger,
	spiffeSource *workloadapi.X509Source,
	pg *pgxpool.Pool,
	secretBox *objectstorage.SecretBox,
	cfg objectstorage.Config,
	runtimeCfg objectStorageRuntimeConfig,
	opts serviceRunOptions,
) error {
	l := envconfig.New()
	proxyAccessKeyID := l.RequireCredential(runtimeCfg.R2ProxyAccessKeyIDName)
	proxySecretAccessKey := l.RequireCredential(runtimeCfg.R2ProxySecretAccessKeyName)
	if err := l.Err(); err != nil {
		return err
	}

	cfg.ProxyAccessKeyID = proxyAccessKeyID

	chConn, err := newClickHouseConn(ctx, spiffeSource, runtimeCfg.ClickHouseAddress, runtimeCfg.ClickHouseUser, runtimeCfg.ClickHouseCACertPath)
	if err != nil {
		return err
	}
	defer func() { _ = chConn.Close() }()

	upstreamS3Transport, err := cloneTransport(http.DefaultTransport)
	if err != nil {
		return fmt.Errorf("clone upstream s3 transport: %w", err)
	}
	upstreamS3Transport.ResponseHeaderTimeout = 5 * time.Second
	upstreamS3HTTPClient := &http.Client{
		Transport: otelhttp.NewTransport(upstreamS3Transport),
	}
	svc := &objectstorage.Service{
		CH:      chConn,
		Store:   objectstorage.NewStore(pg),
		Secrets: secretBox,
		Logger:  logger,
		Config:  cfg,
	}
	if err := svc.DataReady(ctx); err != nil {
		return fmt.Errorf("object-storage s3 readiness: %w", err)
	}
	s3Handler, err := objectstorage.NewS3Handler(
		svc,
		[]string{runtimeCfg.R2Endpoint},
		upstreamS3HTTPClient,
		proxyAccessKeyID,
		proxySecretAccessKey,
		cfg.ProxyRegion,
		logger,
	)
	if err != nil {
		return fmt.Errorf("object-storage s3 handler: %w", err)
	}
	bundleSource, err := workloadapi.NewBundleSource(ctx, workloadapi.WithClientOptions(workloadapi.WithAddr(runtimeCfg.SPIFFEEndpointSocket)))
	if err != nil {
		return fmt.Errorf("object-storage spiffe bundle source: %w", err)
	}
	defer func() { _ = bundleSource.Close() }()
	s3TLSConfig := newS3TLSConfig(spiffeSource, bundleSource)
	s3Mux := http.NewServeMux()
	s3Mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok\n")) })
	s3Mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		readyCtx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := svc.DataReady(readyCtx); err != nil {
			http.Error(w, "not ready", http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte("ready\n"))
	})
	s3Mux.Handle("/", s3Handler)
	s3Server := httpserver.New(opts.ListenAddr, otelhttp.NewHandler(s3PeerMiddleware(s3Mux), "object-storage-s3"))
	s3Server.TLSConfig = s3TLSConfig
	// S3 requests stream large bodies; drop the standard Read/Write timeouts.
	s3Server.ReadTimeout = 0
	s3Server.WriteTimeout = 0
	return httpserver.Run(ctx, logger, s3Server)
}

func newS3TLSConfig(source *workloadapi.X509Source, bundleSource *workloadapi.BundleSource) *tls.Config {
	config := tlsconfig.TLSServerConfig(source)
	config.ClientAuth = tls.RequestClientCert
	verifyPeer := tlsconfig.VerifyPeerCertificate(bundleSource, tlsconfig.AuthorizeAny())
	config.VerifyPeerCertificate = func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
		if len(rawCerts) == 0 {
			return nil
		}
		return verifyPeer(rawCerts, nil)
	}
	return config
}

func s3PeerMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if peerID, ok := workloadauth.PeerIDFromRequest(r); ok {
			r = r.WithContext(workloadauth.ContextWithPeerID(r.Context(), peerID))
		}
		next.ServeHTTP(w, r)
	})
}

func decodeHex32(raw string) ([]byte, error) {
	decoded, err := hex.DecodeString(strings.TrimSpace(raw))
	if err != nil {
		return nil, err
	}
	if len(decoded) != 32 {
		return nil, fmt.Errorf("decoded key must be 32 bytes, got %d", len(decoded))
	}
	return decoded, nil
}

func cloneTransport(base http.RoundTripper) (*http.Transport, error) {
	if base == nil {
		base = http.DefaultTransport
	}
	transport, ok := base.(*http.Transport)
	if !ok {
		return nil, fmt.Errorf("base round tripper %T is not an *http.Transport", base)
	}
	return transport.Clone(), nil
}

func (r runtimeRole) serviceName() string {
	switch r {
	case runtimeRoleAdmin:
		return "object-storage-admin"
	case runtimeRoleS3:
		return "object-storage-service"
	default:
		return "object-storage"
	}
}

func parseRuntimeRole(raw string) (runtimeRole, error) {
	switch runtimeRole(strings.TrimSpace(strings.ToLower(raw))) {
	case runtimeRoleAdmin:
		return runtimeRoleAdmin, nil
	case runtimeRoleS3:
		return runtimeRoleS3, nil
	default:
		return "", fmt.Errorf("--role must be %q or %q", runtimeRoleAdmin, runtimeRoleS3)
	}
}

func newPostgres(ctx context.Context, pgDSN string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(pgDSN)
	if err != nil {
		return nil, fmt.Errorf("parse postgres config: %w", err)
	}
	cfg.MaxConns = 12
	cfg.MaxConnLifetime = 30 * time.Minute
	cfg.MaxConnIdleTime = 5 * time.Minute
	pg, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("open postgres pool: %w", err)
	}
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := pg.Ping(pingCtx); err != nil {
		pg.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}
	return pg, nil
}

func newClickHouseConn(ctx context.Context, spiffeSource *workloadapi.X509Source, chAddress, chUser, caCertPath string) (clickhouse.Conn, error) {
	chTLSConfig, err := workloadauth.TLSConfigWithX509SourceAndCABundle(ctx, spiffeSource, caCertPath)
	if err != nil {
		return nil, fmt.Errorf("object-storage clickhouse tls: %w", err)
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
		return nil, fmt.Errorf("open clickhouse: %w", err)
	}
	return chConn, nil
}

func hostname() string {
	value, err := os.Hostname()
	if err != nil {
		return "unknown"
	}
	return value
}
