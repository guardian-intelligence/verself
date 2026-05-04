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

	objectstorageapi "github.com/verself/object-storage-service/internal/api"
	"github.com/verself/object-storage-service/internal/objectstorage"
	"github.com/verself/object-storage-service/migrations"
	verselfotel "github.com/verself/observability/otel"
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
	return true, migrations.RunCLI(ctx, os.Args[2:], "object-storage-service")
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	roleCfg := envconfig.New()
	roleRaw := roleCfg.RequireString("OBJECT_STORAGE_ROLE")
	if err := roleCfg.Err(); err != nil {
		return err
	}
	role, err := parseRuntimeRole(roleRaw)
	if err != nil {
		return err
	}
	otelShutdown, logger, err := verselfotel.Init(ctx, verselfotel.Config{ServiceName: role.serviceName(), ServiceVersion: serviceVersion})
	if err != nil {
		return fmt.Errorf("otel init: %w", err)
	}
	defer func() { _ = otelShutdown(context.Background()) }()
	slog.SetDefault(logger)

	shared := envconfig.New()
	pgDSN := shared.RequireString("VERSELF_PG_DSN")
	secretKeyHex := shared.RequireCredential("credential-kek")
	spiffeEndpoint := shared.String(workloadauth.EndpointSocketEnv, "")
	environment := shared.String("OBJECT_STORAGE_ENVIRONMENT", "single-node")
	writerInstanceID := shared.String("OBJECT_STORAGE_WRITER_INSTANCE_ID", hostname())
	proxyRegion := shared.String("OBJECT_STORAGE_GARAGE_REGION", "garage")
	if err := shared.Err(); err != nil {
		return err
	}

	spiffeSource, err := workloadauth.Source(ctx, spiffeEndpoint)
	if err != nil {
		return fmt.Errorf("object-storage spiffe source: %w", err)
	}
	defer func() {
		if err := spiffeSource.Close(); err != nil {
			logger.ErrorContext(context.Background(), role.serviceName()+" spiffe source close", "error", err)
		}
	}()
	if _, err := workloadauth.CurrentIDForService(spiffeSource, role.serviceName()); err != nil {
		return err
	}
	pg, err := newPostgres(ctx, pgDSN)
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
	baseConfig := objectstorage.Config{
		ServiceName:      role.serviceName(),
		Environment:      environment,
		ServiceVersion:   serviceVersion,
		WriterInstanceID: writerInstanceID,
		ProxyRegion:      proxyRegion,
	}
	switch role {
	case runtimeRoleAdmin:
		return runAdmin(ctx, logger, spiffeSource, pg, secretBox, baseConfig)
	case runtimeRoleS3:
		return runS3(ctx, logger, spiffeSource, pg, secretBox, baseConfig)
	default:
		return fmt.Errorf("unsupported object-storage role %q", role)
	}
}

func runAdmin(
	ctx context.Context,
	logger *slog.Logger,
	spiffeSource *workloadapi.X509Source,
	pg *pgxpool.Pool,
	secretBox *objectstorage.SecretBox,
	cfg objectstorage.Config,
) error {
	l := envconfig.New()
	adminListenAddr := l.String("OBJECT_STORAGE_ADMIN_LISTEN_ADDR", "127.0.0.1:4257")
	governanceAuditURL := l.RequireURL("OBJECT_STORAGE_GOVERNANCE_AUDIT_URL")
	garageAdminURLs := splitEnvList(l.RequireString("OBJECT_STORAGE_GARAGE_ADMIN_URLS"))
	garageAdminToken := l.RequireCredential("garage-admin-token")
	proxyAccessKeyID := l.RequireCredential("garage-proxy-access-key-id")
	if err := l.Err(); err != nil {
		return err
	}

	adminClientIDs, err := workloadauth.PeerIDsForSource(spiffeSource, workloadauth.ServiceObjectStorageAdmin)
	if err != nil {
		return err
	}
	garageAdminHTTPClient := &http.Client{
		Transport: otelhttp.NewTransport(http.DefaultTransport),
		Timeout:   3 * time.Second,
	}
	garageClient, err := objectstorage.NewGarageAdminClient(garageAdminURLs, garageAdminToken, garageAdminHTTPClient)
	if err != nil {
		return err
	}
	cfg.ProxyAccessKeyID = proxyAccessKeyID

	objectstorageapi.ConfigureAuditSink(governanceAuditURL, spiffeSource)
	svc := &objectstorage.Service{
		Store:   objectstorage.NewStore(pg),
		Garage:  garageClient,
		Secrets: secretBox,
		Logger:  logger,
		Config:  cfg,
	}
	svc.SetAuditSink(func(ctx context.Context, record objectstorage.AuditRecord) error {
		return objectstorageapi.SendGovernanceAudit(ctx, record)
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
	objectstorageapi.NewAPI(adminMux, objectstorageapi.Config{
		Version:    serviceVersion,
		ListenAddr: adminListenAddr,
		Service:    svc,
	})
	adminAllowlist, err := workloadauth.ServerPeerAllowlistMiddleware(adminClientIDs, adminMux)
	if err != nil {
		return fmt.Errorf("object-storage admin allowlist: %w", err)
	}
	adminServer := httpserver.New(adminListenAddr, otelhttp.NewHandler(adminAllowlist, "object-storage-admin"))
	adminServer.TLSConfig = adminTLSConfig
	return httpserver.Run(ctx, logger, adminServer)
}

func runS3(
	ctx context.Context,
	logger *slog.Logger,
	spiffeSource *workloadapi.X509Source,
	pg *pgxpool.Pool,
	secretBox *objectstorage.SecretBox,
	cfg objectstorage.Config,
) error {
	l := envconfig.New()
	s3ListenAddr := l.String("VERSELF_LISTEN_ADDR", "127.0.0.1:4256")
	chAddress := l.String("VERSELF_CLICKHOUSE_ADDRESS", "127.0.0.1:9440")
	chUser := l.String("VERSELF_CLICKHOUSE_USER", "object_storage_service")
	garageS3URLs := splitEnvList(l.RequireString("OBJECT_STORAGE_GARAGE_S3_URLS"))
	proxyAccessKeyID := l.RequireCredential("garage-proxy-access-key-id")
	proxySecretAccessKey := l.RequireCredential("garage-proxy-secret-access-key")
	s3TLSCertPath := l.RequireCredentialPath("s3-tls-cert")
	s3TLSKeyPath := l.RequireCredentialPath("s3-tls-key")
	chCACertPath := l.RequireCredentialPath("clickhouse-ca-cert")
	spiffeEndpoint := l.String(workloadauth.EndpointSocketEnv, "")
	if err := l.Err(); err != nil {
		return err
	}

	cfg.ProxyAccessKeyID = proxyAccessKeyID

	chConn, err := newClickHouseConn(ctx, spiffeSource, chAddress, chUser, chCACertPath)
	if err != nil {
		return err
	}
	defer func() { _ = chConn.Close() }()

	garageS3Transport, err := cloneTransport(http.DefaultTransport)
	if err != nil {
		return fmt.Errorf("clone garage s3 transport: %w", err)
	}
	garageS3Transport.ResponseHeaderTimeout = 5 * time.Second
	garageS3HTTPClient := &http.Client{
		Transport: otelhttp.NewTransport(garageS3Transport),
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
		garageS3URLs,
		garageS3HTTPClient,
		proxyAccessKeyID,
		proxySecretAccessKey,
		cfg.ProxyRegion,
		logger,
	)
	if err != nil {
		return fmt.Errorf("object-storage s3 handler: %w", err)
	}
	bundleSource, err := workloadapi.NewBundleSource(ctx, workloadapi.WithClientOptions(workloadapi.WithAddr(spiffeEndpoint)))
	if err != nil {
		return fmt.Errorf("object-storage spiffe bundle source: %w", err)
	}
	defer func() { _ = bundleSource.Close() }()
	s3TLSConfig, err := newS3TLSConfig(bundleSource, s3TLSCertPath, s3TLSKeyPath)
	if err != nil {
		return fmt.Errorf("object-storage s3 tls: %w", err)
	}
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
	s3Server := httpserver.New(s3ListenAddr, otelhttp.NewHandler(s3PeerMiddleware(s3Mux), "object-storage-s3"))
	s3Server.TLSConfig = s3TLSConfig
	// S3 requests stream large bodies; drop the standard Read/Write timeouts.
	s3Server.ReadTimeout = 0
	s3Server.WriteTimeout = 0
	return httpserver.Run(ctx, logger, s3Server)
}

func newS3TLSConfig(bundleSource *workloadapi.BundleSource, certPath, keyPath string) (*tls.Config, error) {
	cert, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		return nil, err
	}
	config := &tls.Config{
		MinVersion:   tls.VersionTLS12,
		Certificates: []tls.Certificate{cert},
		ClientAuth:   tls.RequestClientCert,
	}
	verifyPeer := tlsconfig.VerifyPeerCertificate(bundleSource, tlsconfig.AuthorizeAny())
	config.VerifyPeerCertificate = func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
		if len(rawCerts) == 0 {
			return nil
		}
		return verifyPeer(rawCerts, nil)
	}
	return config, nil
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
		return "", fmt.Errorf("OBJECT_STORAGE_ROLE must be %q or %q", runtimeRoleAdmin, runtimeRoleS3)
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

func splitEnvList(raw string) []string {
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		out = append(out, part)
	}
	return out
}

func hostname() string {
	value, err := os.Hostname()
	if err != nil {
		return "unknown"
	}
	return value
}
