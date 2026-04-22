package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"sort"
	"strings"
	"syscall"
	"time"

	workloadauth "github.com/forge-metal/auth-middleware/workload"
	fmotel "github.com/forge-metal/otel"
	secretsclient "github.com/forge-metal/secrets-service/client"
	"github.com/spiffe/go-spiffe/v2/spiffeid"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

var version = "dev"

var tracer = otel.Tracer("object-storage-secret-sync")

type syncResult struct {
	TraceID     string   `json:"trace_id"`
	SecretNames []string `json:"secret_names"`
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	otelShutdown, _, err := fmotel.Init(ctx, fmotel.Config{
		ServiceName:    "object-storage-secret-sync",
		ServiceVersion: version,
	})
	if err != nil {
		return fmt.Errorf("otel init: %w", err)
	}
	defer func() { _ = otelShutdown(context.Background()) }()

	secretsURL := requireEnv("OBJECT_STORAGE_SECRET_SYNC_SECRETS_URL")
	secretsSPIFFEID, err := parseSPIFFEID(requireEnv("OBJECT_STORAGE_SECRET_SYNC_SECRETS_SPIFFE_ID"))
	if err != nil {
		return err
	}
	source, err := workloadauth.Source(ctx, envOr(workloadauth.EndpointSocketEnv, ""))
	if err != nil {
		return fmt.Errorf("spiffe source: %w", err)
	}
	defer func() { _ = source.Close() }()

	httpClient, err := workloadauth.MTLSClient(source, secretsSPIFFEID, http.DefaultTransport)
	if err != nil {
		return fmt.Errorf("secrets mTLS client: %w", err)
	}
	runtimeClient, err := secretsclient.New(secretsURL, secretsclient.WithHTTPClient(httpClient))
	if err != nil {
		return fmt.Errorf("secrets client: %w", err)
	}

	values := map[string]string{
		secretsclient.ObjectStorageGarageProxyAccessKeyIDName:     requireFile(envOr("OBJECT_STORAGE_SECRET_SYNC_PROXY_ACCESS_KEY_ID_PATH", "/etc/credstore/object-storage-service/garage-proxy-access-key-id")),
		secretsclient.ObjectStorageGarageProxySecretAccessKeyName: requireFile(envOr("OBJECT_STORAGE_SECRET_SYNC_PROXY_SECRET_ACCESS_KEY_PATH", "/etc/credstore/object-storage-service/garage-proxy-secret-access-key")),
	}

	result, err := syncRuntimeSecrets(ctx, runtimeClient, values)
	if err != nil {
		return err
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(result)
}

func syncRuntimeSecrets(ctx context.Context, client *secretsclient.RuntimeSecretClient, secretValues map[string]string) (syncResult, error) {
	ctx, span := tracer.Start(ctx, "object_storage.runtime_secret.sync", trace.WithSpanKind(trace.SpanKindClient))
	defer span.End()

	names := make([]string, 0, len(secretValues))
	for name := range secretValues {
		names = append(names, name)
	}
	sort.Strings(names)
	span.SetAttributes(attribute.Int("forge_metal.secret_count", len(names)))

	syncCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := client.UpsertPlatformRuntimeSecrets(syncCtx, secretValues); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return syncResult{}, fmt.Errorf("sync object-storage runtime secrets: %w", err)
	}

	traceID := ""
	if sc := trace.SpanContextFromContext(ctx); sc.HasTraceID() {
		traceID = sc.TraceID().String()
	}
	return syncResult{TraceID: traceID, SecretNames: names}, nil
}

func parseSPIFFEID(raw string) (spiffeid.ID, error) {
	return workloadauth.ParseID(strings.TrimSpace(raw))
}

func requireEnv(name string) string {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		panic(name + " is required")
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

func requireFile(path string) string {
	data, err := os.ReadFile(strings.TrimSpace(path))
	if err != nil {
		panic(fmt.Sprintf("read %s: %v", path, err))
	}
	return strings.TrimSpace(string(data))
}
