package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"sort"
	"strings"
	"syscall"
	"time"

	workloadauth "github.com/verself/auth-middleware/workload"
	"github.com/verself/envconfig"
	verselfotel "github.com/verself/otel"
	secretsclient "github.com/verself/secrets-service/client"
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

	otelShutdown, _, err := verselfotel.Init(ctx, verselfotel.Config{
		ServiceName:    "object-storage-secret-sync",
		ServiceVersion: version,
	})
	if err != nil {
		return fmt.Errorf("otel init: %w", err)
	}
	defer func() { _ = otelShutdown(context.Background()) }()

	cfg := envconfig.New()
	secretsURL := cfg.RequireURL("OBJECT_STORAGE_SECRET_SYNC_SECRETS_URL")
	spiffeEndpoint := cfg.String(workloadauth.EndpointSocketEnv, "")
	accessKeyIDPath := cfg.String("OBJECT_STORAGE_SECRET_SYNC_PROXY_ACCESS_KEY_ID_PATH", "/etc/credstore/object-storage-service/garage-proxy-access-key-id")
	secretAccessKeyPath := cfg.String("OBJECT_STORAGE_SECRET_SYNC_PROXY_SECRET_ACCESS_KEY_PATH", "/etc/credstore/object-storage-service/garage-proxy-secret-access-key")
	accessKeyID := cfg.RequireFile(accessKeyIDPath)
	secretAccessKey := cfg.RequireFile(secretAccessKeyPath)
	if err := cfg.Err(); err != nil {
		return err
	}

	source, err := workloadauth.Source(ctx, spiffeEndpoint)
	if err != nil {
		return fmt.Errorf("spiffe source: %w", err)
	}
	defer func() { _ = source.Close() }()

	httpClient, err := workloadauth.MTLSClientForService(source, workloadauth.ServiceSecrets, nil)
	if err != nil {
		return fmt.Errorf("secrets mtls: %w", err)
	}
	runtimeClient, err := secretsclient.NewClientWithResponses(secretsURL, secretsclient.WithHTTPClient(httpClient))
	if err != nil {
		return fmt.Errorf("secrets client: %w", err)
	}

	values := map[string]string{
		secretsclient.ObjectStorageGarageProxyAccessKeyIDName:     accessKeyID,
		secretsclient.ObjectStorageGarageProxySecretAccessKeyName: secretAccessKey,
	}

	result, err := syncRuntimeSecrets(ctx, runtimeClient, values)
	if err != nil {
		return err
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(result)
}

func syncRuntimeSecrets(ctx context.Context, client *secretsclient.ClientWithResponses, secretValues map[string]string) (syncResult, error) {
	ctx, span := tracer.Start(ctx, "object_storage.runtime_secret.sync", trace.WithSpanKind(trace.SpanKindClient))
	defer span.End()

	names := make([]string, 0, len(secretValues))
	for name := range secretValues {
		names = append(names, name)
	}
	sort.Strings(names)
	span.SetAttributes(attribute.Int("verself.secret_count", len(names)))

	syncCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	scopeLevel := secretsclient.PutSecretBodyScopeLevelOrg
	for _, name := range names {
		desired := secretValues[name]
		readResp, err := client.ReadSecretWithResponse(syncCtx, name)
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			return syncResult{}, fmt.Errorf("read runtime secret %s: %w", name, err)
		}
		if readResp.JSON200 != nil && readResp.JSON200.Value == desired {
			continue
		}
		if readResp.StatusCode() != http.StatusOK && readResp.StatusCode() != http.StatusNotFound {
			err := fmt.Errorf("read runtime secret %s: unexpected status %d: %s", name, readResp.StatusCode(), strings.TrimSpace(string(readResp.Body)))
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			return syncResult{}, err
		}
		putResp, err := client.PutSecretWithResponse(syncCtx, name, &secretsclient.PutSecretParams{
			IdempotencyKey: runtimeSecretUpsertKey(name, desired),
		}, secretsclient.PutSecretJSONRequestBody{
			ScopeLevel: &scopeLevel,
			Value:      desired,
		})
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			return syncResult{}, fmt.Errorf("write runtime secret %s: %w", name, err)
		}
		if putResp.JSON200 == nil {
			err := fmt.Errorf("write runtime secret %s: unexpected status %d: %s", name, putResp.StatusCode(), strings.TrimSpace(string(putResp.Body)))
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			return syncResult{}, err
		}
	}

	traceID := ""
	if sc := trace.SpanContextFromContext(ctx); sc.HasTraceID() {
		traceID = sc.TraceID().String()
	}
	return syncResult{TraceID: traceID, SecretNames: names}, nil
}

func runtimeSecretUpsertKey(name string, value string) string {
	sum := sha256.Sum256([]byte(name + "\x00" + value))
	return fmt.Sprintf("object-storage-runtime-upsert-%x", sum)
}
