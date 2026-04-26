package sdkclient

import (
	"context"
	"crypto/tls"
	"log/slog"
	"strings"

	"github.com/spiffe/go-spiffe/v2/spiffetls/tlsconfig"
	"github.com/spiffe/go-spiffe/v2/workloadapi"
	workloadauth "github.com/verself/auth-middleware/workload"
	"github.com/verself/envconfig"
	"github.com/verself/temporal-platform/internal/temporallog"
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
}

func LoadConfigFromEnv() (Config, error) {
	l := envconfig.New()
	cfg := Config{
		HostPort: l.String("VERSELF_TEMPORAL_FRONTEND_ADDRESS", DefaultFrontendAddress),
	}
	if err := l.Err(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func NewSource(ctx context.Context, socket string) (*workloadapi.X509Source, error) {
	return workloadauth.Source(ctx, socket)
}

func NewNamespaceClient(cfg Config, source *workloadapi.X509Source, tracerName string) (client.NamespaceClient, error) {
	options, err := namespaceClientOptions(cfg, source, tracerName)
	if err != nil {
		return nil, err
	}
	return client.NewNamespaceClient(options)
}

func NewWorkflowClient(cfg Config, namespace string, source *workloadapi.X509Source, tracerName string) (client.Client, error) {
	options, err := workflowClientOptions(cfg, namespace, source, tracerName)
	if err != nil {
		return nil, err
	}
	return client.Dial(options)
}

func namespaceClientOptions(cfg Config, source *workloadapi.X509Source, tracerName string) (client.Options, error) {
	return baseClientOptions(cfg, "", source, tracerName)
}

func workflowClientOptions(cfg Config, namespace string, source *workloadapi.X509Source, tracerName string) (client.Options, error) {
	return baseClientOptions(cfg, namespace, source, tracerName)
}

func baseClientOptions(cfg Config, namespace string, source *workloadapi.X509Source, tracerName string) (client.Options, error) {
	tlsCfg, err := temporalTLSConfig(source)
	if err != nil {
		return client.Options{}, err
	}
	return client.Options{
		HostPort:  cfg.HostPort,
		Namespace: namespace,
		Logger:    temporallog.New(slog.Default()),
		ConnectionOptions: client.ConnectionOptions{
			TLS: tlsCfg,
			DialOptions: []grpc.DialOption{
				grpc.WithStatsHandler(otelgrpc.NewClientHandler()),
			},
		},
		Interceptors: []sdkinterceptor.ClientInterceptor{mustTracingInterceptor(tracerName)},
	}, nil
}

func temporalTLSConfig(source *workloadapi.X509Source) (*tls.Config, error) {
	serverID, err := workloadauth.PeerIDForSource(source, workloadauth.ServiceTemporalServer)
	if err != nil {
		return nil, err
	}
	return tlsconfig.MTLSClientConfig(source, source, tlsconfig.AuthorizeID(serverID)), nil
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
