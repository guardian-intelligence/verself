package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/spiffe/go-spiffe/v2/spiffetls/tlsconfig"
	"github.com/spiffe/go-spiffe/v2/workloadapi"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	workloadauth "github.com/forge-metal/auth-middleware/workload"
	objectstorageapi "github.com/forge-metal/object-storage-service/internal/api"
	"github.com/forge-metal/object-storage-service/internal/objectstorage"
	fmotel "github.com/forge-metal/otel"
	secretsclient "github.com/forge-metal/secrets-service/client"

	_ "github.com/lib/pq"
)

const serviceVersion = "1.0.0"

type runtimeRole string

const (
	runtimeRoleAdmin runtimeRole = "admin"
	runtimeRoleS3    runtimeRole = "s3"
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

	role, err := parseRuntimeRole(requireEnv("OBJECT_STORAGE_ROLE"))
	if err != nil {
		return err
	}
	otelShutdown, logger, err := fmotel.Init(ctx, fmotel.Config{ServiceName: role.serviceName(), ServiceVersion: serviceVersion})
	if err != nil {
		return fmt.Errorf("otel init: %w", err)
	}
	defer otelShutdown(context.Background())
	slog.SetDefault(logger)

	pgDSN := requireEnv("OBJECT_STORAGE_PG_DSN")
	secretKeyHex := strings.TrimSpace(requireCredential("credential-kek"))

	spiffeSource, err := workloadauth.Source(ctx, envOr(workloadauth.EndpointSocketEnv, ""))
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
		Environment:      envOr("OBJECT_STORAGE_ENVIRONMENT", "single-node"),
		ServiceVersion:   serviceVersion,
		WriterInstanceID: envOr("OBJECT_STORAGE_WRITER_INSTANCE_ID", hostname()),
		ProxyRegion:      envOr("OBJECT_STORAGE_GARAGE_REGION", "garage"),
	}
	switch role {
	case runtimeRoleAdmin:
		return runAdmin(ctx, stop, logger, spiffeSource, pg, secretBox, baseConfig)
	case runtimeRoleS3:
		secretsURL := requireEnv("OBJECT_STORAGE_SECRETS_URL")
		runtimeSecretsClient, err := newRuntimeSecretsClient(spiffeSource, secretsURL)
		if err != nil {
			return err
		}
		return runS3(ctx, stop, logger, spiffeSource, runtimeSecretsClient, pg, secretBox, baseConfig)
	default:
		return fmt.Errorf("unsupported object-storage role %q", role)
	}
}

func runAdmin(
	ctx context.Context,
	stop context.CancelFunc,
	logger *slog.Logger,
	spiffeSource *workloadapi.X509Source,
	pg *sql.DB,
	secretBox *objectstorage.SecretBox,
	cfg objectstorage.Config,
) error {
	adminListenAddr := envOr("OBJECT_STORAGE_ADMIN_LISTEN_ADDR", "127.0.0.1:4257")
	governanceAuditURL := requireEnv("OBJECT_STORAGE_GOVERNANCE_AUDIT_URL")
	adminClientIDs, err := workloadauth.PeerIDsForSource(spiffeSource, workloadauth.ServiceObjectStorageAdmin)
	if err != nil {
		return err
	}
	garageAdminHTTPClient := &http.Client{
		Transport: otelhttp.NewTransport(http.DefaultTransport),
		Timeout:   3 * time.Second,
	}
	garageClient, err := objectstorage.NewGarageAdminClient(
		splitEnvList(requireEnv("OBJECT_STORAGE_GARAGE_ADMIN_URLS")),
		requireCredential("garage-admin-token"),
		garageAdminHTTPClient,
	)
	if err != nil {
		return err
	}
	cfg.ProxyAccessKeyID = requireCredential("garage-proxy-access-key-id")

	objectstorageapi.ConfigureAuditSink(governanceAuditURL, spiffeSource)
	svc := &objectstorage.Service{
		PG:      pg,
		Store:   &objectstorage.Store{DB: pg},
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
	adminServer := &http.Server{
		Addr:              adminListenAddr,
		Handler:           otelhttp.NewHandler(adminAllowlist, "object-storage-admin"),
		TLSConfig:         adminTLSConfig,
		ReadHeaderTimeout: 2 * time.Second,
		ReadTimeout:       5 * time.Second,
		WriteTimeout:      5 * time.Second,
		IdleTimeout:       30 * time.Second,
		MaxHeaderBytes:    16 << 10,
	}
	logger.InfoContext(ctx, "object-storage-admin listening", "addr", adminListenAddr)
	return serveSingleServer(ctx, stop, logger, adminServer, "object-storage admin")
}

func runS3(
	ctx context.Context,
	stop context.CancelFunc,
	logger *slog.Logger,
	spiffeSource *workloadapi.X509Source,
	runtimeSecretsClient *secretsclient.ClientWithResponses,
	pg *sql.DB,
	secretBox *objectstorage.SecretBox,
	cfg objectstorage.Config,
) error {
	s3ListenAddr := envOr("OBJECT_STORAGE_S3_LISTEN_ADDR", "127.0.0.1:4256")
	chAddress := envOr("OBJECT_STORAGE_CH_ADDRESS", "127.0.0.1:9440")
	chUser := envOr("OBJECT_STORAGE_CH_USER", "object_storage_service")
	runtimeSecrets, err := fetchObjectStorageRuntimeSecrets(ctx, runtimeSecretsClient,
		secretsclient.ObjectStorageGarageProxyAccessKeyIDName,
		secretsclient.ObjectStorageGarageProxySecretAccessKeyName,
	)
	if err != nil {
		return err
	}
	cfg.ProxyAccessKeyID = runtimeSecrets[secretsclient.ObjectStorageGarageProxyAccessKeyIDName]

	chConn, err := newClickHouseConn(ctx, spiffeSource, chAddress, chUser)
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
		PG:      pg,
		CH:      chConn,
		Store:   &objectstorage.Store{DB: pg},
		Secrets: secretBox,
		Logger:  logger,
		Config:  cfg,
	}
	if err := svc.DataReady(ctx); err != nil {
		return fmt.Errorf("object-storage s3 readiness: %w", err)
	}
	s3Handler, err := objectstorage.NewS3Handler(
		svc,
		splitEnvList(requireEnv("OBJECT_STORAGE_GARAGE_S3_URLS")),
		garageS3HTTPClient,
		runtimeSecrets[secretsclient.ObjectStorageGarageProxyAccessKeyIDName],
		runtimeSecrets[secretsclient.ObjectStorageGarageProxySecretAccessKeyName],
		cfg.ProxyRegion,
		logger,
	)
	if err != nil {
		return fmt.Errorf("object-storage s3 handler: %w", err)
	}
	bundleSource, err := workloadapi.NewBundleSource(ctx, workloadapi.WithClientOptions(workloadapi.WithAddr(envOr(workloadauth.EndpointSocketEnv, ""))))
	if err != nil {
		return fmt.Errorf("object-storage spiffe bundle source: %w", err)
	}
	defer bundleSource.Close()
	s3TLSConfig, err := newS3TLSConfig(bundleSource)
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
	s3Server := &http.Server{
		Addr:              s3ListenAddr,
		Handler:           otelhttp.NewHandler(s3PeerMiddleware(s3Mux), "object-storage-s3"),
		TLSConfig:         s3TLSConfig,
		ReadHeaderTimeout: 2 * time.Second,
		IdleTimeout:       30 * time.Second,
		MaxHeaderBytes:    16 << 10,
	}
	logger.InfoContext(ctx, "object-storage-service listening", "addr", s3ListenAddr)
	return serveSingleServer(ctx, stop, logger, s3Server, "object-storage s3")
}

func serveSingleServer(ctx context.Context, stop context.CancelFunc, logger *slog.Logger, server *http.Server, description string) error {
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			logger.ErrorContext(context.Background(), description+" shutdown", "error", err)
		}
	}()
	errCh := make(chan error, 1)
	go func() {
		if err := server.ListenAndServeTLS("", ""); err != nil && err != http.ErrServerClosed {
			errCh <- err
			return
		}
		errCh <- nil
	}()
	if err := <-errCh; err != nil {
		stop()
		return fmt.Errorf("serve %s: %w", description, err)
	}
	return nil
}

func newS3TLSConfig(bundleSource *workloadapi.BundleSource) (*tls.Config, error) {
	cert, err := tls.LoadX509KeyPair(credentialPath("s3-tls-cert"), credentialPath("s3-tls-key"))
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

func newRuntimeSecretsClient(spiffeSource *workloadapi.X509Source, secretsURL string) (*secretsclient.ClientWithResponses, error) {
	httpClient, err := workloadauth.MTLSClientForService(spiffeSource, workloadauth.ServiceSecrets, nil)
	if err != nil {
		return nil, fmt.Errorf("object-storage secrets runtime mtls: %w", err)
	}
	client, err := secretsclient.NewClientWithResponses(secretsURL, secretsclient.WithHTTPClient(httpClient))
	if err != nil {
		return nil, fmt.Errorf("object-storage secrets runtime client: %w", err)
	}
	return client, nil
}

func newPostgres(ctx context.Context, pgDSN string) (*sql.DB, error) {
	pg, err := sql.Open("postgres", pgDSN)
	if err != nil {
		return nil, fmt.Errorf("open postgres: %w", err)
	}
	pg.SetMaxOpenConns(12)
	pg.SetMaxIdleConns(4)
	pg.SetConnMaxLifetime(30 * time.Minute)
	pg.SetConnMaxIdleTime(5 * time.Minute)
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := pg.PingContext(pingCtx); err != nil {
		_ = pg.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}
	return pg, nil
}

func newClickHouseConn(ctx context.Context, spiffeSource *workloadapi.X509Source, chAddress, chUser string) (clickhouse.Conn, error) {
	chTLSConfig, err := workloadauth.TLSConfigWithX509SourceAndCABundle(ctx, spiffeSource, credentialPath("clickhouse-ca-cert"))
	if err != nil {
		return nil, fmt.Errorf("object-storage clickhouse tls: %w", err)
	}
	chConn, err := clickhouse.Open(&clickhouse.Options{
		Addr: []string{chAddress},
		Auth: clickhouse.Auth{
			Database: "forge_metal",
			Username: chUser,
		},
		TLS: chTLSConfig,
	})
	if err != nil {
		return nil, fmt.Errorf("open clickhouse: %w", err)
	}
	return chConn, nil
}

func requireEnv(name string) string {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		panic(fmt.Sprintf("%s is required", name))
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
	data, err := os.ReadFile(credentialPath(name))
	if err != nil {
		panic(fmt.Sprintf("read credential %s: %v", name, err))
	}
	return strings.TrimSpace(string(data))
}

func credentialPath(name string) string {
	base := strings.TrimSpace(os.Getenv("CREDENTIALS_DIRECTORY"))
	if base == "" {
		panic("missing CREDENTIALS_DIRECTORY for credential " + name)
	}
	return filepath.Join(base, name)
}

func fetchObjectStorageRuntimeSecrets(ctx context.Context, client *secretsclient.ClientWithResponses, secretNames ...string) (map[string]string, error) {
	if client == nil {
		return nil, fmt.Errorf("object-storage runtime secret client is required")
	}
	if len(secretNames) == 0 {
		return map[string]string{}, nil
	}
	secretCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	values := make(map[string]string, len(secretNames))
	for _, secretName := range secretNames {
		resp, err := client.ReadSecretWithResponse(secretCtx, secretName)
		if err != nil {
			return nil, fmt.Errorf("resolve object-storage runtime secret %s: %w", secretName, err)
		}
		if resp.JSON200 == nil {
			return nil, fmt.Errorf("resolve object-storage runtime secret %s: unexpected status %d: %s", secretName, resp.StatusCode(), strings.TrimSpace(string(resp.Body)))
		}
		values[secretName] = resp.JSON200.Value
	}
	for _, name := range secretNames {
		if strings.TrimSpace(values[name]) == "" {
			return nil, fmt.Errorf("object-storage runtime secret %s is missing", name)
		}
	}
	return values, nil
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
