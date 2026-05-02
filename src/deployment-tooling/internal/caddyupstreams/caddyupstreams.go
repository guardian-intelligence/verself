// Package caddyupstreams reconciles /etc/caddy/upstreams.env from
// Nomad's native service catalog.
//
// The Caddyfile expands `{$VERSELF_UPSTREAM_<COMP>_<EP>}` for every
// Nomad-supervised upstream; this package walks Nomad's catalog,
// rewrites the env file, and restarts Caddy so its config-time env
// substitution picks up the new ports.
//
// The mapping rule is mechanical: a Nomad service named
// `<jobid>-<endpoint>` becomes the env var
// `VERSELF_UPSTREAM_<UPPER(jobid_with_dashes_to_underscores)_<UPPER(endpoint)>`.
package caddyupstreams

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/verself/deployment-tooling/internal/nomadclient"
	"github.com/verself/deployment-tooling/internal/sshtun"
)

const tracerName = "github.com/verself/deployment-tooling/internal/caddyupstreams"

const upstreamsPath = "/etc/caddy/upstreams.env"

// Reconcile fetches every Nomad-registered service address, rewrites
// /etc/caddy/upstreams.env on the controller node, and restarts the
// caddy unit so the new env values reach Caddy's parser.
//
// Restart is intentional rather than reload: systemd's
// EnvironmentFile= directive is read on (re)start, not on signal-
// triggered reload, so the change-mode signal Caddy supports for
// admin-API config swaps would not see the new env. A restart is
// ~100ms; Cloudflare retries any in-flight request for routes
// proxied through Caddy.
func Reconcile(ctx context.Context, client *nomadclient.Client, ssh *sshtun.Client) error {
	tracer := otel.Tracer(tracerName)
	ctx, span := tracer.Start(ctx, "verself_deploy.caddy.reconcile_upstreams",
		trace.WithSpanKind(trace.SpanKindInternal),
	)
	defer span.End()

	addresses, err := client.ListServiceAddresses(ctx)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	body := buildEnvFile(addresses)
	span.SetAttributes(
		attribute.Int("verself.caddy.upstream.count", len(addresses)),
		attribute.Int("verself.caddy.upstream.bytes", len(body)),
	)

	if err := writeUpstreamFile(ctx, ssh, body); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	if err := restartCaddy(ctx, ssh); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	span.SetStatus(codes.Ok, "")
	return nil
}

// buildEnvFile renders the upstreams.env body. It is exported via the
// lowercase function name only for tests in the same package; the
// rest of the package is callable surface for verself-deploy.
func buildEnvFile(addresses []nomadclient.ServiceAddress) string {
	rows := make(map[string]string, len(addresses))
	for _, addr := range addresses {
		envVar := envVarName(addr.Name)
		if envVar == "" {
			continue
		}
		rows[envVar] = fmt.Sprintf("%s:%d", addr.Address, addr.Port)
	}
	keys := make([]string, 0, len(rows))
	for key := range rows {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, key := range keys {
		b.WriteString(key)
		b.WriteByte('=')
		b.WriteString(rows[key])
		b.WriteByte('\n')
	}
	return b.String()
}

// envVarName converts a Nomad service name (`<jobid>-<endpoint>`) to
// the matching VERSELF_UPSTREAM_* env var Caddy expects. Service
// names outside the authored job naming contract return an empty
// string so the caller skips them.
func envVarName(serviceName string) string {
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

func writeUpstreamFile(ctx context.Context, ssh *sshtun.Client, body string) error {
	// Heredoc keeps the quoted content out of the shell's word
	// expansion path entirely. The leading `'EOF'` (single-quoted
	// terminator) tells bash not to interpolate $-variables in the
	// body. This file is plain `KEY=VALUE` pairs, so the safety has no
	// functional cost.
	cmd := fmt.Sprintf(
		"sudo tee %s >/dev/null <<'VERSELFCADDY_UPSTREAMS_EOF'\n%sVERSELFCADDY_UPSTREAMS_EOF\n"+
			"sudo chmod 0640 %s && sudo chown root:caddy %s",
		upstreamsPath, body, upstreamsPath, upstreamsPath,
	)
	if _, err := ssh.Exec(ctx, cmd); err != nil {
		return fmt.Errorf("write %s: %w", upstreamsPath, err)
	}
	return nil
}

func restartCaddy(ctx context.Context, ssh *sshtun.Client) error {
	if _, err := ssh.Exec(ctx, "sudo systemctl restart caddy"); err != nil {
		return fmt.Errorf("systemctl restart caddy: %w", err)
	}
	return nil
}
