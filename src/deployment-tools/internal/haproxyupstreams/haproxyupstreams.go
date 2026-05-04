// Package haproxyupstreams reconciles HAProxy's Nomad-backed backend
// configuration from Nomad's native service catalog.
package haproxyupstreams

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/verself/deployment-tools/internal/nomadclient"
	"github.com/verself/deployment-tools/internal/sshtun"
	edgecontract "github.com/verself/host-configuration/edgecontract"
)

const tracerName = "github.com/verself/deployment-tools/internal/haproxyupstreams"

const (
	nomadUpstreamsPath   = "/etc/haproxy/nomad-upstreams.cfg"
	haproxyBin           = "/opt/verself/profile/bin/haproxy"
	haproxyConfig        = "/etc/haproxy/haproxy.cfg"
	haproxyLDLibraryPath = "/opt/aws-lc/lib/x86_64-linux-gnu"
)

type Options struct {
	RepoRoot string
	Site     string
}

type Result struct {
	Changed       bool
	EndpointCount int
	ConfigBytes   int
}

// Reconcile fetches Nomad service registrations, renders every HAProxy
// backend that targets a Nomad upstream, validates the complete HAProxy
// configuration, and reloads HAProxy only when the rendered backend file
// changes.
func Reconcile(ctx context.Context, client *nomadclient.Client, ssh *sshtun.Client, opts Options) (Result, error) {
	tracer := otel.Tracer(tracerName)
	ctx, span := tracer.Start(ctx, "verself_deploy.haproxy.reconcile_upstreams",
		trace.WithSpanKind(trace.SpanKindInternal),
	)
	defer span.End()

	body, endpointCount, err := buildConfig(ctx, client, opts)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return Result{}, err
	}
	span.SetAttributes(
		attribute.Int("verself.haproxy.upstream.endpoint_count", endpointCount),
		attribute.Int("verself.haproxy.upstream.bytes", len(body)),
	)

	changed, err := writeAndValidateUpstreamConfig(ctx, ssh, body)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return Result{}, err
	}
	if changed {
		if err := reloadHAProxy(ctx, ssh); err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			return Result{}, err
		}
	}
	span.SetAttributes(attribute.Bool("verself.haproxy.upstream.changed", changed))
	span.SetStatus(codes.Ok, "")
	return Result{Changed: changed, EndpointCount: endpointCount, ConfigBytes: len(body)}, nil
}

// Stage writes the Nomad backend config without validating or reloading
// HAProxy. The deploy controller uses it immediately before host
// configuration convergence, where the currently running HAProxy config may
// still define the old dynamic backends inline; Ansible validates the staged
// file together with the newly rendered static config before restart.
func Stage(ctx context.Context, client *nomadclient.Client, ssh *sshtun.Client, opts Options) (Result, error) {
	tracer := otel.Tracer(tracerName)
	ctx, span := tracer.Start(ctx, "verself_deploy.haproxy.stage_upstreams",
		trace.WithSpanKind(trace.SpanKindInternal),
	)
	defer span.End()

	body, endpointCount, err := buildConfig(ctx, client, opts)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return Result{}, err
	}
	changed, err := writeUpstreamConfig(ctx, ssh, body)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return Result{}, err
	}
	span.SetAttributes(
		attribute.Int("verself.haproxy.upstream.endpoint_count", endpointCount),
		attribute.Int("verself.haproxy.upstream.bytes", len(body)),
		attribute.Bool("verself.haproxy.upstream.changed", changed),
	)
	span.SetStatus(codes.Ok, "")
	return Result{Changed: changed, EndpointCount: endpointCount, ConfigBytes: len(body)}, nil
}

func buildConfig(ctx context.Context, client *nomadclient.Client, opts Options) (string, int, error) {
	if opts.RepoRoot == "" {
		return "", 0, fmt.Errorf("haproxy upstream reconcile requires RepoRoot")
	}
	if opts.Site == "" {
		return "", 0, fmt.Errorf("haproxy upstream reconcile requires Site")
	}
	bundle, err := edgecontract.Build(edgecontract.Config{RepoRoot: opts.RepoRoot, Site: opts.Site})
	if err != nil {
		return "", 0, err
	}
	if len(bundle.Issues) > 0 {
		return "", 0, fmt.Errorf("edge contract has issues: %s", strings.Join(bundle.Issues, "; "))
	}

	addresses, err := client.ListServiceAddresses(ctx)
	if err != nil {
		return "", 0, err
	}
	endpoints := make([]edgecontract.NomadEndpoint, 0, len(addresses))
	for _, addr := range addresses {
		if err := validateLoopbackAddress(addr); err != nil {
			return "", 0, err
		}
		endpoints = append(endpoints, edgecontract.NomadEndpoint{
			ServiceName: addr.Name,
			ServiceID:   addr.ServiceID,
			AllocID:     addr.AllocID,
			JobID:       addr.JobID,
			Address:     addr.Address,
			Port:        addr.Port,
		})
	}
	return edgecontract.RenderNomadUpstreamsConfig(bundle.Plan, endpoints), len(endpoints), nil
}

func validateLoopbackAddress(addr nomadclient.ServiceAddress) error {
	if addr.Address != "127.0.0.1" {
		return fmt.Errorf("service %q advertised non-loopback address %q", addr.Name, addr.Address)
	}
	if addr.Port <= 0 || addr.Port > 65535 {
		return fmt.Errorf("service %q advertised invalid port %d", addr.Name, addr.Port)
	}
	return nil
}

func writeUpstreamConfig(ctx context.Context, ssh *sshtun.Client, body string) (bool, error) {
	cmd := fmt.Sprintf(
		"set -eu\n"+
			"tmp=$(sudo mktemp %s.tmp.XXXXXX)\n"+
			"trap 'if [ -n \"${tmp:-}\" ] && sudo test -e \"$tmp\"; then sudo rm -f \"$tmp\"; fi' EXIT\n"+
			"sudo tee \"$tmp\" >/dev/null <<'VERSELFHAPROXY_NOMAD_UPSTREAMS_EOF'\n%sVERSELFHAPROXY_NOMAD_UPSTREAMS_EOF\n"+
			"sudo chmod 0640 \"$tmp\"\n"+
			"sudo chown root:haproxy \"$tmp\"\n"+
			"if sudo test -e %s && sudo cmp -s \"$tmp\" %s; then\n"+
			"  sudo rm -f \"$tmp\"\n"+
			"  echo VERSELF_HAPROXY_STATUS=unchanged\n"+
			"  exit 0\n"+
			"fi\n"+
			"sudo mv \"$tmp\" %s\n"+
			"echo VERSELF_HAPROXY_STATUS=changed\n",
		nomadUpstreamsPath,
		body,
		nomadUpstreamsPath,
		nomadUpstreamsPath,
		nomadUpstreamsPath,
	)
	out, err := ssh.Exec(ctx, cmd)
	if err != nil {
		return false, fmt.Errorf("write %s: %w", nomadUpstreamsPath, err)
	}
	return strings.Contains(string(out), "VERSELF_HAPROXY_STATUS=changed"), nil
}

func writeAndValidateUpstreamConfig(ctx context.Context, ssh *sshtun.Client, body string) (bool, error) {
	checkCommand := fmt.Sprintf(
		"cd / && LD_LIBRARY_PATH=%s %s -c -f %s -f %s",
		haproxyLDLibraryPath,
		haproxyBin,
		haproxyConfig,
		nomadUpstreamsPath,
	)
	cmd := fmt.Sprintf(
		"set -eu\n"+
			"tmp=$(sudo mktemp %s.tmp.XXXXXX)\n"+
			"backup=\"\"\n"+
			"trap 'if [ -n \"${tmp:-}\" ] && sudo test -e \"$tmp\"; then sudo rm -f \"$tmp\"; fi' EXIT\n"+
			"sudo tee \"$tmp\" >/dev/null <<'VERSELFHAPROXY_NOMAD_UPSTREAMS_EOF'\n%sVERSELFHAPROXY_NOMAD_UPSTREAMS_EOF\n"+
			"sudo chmod 0640 \"$tmp\"\n"+
			"sudo chown root:haproxy \"$tmp\"\n"+
			"if sudo test -e %s && sudo cmp -s \"$tmp\" %s; then\n"+
			"  sudo rm -f \"$tmp\"\n"+
			"  echo VERSELF_HAPROXY_STATUS=unchanged\n"+
			"  exit 0\n"+
			"fi\n"+
			"if sudo test -e %s; then\n"+
			"  backup=$(sudo mktemp %s.backup.XXXXXX)\n"+
			"  sudo cp -a %s \"$backup\"\n"+
			"fi\n"+
			"sudo mv \"$tmp\" %s\n"+
			// `haproxy -c` opens shm-stats-file, so validate as the service user that must reload it.
			"if ! sudo -u haproxy /bin/sh -c %s; then\n"+
			"  if [ -n \"$backup\" ]; then\n"+
			"    sudo mv \"$backup\" %s\n"+
			"  else\n"+
			"    sudo rm -f %s\n"+
			"  fi\n"+
			"  exit 1\n"+
			"fi\n"+
			"if [ -n \"$backup\" ]; then\n"+
			"  sudo rm -f \"$backup\"\n"+
			"fi\n"+
			"echo VERSELF_HAPROXY_STATUS=changed\n",
		nomadUpstreamsPath,
		body,
		nomadUpstreamsPath,
		nomadUpstreamsPath,
		nomadUpstreamsPath,
		nomadUpstreamsPath,
		nomadUpstreamsPath,
		nomadUpstreamsPath,
		strconv.Quote(checkCommand),
		nomadUpstreamsPath,
		nomadUpstreamsPath,
	)
	out, err := ssh.Exec(ctx, cmd)
	if err != nil {
		return false, fmt.Errorf("write and validate %s: %w", nomadUpstreamsPath, err)
	}
	return strings.Contains(string(out), "VERSELF_HAPROXY_STATUS=changed"), nil
}

func reloadHAProxy(ctx context.Context, ssh *sshtun.Client) error {
	if _, err := ssh.Exec(ctx, "sudo systemctl reload haproxy"); err != nil {
		return fmt.Errorf("systemctl reload haproxy: %w", err)
	}
	return nil
}
