// Package serviceenv projects a component + unit pair into the
// environment-variable map that Nomad injects into the running service.
//
// Authoring contract: services declare their env in
// `service_facts.cue.<service>.workload.units[*]`. That block is the
// source of truth for Nomad-supervised components.
package serviceenv

import (
	"fmt"
	"sort"

	"github.com/verself/cue-renderer/internal/render/projection"
)

// Unit returns the merged environment map (derived service vars +
// explicit unit overrides) for the given component / unit pair. Components
// of `kind: "service"` get the verself-runtime derivation (OTEL, listen
// addresses, SPIFFE socket, Postgres DSN/pool, ClickHouse, auth).
// Components of other kinds receive only the explicit `environment` block.
func Unit(component projection.NamedMap, unit map[string]any) (map[string]string, error) {
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
			return nil, fmt.Errorf("%s.workload.units.%s.environment.%s: expected string, got %T", component.Name, mustString(unit, "name"), key, value)
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
	if supervisor(component) == "nomad" {
		environment["VERSELF_SUPERVISOR"] = "nomad"
		environment["OTEL_RESOURCE_ATTRIBUTES"] = "verself.supervisor=nomad"
	}
	if _, ok := endpoints["public_http"]; ok {
		environment["VERSELF_LISTEN_ADDR"] = topologyEndpointAddress(component.Name, "public_http")
	}
	if _, ok := endpoints["internal_https"]; ok {
		environment["VERSELF_INTERNAL_LISTEN_ADDR"] = topologyEndpointAddress(component.Name, "internal_https")
	}
	if mustBool(unit, "requires_spiffe_sock") {
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
	workload, err := projection.Map(component.Value, component.Name, "workload")
	if err != nil {
		return nil, err
	}
	if raw, ok := workload["clickhouse"]; ok && unitHasClickHouseCredential(unit) {
		clickhouse, ok := raw.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("%s.workload.clickhouse: expected map, got %T", component.Name, raw)
		}
		user, err := projection.String(clickhouse, component.Name+".workload.clickhouse", "user")
		if err != nil {
			return nil, err
		}
		environment["VERSELF_CLICKHOUSE_ADDRESS"] = "{{ topology_endpoints.clickhouse.endpoints.native_tls.address }}"
		environment["VERSELF_CLICKHOUSE_USER"] = user
	}
	if len(endpoints) > 0 {
		auth, err := projection.Map(workload, component.Name+".workload", "auth")
		if err != nil {
			return nil, err
		}
		authKind, err := projection.String(auth, component.Name+".workload.auth", "kind")
		if err != nil {
			return nil, err
		}
		if authKind != "none" {
			audience, err := projection.String(auth, component.Name+".workload.auth", "audience")
			if err != nil {
				return nil, err
			}
			environment["VERSELF_AUTH_ISSUER_URL"] = "https://auth.{{ verself_domain }}"
			environment["VERSELF_AUTH_AUDIENCE"] = audience
		}
	}
	return environment, nil
}

func supervisor(component projection.NamedMap) string {
	deployment, _ := component.Value["deployment"].(map[string]any)
	value, _ := deployment["supervisor"].(string)
	return value
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

// EndpointsForUnit returns the endpoint labels the given unit *binds*,
// sorted alphabetically. This is the port-reservation view (used by the
// Nomad renderer) and is stricter than the env-var view that
// `serviceenv.Unit` derives internally:
//
//   - A named worker process binds exactly the endpoints listed in
//     `process.endpoints`.
//   - The primary unit binds the component endpoints that no named
//     process claims. (For env-var derivation the primary still knows
//     about every endpoint — being aware of an endpoint and binding it
//     are different.)
//
// The split keeps two TaskGroups from racing for the same Nomad
// ReservedPort label when a multi-process component opts into Nomad
// supervision.
//
// Errors when the unit name doesn't match the component artifact output
// or any processes.<n>.unit entry.
func EndpointsForUnit(component projection.NamedMap, unit map[string]any) ([]string, error) {
	process, err := processForUnit(component, mustString(unit, "name"))
	if err != nil {
		return nil, err
	}
	owned := endpointSet(component, process)
	if process["name"] == "primary" {
		processes, err := projection.NestedFields(component, "processes")
		if err != nil {
			return nil, err
		}
		for _, p := range processes {
			for _, claimed := range mustStringSlice(p.Value, "endpoints") {
				delete(owned, claimed)
			}
		}
	}
	out := make([]string, 0, len(owned))
	for name := range owned {
		out = append(out, name)
	}
	sort.Strings(out)
	return out, nil
}

// processForUnit returns the per-process record (primary or named worker)
// that owns the given unit name. The primary unit is the component artifact
// output; named workers come from the `processes` block.
func processForUnit(component projection.NamedMap, unitName string) (map[string]any, error) {
	artifact, err := projection.Map(component.Value, component.Name, "artifact")
	if err != nil {
		return nil, err
	}
	primaryUnit, _ := artifact["output"].(string)
	if unitName == primaryUnit {
		return map[string]any{"name": "primary"}, nil
	}
	processes, err := projection.NestedFields(component, "processes")
	if err != nil {
		return nil, err
	}
	for _, process := range processes {
		processUnit, _ := process.Value["unit"].(string)
		if unitName == processUnit {
			out := projection.CloneMap(process.Value)
			out["name"] = process.Name
			return out, nil
		}
	}
	return nil, fmt.Errorf("%s.workload.units.%s: no matching runtime or process", component.Name, unitName)
}

// endpointSet returns the labels of the endpoints this unit/process owns.
// The primary unit owns every endpoint declared on the component; named
// workers own only those listed in `process.endpoints`.
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

// UnitHasCredential reports whether the unit's load_credentials list
// includes a credential with the given name.
func UnitHasCredential(unit map[string]any, name string) bool {
	for _, credential := range mustMapSlice(unit, "load_credentials") {
		if mustString(credential, "name") == name {
			return true
		}
	}
	return false
}

func unitHasClickHouseCredential(unit map[string]any) bool {
	if UnitHasCredential(unit, "clickhouse-ca-cert") {
		return true
	}
	_, ok := mustMap(unit, "environment")["VERSELF_CRED_CLICKHOUSE_CA_CERT"]
	return ok
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

func mustBool(values map[string]any, key string) bool {
	value, _ := values[key].(bool)
	return value
}

// SortedKeys is exported for callers that want to walk the env map in a
// deterministic order, which both renderers need so their output diffs
// stay stable.
func SortedKeys(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
