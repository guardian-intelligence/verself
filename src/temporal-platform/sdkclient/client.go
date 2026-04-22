package sdkclient

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"os"
	"strings"

	workloadauth "github.com/forge-metal/auth-middleware/workload"
	"github.com/forge-metal/temporal-platform/internal/temporallog"
	"github.com/spiffe/go-spiffe/v2/spiffeid"
	"github.com/spiffe/go-spiffe/v2/spiffetls/tlsconfig"
	"github.com/spiffe/go-spiffe/v2/workloadapi"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"go.opentelemetry.io/otel"
	otelpropagation "go.opentelemetry.io/otel/propagation"
	"go.temporal.io/sdk/client"
	sdkotel "go.temporal.io/sdk/contrib/opentelemetry"
	sdkinterceptor "go.temporal.io/sdk/interceptor"
	"google.golang.org/grpc"
)

const DefaultFrontendAddress = "127.0.0.1:7233"

type Config struct {
	HostPort string
	ServerID spiffeid.ID
}

func LoadConfigFromEnv() (Config, error) {
	cfg := Config{
		HostPort: envOr("FM_TEMPORAL_FRONTEND_ADDRESS", DefaultFrontendAddress),
	}
	serverID, err := parseSPIFFEIDEnv("FM_TEMPORAL_SERVER_SPIFFE_ID")
	if err != nil {
		return Config{}, err
	}
	cfg.ServerID = serverID
	return cfg, nil
}

func NewSource(ctx context.Context, socket string) (*workloadapi.X509Source, error) {
	return workloadauth.Source(ctx, socket)
}

func NewNamespaceClient(cfg Config, source *workloadapi.X509Source, tracerName string) (client.NamespaceClient, error) {
	return client.NewNamespaceClient(namespaceClientOptions(cfg, source, tracerName))
}

func NewWorkflowClient(cfg Config, namespace string, source *workloadapi.X509Source, tracerName string) (client.Client, error) {
	return client.Dial(workflowClientOptions(cfg, namespace, source, tracerName))
}

func namespaceClientOptions(cfg Config, source *workloadapi.X509Source, tracerName string) client.Options {
	return baseClientOptions(cfg, "", source, tracerName)
}

func workflowClientOptions(cfg Config, namespace string, source *workloadapi.X509Source, tracerName string) client.Options {
	return baseClientOptions(cfg, namespace, source, tracerName)
}

func baseClientOptions(cfg Config, namespace string, source *workloadapi.X509Source, tracerName string) client.Options {
	return client.Options{
		HostPort:  cfg.HostPort,
		Namespace: namespace,
		Logger:    temporallog.New(slog.Default()),
		ConnectionOptions: client.ConnectionOptions{
			TLS: temporalTLSConfig(source, cfg.ServerID),
			DialOptions: []grpc.DialOption{
				grpc.WithStatsHandler(otelgrpc.NewClientHandler()),
			},
		},
		Interceptors: []sdkinterceptor.ClientInterceptor{mustTracingInterceptor(tracerName)},
	}
}

func temporalTLSConfig(source *workloadapi.X509Source, serverID spiffeid.ID) *tls.Config {
	return tlsconfig.MTLSClientConfig(source, source, tlsconfig.AuthorizeID(serverID))
}

func mustTracingInterceptor(tracerName string) sdkinterceptor.Interceptor {
	name := strings.TrimSpace(tracerName)
	if name == "" {
		name = "temporal-sdk"
	}
	interceptor, err := sdkotel.NewTracingInterceptor(sdkotel.TracerOptions{
		Tracer:            otel.GetTracerProvider().Tracer(name),
		TextMapPropagator: otelpropagation.NewCompositeTextMapPropagator(otelpropagation.TraceContext{}, otelpropagation.Baggage{}),
	})
	if err != nil {
		panic(err)
	}
	return interceptor
}

func parseSPIFFEIDEnv(name string) (spiffeid.ID, error) {
	raw := strings.TrimSpace(envOr(name, ""))
	if raw == "" {
		return spiffeid.ID{}, fmt.Errorf("%s is required", name)
	}
	id, err := spiffeid.FromString(raw)
	if err != nil {
		return spiffeid.ID{}, fmt.Errorf("parse %s: %w", name, err)
	}
	return id, nil
}

func envOr(name, fallback string) string {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback
	}
	return raw
}
