// Package haproxyupstreams reconciles HAProxy map files from Nomad's
// native service catalog.
//
// HAProxy maps `VERSELF_UPSTREAM_<COMP>_<EP>` keys to the current
// allocation listener for every Nomad-supervised upstream. This
// package walks Nomad's catalog, rewrites the map atomically, validates
// the live HAProxy configuration, and reloads HAProxy.
//
// The mapping rule is mechanical: a Nomad service named
// `<jobid>-<endpoint>` becomes the map key
// `VERSELF_UPSTREAM_<UPPER(jobid_with_dashes_to_underscores)_<UPPER(endpoint)>`.
package haproxyupstreams

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/verself/deployment-tools/internal/nomadclient"
	"github.com/verself/deployment-tools/internal/sshtun"
)

const tracerName = "github.com/verself/deployment-tools/internal/haproxyupstreams"

const (
	upstreamsPath        = "/etc/haproxy/maps/upstreams.map"
	haproxyBin           = "/opt/verself/profile/bin/haproxy"
	haproxyConfig        = "/etc/haproxy/haproxy.cfg"
	haproxyLDLibraryPath = "/opt/aws-lc/lib/x86_64-linux-gnu"
)

// Reconcile fetches every Nomad-registered service address, rewrites
// /etc/haproxy/maps/upstreams.map on the controller node, validates
// the HAProxy configuration against that map, and reloads HAProxy.
func Reconcile(ctx context.Context, client *nomadclient.Client, ssh *sshtun.Client) error {
	tracer := otel.Tracer(tracerName)
	ctx, span := tracer.Start(ctx, "verself_deploy.haproxy.reconcile_upstreams",
		trace.WithSpanKind(trace.SpanKindInternal),
	)
	defer span.End()

	addresses, err := client.ListServiceAddresses(ctx)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	body, err := buildMapFile(addresses)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	span.SetAttributes(
		attribute.Int("verself.haproxy.upstream.count", len(addresses)),
		attribute.Int("verself.haproxy.upstream.bytes", len(body)),
	)

	if err := writeAndValidateUpstreamFile(ctx, ssh, body); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	if err := reloadHAProxy(ctx, ssh); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	span.SetStatus(codes.Ok, "")
	return nil
}

// buildMapFile renders the HAProxy upstream map body. It is exported
// via the lowercase function name only for tests in the same package;
// the rest of the package is callable surface for verself-deploy.
func buildMapFile(addresses []nomadclient.ServiceAddress) (string, error) {
	rows := make(map[string]string, len(addresses))
	for _, addr := range addresses {
		key := mapKey(addr.Name)
		if key == "" {
			continue
		}
		if err := validateLoopbackAddress(addr); err != nil {
			return "", err
		}
		rows[key] = addr.Address + ":" + strconv.Itoa(addr.Port)
	}
	keys := make([]string, 0, len(rows))
	for key := range rows {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, key := range keys {
		b.WriteString(key)
		b.WriteByte(' ')
		b.WriteString(rows[key])
		b.WriteByte('\n')
	}
	return b.String(), nil
}

// mapKey converts a Nomad service name (`<jobid>-<endpoint>`) to the
// matching VERSELF_UPSTREAM_* key HAProxy expects. Service
// names outside the authored job naming contract return an empty
// string so the caller skips them.
func mapKey(serviceName string) string {
	if serviceName == "" {
		return ""
	}
	for _, r := range serviceName {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= '0' && r <= '9':
		case r == '-' || r == '_':
		default:
			return ""
		}
	}
	return "VERSELF_UPSTREAM_" + strings.ToUpper(strings.ReplaceAll(serviceName, "-", "_"))
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

func writeAndValidateUpstreamFile(ctx context.Context, ssh *sshtun.Client, body string) error {
	// HAProxy validates the fixed map path, so the command installs a
	// backup first and restores it if config validation fails.
	checkCommand := fmt.Sprintf(
		"cd / && LD_LIBRARY_PATH=%s %s -c -f %s",
		haproxyLDLibraryPath,
		haproxyBin,
		haproxyConfig,
	)
	cmd := fmt.Sprintf(
		"set -eu\n"+
			"tmp=$(sudo mktemp %s.tmp.XXXXXX)\n"+
			"backup=\"\"\n"+
			"trap 'if [ -n \"${tmp:-}\" ] && sudo test -e \"$tmp\"; then sudo rm -f \"$tmp\"; fi' EXIT\n"+
			"if sudo test -e %s; then\n"+
			"  backup=$(sudo mktemp %s.backup.XXXXXX)\n"+
			"  sudo cp -a %s \"$backup\"\n"+
			"fi\n"+
			"sudo tee \"$tmp\" >/dev/null <<'VERSELFHAPROXY_UPSTREAMS_EOF'\n%sVERSELFHAPROXY_UPSTREAMS_EOF\n"+
			"sudo chmod 0640 \"$tmp\"\n"+
			"sudo chown root:haproxy \"$tmp\"\n"+
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
			"fi\n",
		upstreamsPath,
		upstreamsPath,
		upstreamsPath,
		upstreamsPath,
		body,
		upstreamsPath,
		strconv.Quote(checkCommand),
		upstreamsPath,
		upstreamsPath,
	)
	if _, err := ssh.Exec(ctx, cmd); err != nil {
		return fmt.Errorf("write and validate %s: %w", upstreamsPath, err)
	}
	return nil
}

func reloadHAProxy(ctx context.Context, ssh *sshtun.Client) error {
	if _, err := ssh.Exec(ctx, "sudo systemctl reload haproxy"); err != nil {
		return fmt.Errorf("systemctl reload haproxy: %w", err)
	}
	return nil
}
