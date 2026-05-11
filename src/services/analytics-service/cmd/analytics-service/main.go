package main

import (
	"compress/gzip"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/spiffe/go-spiffe/v2/workloadapi"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	"github.com/verself/analytics-service/internal/analytics"
	verselfotel "github.com/verself/observability/otel"
	"github.com/verself/service-runtime/envconfig"
	"github.com/verself/service-runtime/httpserver"
	workloadauth "github.com/verself/service-runtime/workload"
)

const (
	serviceName       = "analytics-service"
	serviceVersion    = "0.1.0"
	requestBodyLimit  = 1 << 20
	defaultListenAddr = "127.0.0.1:4266"
	defaultCHAddress  = "127.0.0.1:9440"
	defaultCHUser     = "analytics_service"
	githubOIDCIssuer  = "https://token.actions.githubusercontent.com"
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

	otelShutdown, logger, err := verselfotel.Init(ctx, verselfotel.Config{ServiceName: serviceName, ServiceVersion: serviceVersion})
	if err != nil {
		return fmt.Errorf("otel init: %w", err)
	}
	defer func() {
		if err := otelShutdown(context.Background()); err != nil {
			logger.ErrorContext(context.Background(), "analytics-service otel shutdown", "error", err)
		}
	}()
	slog.SetDefault(logger)

	cfg := envconfig.New()
	listenAddr := cfg.String("VERSELF_LISTEN_ADDR", defaultListenAddr)
	chAddress := cfg.String("VERSELF_CLICKHOUSE_ADDRESS", defaultCHAddress)
	chUser := cfg.String("VERSELF_CLICKHOUSE_USER", defaultCHUser)
	chCACertPath := cfg.RequireCredentialPath("clickhouse-ca-cert")
	spiffeEndpoint := cfg.String(workloadauth.EndpointSocketEnv, "")
	oidcAudience := cfg.RequireURL("ANALYTICS_GITHUB_OIDC_AUDIENCE")
	allowedRepositories := cfg.RequireString("ANALYTICS_GITHUB_ALLOWED_REPOSITORIES")
	allowedPrefixes := analytics.ParseAllowedPrefixes(cfg.String("ANALYTICS_ALLOWED_EVENT_PREFIXES", "build."))
	if err := cfg.Err(); err != nil {
		return err
	}

	spiffeSource, err := workloadauth.Source(ctx, spiffeEndpoint)
	if err != nil {
		return fmt.Errorf("analytics spiffe workload source: %w", err)
	}
	defer func() {
		if err := spiffeSource.Close(); err != nil {
			logger.ErrorContext(context.Background(), "analytics-service spiffe source close", "error", err)
		}
	}()
	if _, err := workloadauth.CurrentIDForService(spiffeSource, workloadauth.ServiceAnalytics); err != nil {
		return err
	}

	chConn, err := newClickHouseConn(ctx, spiffeSource, chAddress, chUser, chCACertPath)
	if err != nil {
		return err
	}
	defer func() { _ = chConn.Close() }()

	verifier, err := analytics.NewGitHubOIDCVerifier(ctx, githubOIDCIssuer, oidcAudience, allowedRepositories)
	if err != nil {
		return err
	}
	svc := &analytics.Service{CH: chConn, AllowedPrefixes: allowedPrefixes}
	if err := svc.Ready(ctx); err != nil {
		return err
	}

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
	mux.HandleFunc("/v1/logs", logsHandler(svc, verifier, logger))

	server := httpserver.New(listenAddr, otelhttp.NewHandler(limitRequestBodies(mux, requestBodyLimit), serviceName))
	return httpserver.Run(ctx, logger, server)
}

func logsHandler(svc *analytics.Service, verifier *analytics.GitHubOIDCVerifier, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		source, err := verifier.SourceFromRequest(r.Context(), r)
		if err != nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			logger.WarnContext(r.Context(), "analytics oidc rejected", "error", err)
			return
		}
		body, err := readRequestBody(r, requestBodyLimit)
		if err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		req, err := analytics.DecodeLogsRequest(r.Header.Get("Content-Type"), body)
		if err != nil {
			http.Error(w, "invalid otlp logs request", http.StatusBadRequest)
			logger.WarnContext(r.Context(), "analytics otlp decode rejected", "error", err)
			return
		}
		count, err := svc.IngestLogs(r.Context(), source, req)
		if err != nil {
			http.Error(w, "analytics ingest rejected", http.StatusBadRequest)
			logger.WarnContext(r.Context(), "analytics ingest rejected", "error", err)
			return
		}
		resp, contentType, err := analytics.EncodeLogsResponse(r.Header.Get("Content-Type"))
		if err != nil {
			http.Error(w, "encode otlp response", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", contentType)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(resp)
		logger.InfoContext(r.Context(), "analytics logs ingested", "event_count", count, "repository", source.Repository, "run_id", source.RunID)
	}
}

func readRequestBody(r *http.Request, maxBytes int64) ([]byte, error) {
	var reader io.Reader = r.Body
	if strings.EqualFold(strings.TrimSpace(r.Header.Get("Content-Encoding")), "gzip") {
		gz, err := gzip.NewReader(r.Body)
		if err != nil {
			return nil, err
		}
		defer func() { _ = gz.Close() }()
		reader = gz
	}
	data, err := io.ReadAll(io.LimitReader(reader, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxBytes {
		return nil, fmt.Errorf("request body exceeds %d bytes", maxBytes)
	}
	return data, nil
}

func newClickHouseConn(ctx context.Context, spiffeSource *workloadapi.X509Source, chAddress, chUser, caCertPath string) (clickhouse.Conn, error) {
	chTLSConfig, err := workloadauth.TLSConfigWithX509SourceAndCABundle(ctx, spiffeSource, caCertPath)
	if err != nil {
		return nil, fmt.Errorf("analytics clickhouse tls: %w", err)
	}
	chTLSConfig.MinVersion = tls.VersionTLS12
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

func limitRequestBodies(next http.Handler, maxBytes int64) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			if r.ContentLength > maxBytes {
				http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
				return
			}
			r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
		}
		next.ServeHTTP(w, r)
	})
}
