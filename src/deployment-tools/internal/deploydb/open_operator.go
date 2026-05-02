package deploydb

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/verself/deployment-tools/internal/sshtun"
)

// OpenOperator opens the deploy controller's ClickHouse connection by
// reading the host-rendered operator client configuration over SSH,
// forwarding the native TLS port, and dialing clickhouse-go through
// that local tunnel.
func OpenOperator(ctx context.Context, ssh *sshtun.Client, cfg Config) (*Client, error) {
	if ssh == nil {
		return nil, fmt.Errorf("deploydb: ssh client is required")
	}
	if cfg.Database == "" {
		return nil, fmt.Errorf("deploydb: Database is required")
	}
	if cfg.Username == "" {
		return nil, fmt.Errorf("deploydb: Username is required")
	}
	if cfg.OperatorConfigPath == "" {
		return nil, fmt.Errorf("deploydb: OperatorConfigPath is required")
	}

	tr := tracer()
	ctx, span := tr.Start(ctx, "verself_deploy.clickhouse.open",
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(
			attribute.String("db.system", "clickhouse"),
			attribute.String("db.name", cfg.Database),
			attribute.String("db.user", cfg.Username),
			attribute.String("clickhouse.operator_config_path", cfg.OperatorConfigPath),
		),
	)
	defer span.End()

	rawConfig, err := readRemoteFile(ctx, ssh, cfg.OperatorConfigPath)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, fmt.Errorf("deploydb: read operator config: %w", err)
	}
	operatorCfg, err := parseOperatorConfig(rawConfig)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}
	span.SetAttributes(
		attribute.String("clickhouse.config_host", operatorCfg.Host),
		attribute.Int("clickhouse.native_tls_port", operatorCfg.Port),
	)

	forward, err := ssh.Forward(ctx, "clickhouse-native", operatorCfg.Port)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, fmt.Errorf("deploydb: open native forward: %w", err)
	}
	span.SetAttributes(attribute.String("clickhouse.forward_addr", forward.ListenAddr))

	material, err := readTLSMaterial(ctx, ssh, operatorCfg)
	if err != nil {
		_ = forward.Close()
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}
	tlsConfig, err := buildTLSConfig(material)
	if err != nil {
		_ = forward.Close()
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}

	conn, err := openNative(ctx, forward.ListenAddr, tlsConfig, cfg)
	if err != nil {
		_ = forward.Close()
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, fmt.Errorf("deploydb: open native client: %w", err)
	}
	if err := conn.Ping(ctx); err != nil {
		_ = conn.Close()
		_ = forward.Close()
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, fmt.Errorf("deploydb: ping native client: %w", err)
	}

	span.SetStatus(codes.Ok, "")
	return &Client{conn: conn, forward: forward, tracer: tr}, nil
}

func readRemoteFile(ctx context.Context, ssh *sshtun.Client, path string) ([]byte, error) {
	if path == "" {
		return nil, fmt.Errorf("empty remote path")
	}
	if !strings.HasPrefix(path, "/") {
		return nil, fmt.Errorf("remote path must be absolute: %q", path)
	}
	if strings.ContainsRune(path, 0) {
		return nil, fmt.Errorf("remote path contains NUL")
	}
	return ssh.Exec(ctx, "sudo /bin/cat -- "+strconv.Quote(path))
}
