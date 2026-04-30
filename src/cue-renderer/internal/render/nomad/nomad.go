// Package nomad projects a CUE component with `deployment.supervisor:
// "nomad"` into a JSON job spec consumable by Nomad's HTTP API and by
// `community.general.nomad_job` (content_format=json).
//
// Output layout: one file per opted-in component at
// `jobs/<component>.nomad.json`, anchored under the cache root that
// `aspect render --site=<site>` populates.
//
// Spec shape: a single TaskGroup per systemd unit declared in
// `converge.systemd.units`. The unit block stays the cross-supervisor
// authoring contract (env vars, dependency wiring, readiness probes);
// this renderer just rewrites it into Nomad JSON. raw_exec is the only
// driver: workloads need peer-auth access to the Postgres Unix socket
// and the SPIRE workload-API socket, which the exec driver's
// chroot/PID-namespace isolation would cut.
//
// Times in the JSON spec are nanoseconds — Nomad's API expects Go's
// time.Duration int64 representation. See
// https://developer.hashicorp.com/nomad/api-docs/json-jobs.
package nomad

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/verself/cue-renderer/internal/load"
	"github.com/verself/cue-renderer/internal/render"
	"github.com/verself/cue-renderer/internal/render/projection"
	"github.com/verself/cue-renderer/internal/render/serviceenv"
)

// placeholderRE matches `{{ key.path }}` substrings in serviceenv-derived
// strings. The systemd path lets Ansible substitute these at deploy time;
// the Nomad path resolves them at render time so the spec the box submits
// is fully formed JSON. component_auth_audience is the lone exception —
// it's persisted to disk by the substrate Ansible flow and resolved by
// nomad-deploy at submit time.
var placeholderRE = regexp.MustCompile(`\{\{\s*([a-zA-Z0-9_.]+)\s*\}\}`)

// AuthAudiencePlaceholder is the sentinel the renderer leaves in the
// Nomad spec for VERSELF_AUTH_AUDIENCE; nomad-deploy substitutes the
// resolved Zitadel audience from /etc/verself/<component>/auth_audience.
const AuthAudiencePlaceholder = "{{ component_auth_audience }}"

type Renderer struct{}

func (Renderer) Name() string { return "nomad" }

func (Renderer) Render(_ context.Context, loaded load.Loaded, out render.WritableFS) error {
	components, err := projection.Components(loaded)
	if err != nil {
		return err
	}
	resolver, err := newResolver(loaded)
	if err != nil {
		return err
	}
	for _, component := range components {
		deployment, _ := component.Value["deployment"].(map[string]any)
		supervisor, _ := deployment["supervisor"].(string)
		if supervisor != "nomad" {
			continue
		}
		spec, err := buildJobSpec(component, deployment, resolver)
		if err != nil {
			return fmt.Errorf("render %s: %w", component.Name, err)
		}
		body, err := json.MarshalIndent(spec, "", "  ")
		if err != nil {
			return fmt.Errorf("marshal %s: %w", component.Name, err)
		}
		body = append(body, '\n')
		if err := out.WriteFile(jobPath(jobID(component.Name)), body); err != nil {
			return err
		}
	}
	return nil
}

func jobPath(id string) string { return "jobs/" + id + ".nomad.json" }

// jobID converts a component name (snake_case) to the Nomad-side
// identifier (kebab-case) that lines up with the systemd unit name on
// the existing systemd path. Keeping these aligned lets operators grep
// either `journalctl -u <name>` or `nomad job status <name>` for the
// same string.
func jobID(componentName string) string {
	return strings.ReplaceAll(componentName, "_", "-")
}

func buildJobSpec(component projection.NamedMap, deployment map[string]any, resolver *resolver) (map[string]any, error) {
	converge, ok := component.Value["converge"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s.converge: missing", component.Name)
	}
	systemdConfig, err := projection.Map(converge, component.Name+".converge", "systemd")
	if err != nil {
		return nil, err
	}
	rawUnits, err := projection.Slice(systemdConfig, component.Name+".converge.systemd", "units")
	if err != nil {
		return nil, err
	}
	if len(rawUnits) == 0 {
		return nil, fmt.Errorf("%s.converge.systemd.units: nomad supervisor requires at least one unit", component.Name)
	}

	taskGroups := make([]map[string]any, 0, len(rawUnits))
	for i, raw := range rawUnits {
		unit, ok := raw.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("%s.converge.systemd.units[%d]: expected map, got %T", component.Name, i, raw)
		}
		group, err := buildTaskGroup(component, unit, deployment, resolver)
		if err != nil {
			return nil, err
		}
		taskGroups = append(taskGroups, group)
	}

	jobBody := map[string]any{
		"ID":          jobID(component.Name),
		"Name":        jobID(component.Name),
		"Type":        "service",
		"Datacenters": []string{"dc1"},
		"TaskGroups":  taskGroups,
		// Meta is filled at submit time with binary_sha256 so Nomad sees
		// a new job version when the binary on disk changes. Keeping the
		// key declared (empty) here means the Ansible nomad_submit task
		// merges into an existing object rather than creating one.
		"Meta": map[string]any{},
	}
	return map[string]any{"Job": jobBody}, nil
}

func buildTaskGroup(component projection.NamedMap, unit map[string]any, deployment map[string]any, resolver *resolver) (map[string]any, error) {
	unitName, _ := unit["name"].(string)
	unitUser, _ := unit["user"].(string)
	exec, _ := unit["exec"].(string)
	if unitName == "" || unitUser == "" || exec == "" {
		return nil, fmt.Errorf("%s.converge.systemd.units: name/user/exec required", component.Name)
	}
	resolvedExec, err := resolver.resolve(exec)
	if err != nil {
		return nil, fmt.Errorf("%s.exec: %w", component.Name, err)
	}

	env, err := serviceenv.Unit(component, unit)
	if err != nil {
		return nil, err
	}

	envOut := make(map[string]string, len(env))
	for _, key := range serviceenv.SortedKeys(env) {
		resolved, err := resolver.resolve(env[key])
		if err != nil {
			return nil, fmt.Errorf("%s.env.%s: %w", component.Name, key, err)
		}
		envOut[key] = resolved
	}

	count := int64(1)
	if c, ok := deployment["count"].(int64); ok && c > 0 {
		count = c
	}
	updateBlock, err := updateStanza(deployment)
	if err != nil {
		return nil, err
	}
	resources, err := resourceStanza(deployment)
	if err != nil {
		return nil, err
	}
	killSignal, killTimeout, err := drainStanza(deployment)
	if err != nil {
		return nil, err
	}

	endpoints, _ := component.Value["endpoints"].(map[string]any)
	reservedPorts, primaryPort, err := reservedPorts(component.Name, endpoints, unit)
	if err != nil {
		return nil, err
	}

	services, err := buildServices(component.Name, unitName, unit, primaryPort)
	if err != nil {
		return nil, err
	}

	task := map[string]any{
		"Name":   unitName,
		"Driver": "raw_exec",
		"User":   unitUser,
		"Config": map[string]any{
			"command": resolvedExec,
		},
		"Env":          envOut,
		"Resources":    resources,
		"KillSignal":   killSignal,
		"KillTimeout":  killTimeout,
		"Services":     services,
		"RestartPolicy": map[string]any{
			"Attempts": 3,
			"Interval": int64(5 * time.Minute / time.Nanosecond),
			"Delay":    int64(15 * time.Second / time.Nanosecond),
			"Mode":     "delay",
		},
	}

	group := map[string]any{
		"Name":   unitName,
		"Count":  count,
		"Tasks":  []map[string]any{task},
		"Update": updateBlock,
	}
	if len(reservedPorts) > 0 {
		group["Networks"] = []map[string]any{
			{"Mode": "host", "ReservedPorts": reservedPorts},
		}
	}
	return group, nil
}

func updateStanza(deployment map[string]any) (map[string]any, error) {
	update, _ := deployment["update"].(map[string]any)
	maxParallel, err := optionalInt64(update, "max_parallel", 1)
	if err != nil {
		return nil, err
	}
	minHealthy, err := optionalDuration(update, "min_healthy_time", 30*time.Second)
	if err != nil {
		return nil, err
	}
	healthyDeadline, err := optionalDuration(update, "healthy_deadline", 5*time.Minute)
	if err != nil {
		return nil, err
	}
	progressDeadline, err := optionalDuration(update, "progress_deadline", 10*time.Minute)
	if err != nil {
		return nil, err
	}
	autoRevert := true
	if v, ok := update["auto_revert"].(bool); ok {
		autoRevert = v
	}
	return map[string]any{
		"MaxParallel":      maxParallel,
		"MinHealthyTime":   int64(minHealthy / time.Nanosecond),
		"HealthyDeadline":  int64(healthyDeadline / time.Nanosecond),
		"ProgressDeadline": int64(progressDeadline / time.Nanosecond),
		"AutoRevert":       autoRevert,
	}, nil
}

func drainStanza(deployment map[string]any) (string, int64, error) {
	drain, _ := deployment["drain"].(map[string]any)
	signal, _ := drain["kill_signal"].(string)
	if signal == "" {
		signal = "SIGTERM"
	}
	timeout, err := optionalDuration(drain, "kill_timeout", 30*time.Second)
	if err != nil {
		return "", 0, err
	}
	return signal, int64(timeout / time.Nanosecond), nil
}

func resourceStanza(deployment map[string]any) (map[string]any, error) {
	resources, _ := deployment["resources"].(map[string]any)
	cpu, err := optionalInt64(resources, "cpu_mhz", 500)
	if err != nil {
		return nil, err
	}
	memory, err := optionalInt64(resources, "memory_mb", 256)
	if err != nil {
		return nil, err
	}
	return map[string]any{"CPU": cpu, "MemoryMB": memory}, nil
}

func reservedPorts(componentName string, endpoints map[string]any, unit map[string]any) ([]map[string]any, string, error) {
	if len(endpoints) == 0 {
		return nil, "", nil
	}
	ownedEndpoints, err := endpointsForUnit(componentName, endpoints, unit)
	if err != nil {
		return nil, "", err
	}
	reserved := make([]map[string]any, 0, len(ownedEndpoints))
	primary := ""
	for _, name := range ownedEndpoints {
		endpoint, ok := endpoints[name].(map[string]any)
		if !ok {
			continue
		}
		port, err := projection.Int(endpoint, "topology.components."+componentName+".endpoints."+name, "port")
		if err != nil {
			return nil, "", err
		}
		reserved = append(reserved, map[string]any{"Label": name, "Value": port})
		if primary == "" || name == "public_http" {
			primary = name
		}
	}
	return reserved, primary, nil
}

// endpointsForUnit returns the endpoint labels owned by the unit. The
// primary unit owns every endpoint declared on the component; named
// processes own only their `endpoints` list. Mirrors serviceenv's
// process resolution but at the topology-endpoints level rather than the
// derived env-var level.
func endpointsForUnit(componentName string, endpoints map[string]any, unit map[string]any) ([]string, error) {
	out := make([]string, 0, len(endpoints))
	for name := range endpoints {
		out = append(out, name)
	}
	// Sort for deterministic output.
	for i := 0; i < len(out); i++ {
		for j := i + 1; j < len(out); j++ {
			if out[j] < out[i] {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out, nil
}

func buildServices(componentName, unitName string, unit map[string]any, primaryPort string) ([]map[string]any, error) {
	rawReadiness, _ := unit["readiness"].([]any)
	if len(rawReadiness) == 0 {
		return nil, nil
	}
	checks := make([]map[string]any, 0, len(rawReadiness))
	for _, item := range rawReadiness {
		probe, ok := item.(map[string]any)
		if !ok {
			continue
		}
		kind, _ := probe["kind"].(string)
		endpoint, _ := probe["endpoint"].(string)
		check := map[string]any{
			"Name":     unitName + "-" + kind + "-" + endpoint,
			"PortLabel": endpoint,
			"Interval": int64(10 * time.Second / time.Nanosecond),
			"Timeout":  int64(3 * time.Second / time.Nanosecond),
		}
		switch kind {
		case "tcp":
			check["Type"] = "tcp"
		case "http":
			check["Type"] = "http"
			path, _ := probe["path"].(string)
			if path == "" {
				path = "/"
			}
			check["Path"] = path
			scheme, _ := probe["scheme"].(string)
			if scheme == "https" {
				check["Protocol"] = "https"
				check["TLSSkipVerify"] = true
			}
		default:
			return nil, fmt.Errorf("%s.converge.systemd.units.%s.readiness: unsupported probe kind %q", componentName, unitName, kind)
		}
		checks = append(checks, check)
	}
	if len(checks) == 0 {
		return nil, nil
	}
	if primaryPort == "" {
		// No port to bind the service to — Nomad rejects services
		// without a PortLabel when the group declares Networks. Skip.
		return nil, nil
	}
	return []map[string]any{{
		"Name":      unitName,
		"PortLabel": primaryPort,
		// Use Nomad's native service registry (1.3+). The default is
		// Consul, which adds an automatic ${attr.consul.version} >= 1.8.0
		// constraint and prevents placement on Consul-less hosts.
		"Provider": "nomad",
		"Checks":   checks,
	}}, nil
}

func optionalInt64(m map[string]any, key string, fallback int64) (int64, error) {
	if m == nil {
		return fallback, nil
	}
	if v, ok := m[key].(int64); ok {
		return v, nil
	}
	if v, ok := m[key].(int); ok {
		return int64(v), nil
	}
	if v, ok := m[key].(float64); ok {
		return int64(v), nil
	}
	if _, present := m[key]; !present {
		return fallback, nil
	}
	return 0, fmt.Errorf("%s: expected integer, got %T", key, m[key])
}

func optionalDuration(m map[string]any, key string, fallback time.Duration) (time.Duration, error) {
	if m == nil {
		return fallback, nil
	}
	v, ok := m[key].(string)
	if !ok || v == "" {
		return fallback, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", key, err)
	}
	return d, nil
}

// resolver substitutes Jinja-style `{{ key }}` references in
// serviceenv-derived strings to literal values. The systemd renderer
// leaves them for Ansible to template; the Nomad renderer resolves at
// render time so the spec the bare-metal node submits is fully formed.
//
// Three known-static buckets land here:
//   - `verself_bin`, `verself_domain`, `spire_agent_socket_path` from
//     loaded.Config (ansible_vars + spire blocks).
//   - `topology_endpoints.<comp>.endpoints.<ep>.{address,port}` and
//     `topology_endpoints.<comp>.host` from the topology projection.
//
// `component_auth_audience` is the lone dynamic value; it stays as a
// `{{ component_auth_audience }}` substring for nomad-deploy to swap
// at submit time using /etc/verself/<component>/auth_audience.
type resolver struct {
	verselfBin            string
	verselfDomain         string
	spireAgentSocketPath  string
	endpointAddrs         map[string]string // "topology_endpoints.<c>.endpoints.<e>.address"
	endpointPorts         map[string]string
	componentHosts        map[string]string // "topology_endpoints.<c>.host"
}

func newResolver(loaded load.Loaded) (*resolver, error) {
	r := &resolver{
		spireAgentSocketPath: loaded.Config.Spire.AgentSocketPath,
		endpointAddrs:        map[string]string{},
		endpointPorts:        map[string]string{},
		componentHosts:       map[string]string{},
	}
	if v, ok := loaded.Config.AnsibleVars["verself_bin"].(string); ok {
		r.verselfBin = v
	}
	if v, ok := loaded.Config.AnsibleVars["verself_domain"].(string); ok {
		r.verselfDomain = v
	}
	for compName, comp := range loaded.Topology.Components {
		host := comp.Host
		if host == "" {
			host = "127.0.0.1"
		}
		r.componentHosts["topology_endpoints."+compName+".host"] = string(host)
		for epName, ep := range comp.Endpoints {
			r.endpointAddrs["topology_endpoints."+compName+".endpoints."+epName+".address"] =
				fmt.Sprintf("%s:%d", string(host), ep.Port)
			r.endpointPorts["topology_endpoints."+compName+".endpoints."+epName+".port"] =
				fmt.Sprintf("%d", ep.Port)
		}
	}
	return r, nil
}

func (r *resolver) resolve(in string) (string, error) {
	var resolveErr error
	out := placeholderRE.ReplaceAllStringFunc(in, func(match string) string {
		m := placeholderRE.FindStringSubmatch(match)
		if len(m) != 2 {
			return match
		}
		key := m[1]
		switch key {
		case "verself_bin":
			return r.verselfBin
		case "verself_domain":
			return r.verselfDomain
		case "spire_agent_socket_path":
			return r.spireAgentSocketPath
		case "component_auth_audience":
			// Resolved by nomad-deploy at submit time.
			return match
		}
		if v, ok := r.endpointAddrs[key]; ok {
			return v
		}
		if v, ok := r.endpointPorts[key]; ok {
			return v
		}
		if v, ok := r.componentHosts[key]; ok {
			return v
		}
		resolveErr = fmt.Errorf("nomad renderer: unresolved placeholder %q in %q", key, in)
		return match
	})
	if resolveErr != nil {
		return "", resolveErr
	}
	// Fail loud on residual `{{` that snuck past the regex (mismatched
	// braces, bare `{{` on a literal value, etc.). The auth-audience
	// sentinel is the only allowed remainder.
	for _, fragment := range strings.Split(out, AuthAudiencePlaceholder) {
		if strings.Contains(fragment, "{{") || strings.Contains(fragment, "}}") {
			return "", fmt.Errorf("nomad renderer: unresolved braces in %q", in)
		}
	}
	return out, nil
}
