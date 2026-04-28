package systemd

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/verself/cue-renderer/internal/load"
	"github.com/verself/cue-renderer/internal/render"
	"github.com/verself/cue-renderer/internal/render/projection"
)

type Renderer struct{}

func (Renderer) Name() string { return "systemd" }

func (Renderer) Render(_ context.Context, loaded load.Loaded, out render.WritableFS) error {
	components, err := projection.Components(loaded)
	if err != nil {
		return err
	}
	unitNames := map[string]struct{}{}
	for _, component := range components {
		converge, ok := component.Value["converge"].(map[string]any)
		if !ok {
			continue
		}
		enabled, _ := converge["enabled"].(bool)
		if !enabled {
			continue
		}
		systemdConfig, err := projection.Map(converge, component.Name+".converge", "systemd")
		if err != nil {
			return err
		}
		units, err := projection.Slice(systemdConfig, component.Name+".converge.systemd", "units")
		if err != nil {
			return err
		}
		for i, raw := range units {
			unit, ok := raw.(map[string]any)
			if !ok {
				return fmt.Errorf("%s.converge.systemd.units[%d]: expected map, got %T", component.Name, i, raw)
			}
			name, err := projection.String(unit, component.Name+".converge.systemd.units", "name")
			if err != nil {
				return err
			}
			unitNames[name] = struct{}{}
			body, err := renderUnit(component, unit)
			if err != nil {
				return fmt.Errorf("render %s: %w", name, err)
			}
			if err := out.WriteFile(unitPath(name), body); err != nil {
				return err
			}
		}
	}
	if err := out.WriteFile(handlerPath(), renderHandlers(unitNames)); err != nil {
		return err
	}
	return nil
}

func unitPath(name string) string {
	return projection.RenderedPath("/etc/systemd/system/" + name + ".service")
}

func handlerPath() string {
	return "src/platform/ansible/handlers/component-systemd.yml"
}

func renderHandlers(unitNames map[string]struct{}) []byte {
	names := make([]string, 0, len(unitNames))
	for name := range unitNames {
		names = append(names, name)
	}
	sort.Strings(names)

	var b strings.Builder
	b.WriteString(projection.Header)
	b.WriteString("---\n")
	for i, name := range names {
		if i > 0 {
			b.WriteString("\n")
		}
		fmt.Fprintf(&b, "- name: Restart %s\n", name)
		fmt.Fprintf(&b, "  listen: restart %s\n", name)
		b.WriteString("  ansible.builtin.systemd:\n")
		b.WriteString("    daemon_reload: true\n")
		fmt.Fprintf(&b, "    name: %s\n", name)
		b.WriteString("    state: restarted\n")
	}
	return []byte(b.String())
}

func renderUnit(component projection.NamedMap, unit map[string]any) ([]byte, error) {
	name := mustString(unit, "name")
	hardening := normalizedHardening(mustMap(unit, "hardening"))
	environment, err := unitEnvironment(component, unit)
	if err != nil {
		return nil, err
	}

	var b strings.Builder
	b.WriteString(projection.Header)
	b.WriteString("[Unit]\n")
	fmt.Fprintf(&b, "Description=%s\n", mustString(unit, "description"))
	writeJoined(&b, "After", mustStringSlice(unit, "after"))
	writeJoined(&b, "Requires", mustStringSlice(unit, "requires"))
	writeJoined(&b, "Wants", mustStringSlice(unit, "wants"))

	b.WriteString("\n[Service]\n")
	b.WriteString("Type=simple\n")
	fmt.Fprintf(&b, "User=%s\n", mustString(unit, "user"))
	fmt.Fprintf(&b, "Group=%s\n", mustString(unit, "group"))
	writeJoined(&b, "SupplementaryGroups", mustStringSlice(unit, "supplementary_groups"))
	for _, value := range mustStringSlice(unit, "bind_read_only_paths") {
		fmt.Fprintf(&b, "BindReadOnlyPaths=%s\n", value)
	}
	for _, credential := range mustMapSlice(unit, "load_credentials") {
		fmt.Fprintf(&b, "LoadCredential=%s:%s\n", mustString(credential, "name"), mustString(credential, "path"))
	}
	for _, key := range sortedStringKeys(environment) {
		fmt.Fprintf(&b, "Environment=%s=%s\n", key, environment[key])
	}
	fmt.Fprintf(&b, "ExecStart=%s\n", mustString(unit, "exec"))
	fmt.Fprintf(&b, "Restart=%s\n", mustString(unit, "restart"))
	fmt.Fprintf(&b, "RestartSec=%d\n", mustInt(unit, "restart_sec"))
	fmt.Fprintf(&b, "CapabilityBoundingSet=%s\n", mustString(hardening, "capability_bounding_set"))
	writeBool(&b, "ProtectHome", hardening, "protect_home")
	fmt.Fprintf(&b, "ProtectSystem=%s\n", mustString(hardening, "protect_system"))
	for _, value := range mustStringSlice(hardening, "read_write_paths") {
		fmt.Fprintf(&b, "ReadWritePaths=%s\n", value)
	}
	writeBool(&b, "PrivateDevices", hardening, "private_devices")
	writeBool(&b, "PrivateTmp", hardening, "private_tmp")
	writeBool(&b, "ProtectClock", hardening, "protect_clock")
	writeBool(&b, "ProtectControlGroups", hardening, "protect_control_groups")
	writeBool(&b, "ProtectKernelLogs", hardening, "protect_kernel_logs")
	writeBool(&b, "ProtectKernelModules", hardening, "protect_kernel_modules")
	writeBool(&b, "ProtectKernelTunables", hardening, "protect_kernel_tunables")
	writeBool(&b, "LockPersonality", hardening, "lock_personality")
	writeBool(&b, "NoNewPrivileges", hardening, "no_new_privileges")
	writeJoined(&b, "RestrictAddressFamilies", mustStringSlice(hardening, "restrict_address_families"))
	if _, ok := hardening["restrict_namespaces"]; ok {
		writeBool(&b, "RestrictNamespaces", hardening, "restrict_namespaces")
	}
	writeBool(&b, "RestrictRealtime", hardening, "restrict_realtime")
	writeBool(&b, "RestrictSUIDSGID", hardening, "restrict_suid_sgid")
	fmt.Fprintf(&b, "SystemCallArchitectures=%s\n", mustString(hardening, "system_call_architectures"))
	fmt.Fprintf(&b, "UMask=%s\n", mustString(hardening, "umask"))

	b.WriteString("\n[Install]\n")
	b.WriteString("WantedBy=multi-user.target\n")
	if name == "" {
		return nil, fmt.Errorf("unit name is empty")
	}
	return []byte(b.String()), nil
}

func unitEnvironment(component projection.NamedMap, unit map[string]any) (map[string]string, error) {
	environment := map[string]string{}
	kind, err := projection.String(component.Value, component.Name, "kind")
	if err != nil {
		return nil, err
	}
	if kind == "service" {
		derived, err := derivedServiceEnvironment(component, unit)
		if err != nil {
			return nil, err
		}
		for key, value := range derived {
			environment[key] = value
		}
	}
	for key, value := range mustMap(unit, "environment") {
		stringValue, ok := value.(string)
		if !ok {
			return nil, fmt.Errorf("%s.converge.systemd.units.%s.environment.%s: expected string, got %T", component.Name, mustString(unit, "name"), key, value)
		}
		environment[key] = stringValue
	}
	return environment, nil
}

func derivedServiceEnvironment(component projection.NamedMap, unit map[string]any) (map[string]string, error) {
	unitName := mustString(unit, "name")
	unitProcess, err := processForUnit(component, unitName)
	if err != nil {
		return nil, err
	}
	endpoints := endpointSet(component, unitProcess)
	environment := map[string]string{
		"OTEL_EXPORTER_OTLP_ENDPOINT": "http://{{ topology_endpoints.otelcol.endpoints.otlp_grpc.address }}",
		"OTEL_SERVICE_NAME":           unitName,
	}
	if _, ok := endpoints["public_http"]; ok {
		environment["VERSELF_LISTEN_ADDR"] = topologyEndpointAddress(component.Name, "public_http")
	}
	if _, ok := endpoints["internal_https"]; ok {
		environment["VERSELF_INTERNAL_LISTEN_ADDR"] = topologyEndpointAddress(component.Name, "internal_https")
	}
	if requiresSpiffe(component, unit, unitProcess) {
		environment["SPIFFE_ENDPOINT_SOCKET"] = "unix://{{ spire_agent_socket_path }}"
	}
	postgres, err := projection.Map(component.Value, component.Name, "postgres")
	if err != nil {
		return nil, err
	}
	database, err := projection.String(postgres, component.Name+".postgres", "database")
	if err != nil {
		return nil, err
	}
	owner, err := projection.String(postgres, component.Name+".postgres", "owner")
	if err != nil {
		return nil, err
	}
	if database != "" || owner != "" {
		if database == "" || owner == "" {
			return nil, fmt.Errorf("%s.postgres: database and owner must both be set for service env derivation", component.Name)
		}
		environment["VERSELF_PG_DSN"] = fmt.Sprintf("postgres://%s@/%s?host=/var/run/postgresql&sslmode=disable", owner, database)
		pool, err := projection.Map(postgres, component.Name+".postgres", "pool")
		if err != nil {
			return nil, err
		}
		if err := appendPostgresPoolEnvironment(environment, component.Name+".postgres.pool", pool); err != nil {
			return nil, err
		}
	}
	if unitHasCredential(unit, "clickhouse-ca-cert") {
		converge, err := projection.Map(component.Value, component.Name, "converge")
		if err != nil {
			return nil, err
		}
		if raw, ok := converge["clickhouse"]; ok {
			clickhouse, ok := raw.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("%s.converge.clickhouse: expected map, got %T", component.Name, raw)
			}
			user, err := projection.String(clickhouse, component.Name+".converge.clickhouse", "user")
			if err != nil {
				return nil, err
			}
			environment["VERSELF_CLICKHOUSE_ADDRESS"] = "{{ topology_endpoints.clickhouse.endpoints.native_tls.address }}"
			environment["VERSELF_CLICKHOUSE_USER"] = user
		}
	}
	if len(endpoints) > 0 {
		converge, err := projection.Map(component.Value, component.Name, "converge")
		if err != nil {
			return nil, err
		}
		auth, err := projection.Map(converge, component.Name+".converge", "auth")
		if err != nil {
			return nil, err
		}
		authKind, err := projection.String(auth, component.Name+".converge.auth", "kind")
		if err != nil {
			return nil, err
		}
		if authKind != "none" {
			environment["VERSELF_AUTH_ISSUER_URL"] = "https://auth.{{ verself_domain }}"
			environment["VERSELF_AUTH_AUDIENCE"] = "{{ component_auth_audience }}"
		}
	}
	return environment, nil
}

func appendPostgresPoolEnvironment(environment map[string]string, path string, pool map[string]any) error {
	maxConns, err := projection.Int(pool, path, "max_conns")
	if err != nil {
		return err
	}
	minConns, err := projection.Int(pool, path, "min_conns")
	if err != nil {
		return err
	}
	maxLifetime, err := projection.Int(pool, path, "conn_max_lifetime_seconds")
	if err != nil {
		return err
	}
	maxIdle, err := projection.Int(pool, path, "conn_max_idle_seconds")
	if err != nil {
		return err
	}
	environment["VERSELF_PG_MAX_CONNS"] = fmt.Sprint(maxConns)
	environment["VERSELF_PG_MIN_CONNS"] = fmt.Sprint(minConns)
	environment["VERSELF_PG_CONN_MAX_LIFETIME_SECONDS"] = fmt.Sprint(maxLifetime)
	environment["VERSELF_PG_CONN_MAX_IDLE_SECONDS"] = fmt.Sprint(maxIdle)
	return nil
}

func processForUnit(component projection.NamedMap, unitName string) (map[string]any, error) {
	runtime, err := projection.Map(component.Value, component.Name, "runtime")
	if err != nil {
		return nil, err
	}
	primarySystemd, err := projection.String(runtime, component.Name+".runtime", "systemd")
	if err != nil {
		return nil, err
	}
	if unitName == primarySystemd {
		return map[string]any{"name": "primary"}, nil
	}
	processes, err := projection.NestedFields(component, "processes")
	if err != nil {
		return nil, err
	}
	for _, process := range processes {
		systemd, err := projection.String(process.Value, component.Name+".processes."+process.Name, "systemd")
		if err != nil {
			return nil, err
		}
		if unitName == systemd {
			out := projection.CloneMap(process.Value)
			out["name"] = process.Name
			return out, nil
		}
	}
	return nil, fmt.Errorf("%s.converge.systemd.units.%s: no matching runtime or process", component.Name, unitName)
}

func endpointSet(component projection.NamedMap, process map[string]any) map[string]struct{} {
	out := map[string]struct{}{}
	if process["name"] == "primary" {
		endpoints, _ := component.Value["endpoints"].(map[string]any)
		for name := range endpoints {
			out[name] = struct{}{}
		}
		return out
	}
	for _, endpoint := range mustStringSlice(process, "endpoints") {
		out[endpoint] = struct{}{}
	}
	return out
}

func topologyEndpointAddress(componentName, endpointName string) string {
	return "{{ topology_endpoints." + componentName + ".endpoints." + endpointName + ".address }}"
}

func requiresSpiffe(component projection.NamedMap, unit map[string]any, process map[string]any) bool {
	if value, ok := process["requires_spiffe_sock"].(bool); ok && value {
		return true
	}
	if process["name"] == "primary" {
		identities, _ := component.Value["identities"].(map[string]any)
		if len(identities) > 0 {
			return true
		}
	} else if len(mustStringSlice(process, "identities")) > 0 {
		return true
	}
	for _, group := range mustStringSlice(unit, "supplementary_groups") {
		if strings.Contains(group, "spire_workload_group") || group == "spire" {
			return true
		}
	}
	return false
}

func unitHasCredential(unit map[string]any, name string) bool {
	for _, credential := range mustMapSlice(unit, "load_credentials") {
		if mustString(credential, "name") == name {
			return true
		}
	}
	return false
}

func writeJoined(b *strings.Builder, key string, values []string) {
	if len(values) == 0 {
		return
	}
	fmt.Fprintf(b, "%s=%s\n", key, strings.Join(values, " "))
}

func writeBool(b *strings.Builder, systemdKey string, values map[string]any, key string) {
	fmt.Fprintf(b, "%s=%s\n", systemdKey, boolString(mustBool(values, key)))
}

func boolString(v bool) string {
	if v {
		return "true"
	}
	return "false"
}

func normalizedHardening(values map[string]any) map[string]any {
	out := map[string]any{
		"capability_bounding_set":   "",
		"protect_home":              true,
		"protect_system":            "strict",
		"read_write_paths":          []any{},
		"private_devices":           true,
		"private_tmp":               true,
		"protect_clock":             true,
		"protect_control_groups":    true,
		"protect_kernel_logs":       true,
		"protect_kernel_modules":    true,
		"protect_kernel_tunables":   true,
		"lock_personality":          true,
		"no_new_privileges":         true,
		"restrict_address_families": []any{"AF_INET", "AF_INET6", "AF_UNIX"},
		"restrict_realtime":         true,
		"restrict_suid_sgid":        true,
		"system_call_architectures": "native",
		"umask":                     "0077",
	}
	for key, value := range values {
		out[key] = value
	}
	return out
}

func mustMap(values map[string]any, key string) map[string]any {
	out, _ := values[key].(map[string]any)
	return out
}

func mustMapSlice(values map[string]any, key string) []map[string]any {
	raw, _ := values[key].([]any)
	out := make([]map[string]any, 0, len(raw))
	for _, item := range raw {
		if value, ok := item.(map[string]any); ok {
			out = append(out, value)
		}
	}
	return out
}

func mustString(values map[string]any, key string) string {
	value, _ := values[key].(string)
	return value
}

func mustStringSlice(values map[string]any, key string) []string {
	raw, _ := values[key].([]any)
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		if value, ok := item.(string); ok {
			out = append(out, value)
		}
	}
	return out
}

func mustInt(values map[string]any, key string) int64 {
	value, _ := projection.Int(values, "systemd", key)
	return value
}

func mustBool(values map[string]any, key string) bool {
	value, _ := values[key].(bool)
	return value
}

func sortedStringKeys(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
