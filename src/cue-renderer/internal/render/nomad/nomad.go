// Package nomad projects a CUE component with `deployment.supervisor:
// "nomad"` into a JSON job spec consumable by Nomad's HTTP API.
//
// Output layout: one file per opted-in component at
// `jobs/<component>.nomad.json`, anchored under the cache root that
// `aspect render --site=<site>` populates.
//
// Spec shape: a single TaskGroup per unit declared in
// `workload.units`. The unit block is the cross-supervisor authoring
// contract (env vars, dependency wiring, readiness probes);
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
// strings. The Nomad renderer resolves topology placeholders at render time
// so the spec the deploy wrapper submits is fully formed JSON except for
// artifact URLs, which are derived from Bazel build outputs after render.
var placeholderRE = regexp.MustCompile(`\{\{\s*([a-zA-Z0-9_.]+)\s*\}\}`)

type Renderer struct{}

func (Renderer) Name() string { return "nomad" }

// IndexEntry is one row of jobs/index.json. The Bazel Nomad resolver
// reads this file to wire artifact tarball outputs into the rendered
// specs; JSON tags are part of that renderer/rule contract.
type IndexEntry struct {
	Component  string          `json:"component"`
	JobID      string          `json:"job_id"`
	BazelLabel string          `json:"bazel_label"`
	Output     string          `json:"output"`
	Artifacts  []IndexArtifact `json:"artifacts"`
}

type IndexArtifact struct {
	BazelLabel         string `json:"bazel_label"`
	NomadArtifactLabel string `json:"nomad_artifact_label"`
	Output             string `json:"output"`
}

type indexFile struct {
	ArtifactBaseURL   string       `json:"artifact_base_url"`
	ArtifactStorePath string       `json:"artifact_store_path"`
	NomadAddr         string       `json:"nomad_addr"`
	Components        []IndexEntry `json:"components"`
}

func (Renderer) Render(_ context.Context, loaded load.Loaded, out render.WritableFS) error {
	components, err := projection.Components(loaded)
	if err != nil {
		return err
	}
	resolver, err := newResolver(loaded)
	if err != nil {
		return err
	}
	artifactStorePath, _ := loaded.Config.AnsibleVars["nomad_artifacts_dir"].(string)
	var index = indexFile{
		ArtifactBaseURL:   "http://" + resolver.endpointAddrs["topology_endpoints.nomad_artifacts.endpoints.http.address"],
		ArtifactStorePath: artifactStorePath,
		NomadAddr:         "http://" + resolver.endpointAddrs["topology_endpoints.nomad.endpoints.http.address"],
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

		artifacts, err := taskArtifacts(component)
		if err != nil {
			return fmt.Errorf("%s: %w", component.Name, err)
		}
		index.Components = append(index.Components, IndexEntry{
			Component:  component.Name,
			JobID:      jobID(component.Name),
			BazelLabel: artifacts[0].BazelLabel,
			Output:     artifacts[0].Output,
			Artifacts:  artifacts,
		})
	}
	if len(index.Components) == 0 {
		return nil
	}
	indexBody, err := json.MarshalIndent(index, "", "  ")
	if err != nil {
		return err
	}
	indexBody = append(indexBody, '\n')
	return out.WriteFile("jobs/index.json", indexBody)
}

const (
	nodeRuntimeOutput = "nodejs-runtime"
	nodeRuntimeLabel  = "//src/viteplus-monorepo:nodejs_runtime_nomad_artifact"
)

// taskArtifacts extracts every artifact that a rendered Nomad task may
// execute. The first item is the primary component artifact; additional
// items come from named processes such as sandbox-rental's recurring worker
// and shared runtime artifacts required by node_app tasks.
func taskArtifacts(component projection.NamedMap) ([]IndexArtifact, error) {
	artifacts := []IndexArtifact{}
	seen := map[string]struct{}{}
	appendArtifact := func(artifact map[string]any) error {
		next, err := indexArtifact(component.Name, artifact)
		if err != nil {
			return err
		}
		if _, ok := seen[next.Output]; ok {
			return nil
		}
		seen[next.Output] = struct{}{}
		artifacts = append(artifacts, next)
		return nil
	}
	artifact, _ := component.Value["artifact"].(map[string]any)
	if err := appendArtifact(artifact); err != nil {
		return nil, err
	}
	if artifactKind(artifact) == "node_app" {
		appendStaticArtifact(IndexArtifact{BazelLabel: nodeRuntimeLabel, NomadArtifactLabel: nodeRuntimeLabel, Output: nodeRuntimeOutput}, &artifacts, seen)
	}
	processes, err := projection.NestedFields(component, "processes")
	if err != nil {
		return nil, err
	}
	for _, process := range processes {
		artifact, err := projection.Map(process.Value, component.Name+".processes."+process.Name, "artifact")
		if err != nil {
			return nil, err
		}
		if err := appendArtifact(artifact); err != nil {
			return nil, err
		}
		if artifactKind(artifact) == "node_app" {
			appendStaticArtifact(IndexArtifact{BazelLabel: nodeRuntimeLabel, NomadArtifactLabel: nodeRuntimeLabel, Output: nodeRuntimeOutput}, &artifacts, seen)
		}
	}
	if len(artifacts) == 0 {
		return nil, fmt.Errorf("nomad supervisor requires at least one task artifact")
	}
	return artifacts, nil
}

func appendStaticArtifact(next IndexArtifact, artifacts *[]IndexArtifact, seen map[string]struct{}) {
	if _, ok := seen[next.Output]; ok {
		return
	}
	seen[next.Output] = struct{}{}
	*artifacts = append(*artifacts, next)
}

func indexArtifact(componentName string, artifact map[string]any) (IndexArtifact, error) {
	kind := artifactKind(artifact)
	label, _ := artifact["bazel_label"].(string)
	output, _ := artifact["output"].(string)
	if label == "" || output == "" {
		return IndexArtifact{}, fmt.Errorf("%s artifact: bazel_label and output required, got label=%q output=%q", componentName, label, output)
	}
	switch kind {
	case "go_binary":
		return IndexArtifact{BazelLabel: label, NomadArtifactLabel: nomadArtifactLabel(label, output), Output: output}, nil
	case "node_app":
		return IndexArtifact{BazelLabel: label, NomadArtifactLabel: label, Output: output}, nil
	default:
		return IndexArtifact{}, fmt.Errorf("artifact.kind=%q: nomad supervisor requires go_binary or node_app task artifacts", kind)
	}
}

func artifactKind(artifact map[string]any) string {
	kind, _ := artifact["kind"].(string)
	return kind
}

func nomadArtifactLabel(binaryLabel, output string) string {
	pkg, _, ok := strings.Cut(binaryLabel, ":")
	if !ok {
		return binaryLabel + "_nomad_artifact"
	}
	return pkg + ":" + output + "_nomad_artifact"
}

func jobPath(id string) string { return "jobs/" + id + ".nomad.json" }

// jobID converts a component name (snake_case) to the Nomad-side
// identifier (kebab-case) operators use with `nomad job status <name>`.
func jobID(componentName string) string {
	return strings.ReplaceAll(componentName, "_", "-")
}

func buildJobSpec(component projection.NamedMap, deployment map[string]any, resolver *resolver) (map[string]any, error) {
	workload, ok := component.Value["workload"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s.workload: missing", component.Name)
	}
	rawUnits, err := projection.Slice(workload, component.Name+".workload", "units")
	if err != nil {
		return nil, err
	}
	if len(rawUnits) == 0 {
		return nil, fmt.Errorf("%s.workload.units: nomad supervisor requires at least one unit", component.Name)
	}

	taskGroups := make([]map[string]any, 0, len(rawUnits))
	for i, raw := range rawUnits {
		unit, ok := raw.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("%s.workload.units[%d]: expected map, got %T", component.Name, i, raw)
		}
		group, err := buildTaskGroup(component, unit, deployment, resolver, i == 0)
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
		// Meta is filled by the Bazel Nomad resolver with artifact/spec
		// digests so Nomad sees a new job version when the deploy unit
		// changes.
		"Meta": map[string]any{},
	}
	return map[string]any{"Job": jobBody}, nil
}

func componentHasDatabase(component projection.NamedMap) bool {
	postgres, _ := component.Value["postgres"].(map[string]any)
	database, _ := postgres["database"].(string)
	return database != ""
}

func buildTaskGroup(component projection.NamedMap, unit map[string]any, deployment map[string]any, resolver *resolver, firstUnit bool) (map[string]any, error) {
	unitName, _ := unit["name"].(string)
	unitUser, _ := unit["user"].(string)
	if unitName == "" || unitUser == "" {
		return nil, fmt.Errorf("%s.workload.units: name/user required", component.Name)
	}
	if err := checkUnitCompatibility(component.Name, unit); err != nil {
		return nil, err
	}
	artifact, err := artifactForUnit(component, unitName)
	if err != nil {
		return nil, err
	}
	output, _ := artifact["output"].(string)
	kind := artifactKind(artifact)

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
	reservedPorts, primaryPort, err := reservedPorts(component, endpoints, unit)
	if err != nil {
		return nil, err
	}

	services, err := buildServices(component.Name, unitName, unit, primaryPort, endpoints)
	if err != nil {
		return nil, err
	}

	task := map[string]any{
		"Name":   unitName,
		"Driver": "raw_exec",
		"User":   unitUser,
		"Config": map[string]any{
			"command": "local/bin/" + output,
		},
		"Artifacts":   artifactStanzas(kind, output),
		"Env":         envOut,
		"Resources":   resources,
		"KillSignal":  killSignal,
		"KillTimeout": killTimeout,
		"Services":    services,
		"RestartPolicy": map[string]any{
			"Attempts": 3,
			"Interval": int64(5 * time.Minute / time.Nanosecond),
			"Delay":    int64(15 * time.Second / time.Nanosecond),
			"Mode":     "delay",
		},
	}

	tasks := []map[string]any{}
	if firstUnit && componentHasDatabase(component) {
		migrationCommand, migrationArgs, err := migrationCommand(kind, output)
		if err != nil {
			return nil, err
		}
		migrationEnv := map[string]string{}
		for k, v := range envOut {
			migrationEnv[k] = v
		}
		migrationEnv["OTEL_SERVICE_NAME"] = unitName + "-migration"
		tasks = append(tasks, map[string]any{
			"Name":   unitName + "-migrate",
			"Driver": "raw_exec",
			"User":   unitUser,
			"Lifecycle": map[string]any{
				"Hook":    "prestart",
				"Sidecar": false,
			},
			"Config": map[string]any{
				"command": migrationCommand,
				"args":    migrationArgs,
			},
			"Artifacts": artifactStanzas(kind, output),
			"Env":       migrationEnv,
			"Resources": map[string]any{
				"CPU":      int64(100),
				"MemoryMB": int64(128),
			},
		})
	}
	tasks = append(tasks, task)

	group := map[string]any{
		"Name":   unitName,
		"Count":  count,
		"Tasks":  tasks,
		"Update": updateBlock,
	}
	if len(reservedPorts) > 0 {
		group["Networks"] = []map[string]any{
			// host_network "loopback" is registered in nomad.hcl client
			// config and pins ReservedPorts to 127.0.0.1. The native
			// registry's auto address-mode then advertises 127.0.0.1
			// for HTTP/TCP checks — services keep their loopback bind
			// and defense-in-depth is preserved (the per-component
			// nftables ruleset is the second layer, not the first).
			{"Mode": "host", "ReservedPorts": reservedPortsWithHostNetwork(reservedPorts, "loopback")},
		}
	}
	return group, nil
}

func migrationCommand(kind, output string) (string, []string, error) {
	switch kind {
	case "go_binary":
		return "local/bin/" + output, []string{"migrate", "up"}, nil
	case "node_app":
		return "local/bin/" + output + "-migrate", []string{}, nil
	default:
		return "", nil, fmt.Errorf("%s components with PostgreSQL need an explicit Nomad migration command", kind)
	}
}

func artifactForUnit(component projection.NamedMap, unitName string) (map[string]any, error) {
	artifact, err := projection.Map(component.Value, component.Name, "artifact")
	if err != nil {
		return nil, err
	}
	primaryOutput, _ := artifact["output"].(string)
	if primaryOutput == "" {
		return nil, fmt.Errorf("%s.artifact.output: missing", component.Name)
	}
	if unitName == primaryOutput {
		return artifact, nil
	}
	processes, err := projection.NestedFields(component, "processes")
	if err != nil {
		return nil, err
	}
	for _, process := range processes {
		processUnit, _ := process.Value["unit"].(string)
		if unitName != processUnit {
			continue
		}
		artifact, err := projection.Map(process.Value, component.Name+".processes."+process.Name, "artifact")
		if err != nil {
			return nil, err
		}
		output, _ := artifact["output"].(string)
		if output == "" {
			return nil, fmt.Errorf("%s.processes.%s.artifact.output: missing", component.Name, process.Name)
		}
		return artifact, nil
	}
	return nil, fmt.Errorf("%s.workload.units.%s: no component or process artifact matches unit", component.Name, unitName)
}

func artifactStanzas(kind, output string) []map[string]any {
	stanzas := []map[string]any{artifactStanza(output)}
	if kind == "node_app" {
		stanzas = append(stanzas, artifactStanza(nodeRuntimeOutput))
	}
	return stanzas
}

func artifactStanza(output string) map[string]any {
	return map[string]any{
		"Chown":        true,
		"GetterSource": "verself-artifact://" + output,
		"RelativeDest": "local",
	}
}

func reservedPortsWithHostNetwork(reserved []map[string]any, hostNetwork string) []map[string]any {
	out := make([]map[string]any, 0, len(reserved))
	for _, p := range reserved {
		copy := map[string]any{}
		for k, v := range p {
			copy[k] = v
		}
		copy["HostNetwork"] = hostNetwork
		out = append(out, copy)
	}
	return out
}

// checkUnitCompatibility refuses to render a Nomad TaskGroup for a unit
// that uses systemd-only knobs we haven't translated yet. The two
// load-bearing knobs from service_facts.cue are load_credentials (which
// systemd materialises as a tmpfs at $CREDENTIALS_DIRECTORY/<name>)
// and bind_read_only_paths (per-unit mount-namespace overlays). Both
// have raw_exec equivalents, but neither is automatic — fail loud at
// render time so the next service migration explicitly addresses them.
//
// Translation guidance:
//
//	load_credentials: [{name: "x", path: "/host/path"}]
//	  systemd: LoadCredential=x:/host/path → $CREDENTIALS_DIRECTORY/x
//	  nomad:   set an explicit env var
//	           VERSELF_CRED_<NAME>=/etc/credstore/<job>/<name>. If the
//	           source path is already under that service's credstore,
//	           reuse it directly. Otherwise add a secret_refs entry with
//	           source.kind="remote_src" so Ansible copies the source into
//	           the service-owned credstore path before nomad-deploy
//	           submits the job. envconfig reads the explicit path in both
//	           supervisor worlds.
//
//	bind_read_only_paths: ["/etc/verself/foo:/etc/foo"]
//	  systemd: per-unit mount namespace overlay
//	  nomad:   raw_exec has no mount namespace. Two paths:
//	           (a) merge the host-side content into the host's
//	               /etc/<foo> directly (what we did for the
//	               auth-discovery-hosts → /etc/hosts case via the
//	               base role). Use this when content is identical
//	               across services.
//	           (b) emit a Nomad `template { source = "/host/path"
//	               destination = "secrets/foo" }` block. Note
//	               Nomad's template SourcePath is task-local;
//	               use the `data` form with EmbeddedTmpl that
//	               reads from a host-volume mount instead.
func checkUnitCompatibility(componentName string, unit map[string]any) error {
	if creds, _ := unit["load_credentials"].([]any); len(creds) > 0 {
		return fmt.Errorf("%s.workload.units.%s.load_credentials: nomad supervisor needs explicit credential translation:\n%s",
			componentName, mustUnitName(unit), credentialTranslationRecipes(componentName, unit, creds))
	}
	if paths, _ := unit["bind_read_only_paths"].([]any); len(paths) > 0 {
		strs := make([]string, 0, len(paths))
		for _, p := range paths {
			if s, ok := p.(string); ok {
				strs = append(strs, s)
			}
		}
		return fmt.Errorf("%s.workload.units.%s.bind_read_only_paths: nomad supervisor doesn't auto-translate %v; see internal/render/nomad/nomad.go:checkUnitCompatibility for migration guidance",
			componentName, mustUnitName(unit), strs)
	}
	return nil
}

func credentialTranslationRecipes(componentName string, unit map[string]any, creds []any) string {
	componentCredstore := "/etc/credstore/" + jobID(componentName)
	group, _ := unit["group"].(string)
	if group == "" {
		group = componentName
	}

	var b strings.Builder
	for _, c := range creds {
		cm, ok := c.(map[string]any)
		if !ok {
			continue
		}
		name, _ := cm["name"].(string)
		sourcePath, _ := cm["path"].(string)
		if name == "" || sourcePath == "" {
			continue
		}
		envName := credentialPathEnvName(name)
		ownedPrefix := componentCredstore + "/"
		if strings.HasPrefix(sourcePath, ownedPrefix) {
			fmt.Fprintf(&b, "- %s from %s:\n", name, sourcePath)
			fmt.Fprintf(&b, "  remove this load_credentials entry\n")
			fmt.Fprintf(&b, "  environment += {%s: %q}\n", envName, sourcePath)
			continue
		}
		targetPath := componentCredstore + "/" + name
		fmt.Fprintf(&b, "- %s from %s:\n", name, sourcePath)
		fmt.Fprintf(&b, "  secret_refs += {name: %q, path: %q, owner: %q, group: %q, mode: %q, source: {kind: %q, remote_src: %q}}\n",
			name, targetPath, "root", group, "0640", "remote_src", sourcePath)
		fmt.Fprintf(&b, "  environment += {%s: %q}\n", envName, targetPath)
	}
	if b.Len() == 0 {
		return "no named credentials found; each entry must include name and path"
	}
	return strings.TrimRight(b.String(), "\n")
}

func credentialPathEnvName(name string) string {
	var b strings.Builder
	b.WriteString("VERSELF_CRED_")
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r - ('a' - 'A'))
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return b.String()
}

func mustUnitName(unit map[string]any) string {
	n, _ := unit["name"].(string)
	return n
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

func reservedPorts(component projection.NamedMap, endpoints map[string]any, unit map[string]any) ([]map[string]any, string, error) {
	if len(endpoints) == 0 {
		return nil, "", nil
	}
	ownedEndpoints, err := serviceenv.EndpointsForUnit(component, unit)
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
		port, err := projection.Int(endpoint, "topology.components."+component.Name+".endpoints."+name, "port")
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

func buildServices(componentName, unitName string, unit map[string]any, primaryPort string, endpoints map[string]any) ([]map[string]any, error) {
	rawReadiness, _ := unit["readiness"].([]any)
	if len(rawReadiness) == 0 {
		return nil, nil
	}
	_ = endpoints // referenced through PortLabel resolution at the Nomad client side
	checks := make([]map[string]any, 0, len(rawReadiness))
	for _, item := range rawReadiness {
		probe, ok := item.(map[string]any)
		if !ok {
			continue
		}
		kind, _ := probe["kind"].(string)
		endpoint, _ := probe["endpoint"].(string)
		check := map[string]any{
			"Name":      unitName + "-" + kind + "-" + endpoint,
			"PortLabel": endpoint,
			"Interval":  int64(10 * time.Second / time.Nanosecond),
			"Timeout":   int64(3 * time.Second / time.Nanosecond),
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
			return nil, fmt.Errorf("%s.workload.units.%s.readiness: unsupported probe kind %q", componentName, unitName, kind)
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
// serviceenv-derived strings to literal values at render time so the spec the
// bare-metal node submits is fully formed.
//
// Three known-static buckets land here:
//   - string-valued ansible_vars and `spire_agent_socket_path` from
//     loaded.Config.
//   - `topology_endpoints.<comp>.endpoints.<ep>.{address,port}` and
//     `topology_endpoints.<comp>.host` from the topology projection.
type resolver struct {
	spireAgentSocketPath string
	ansibleVars          map[string]string
	endpointAddrs        map[string]string // "topology_endpoints.<c>.endpoints.<e>.address"
	endpointPorts        map[string]string
	componentHosts       map[string]string // "topology_endpoints.<c>.host"
}

func newResolver(loaded load.Loaded) (*resolver, error) {
	r := &resolver{
		spireAgentSocketPath: loaded.Config.Spire.AgentSocketPath,
		ansibleVars:          map[string]string{},
		endpointAddrs:        map[string]string{},
		endpointPorts:        map[string]string{},
		componentHosts:       map[string]string{},
	}
	for key, raw := range loaded.Config.AnsibleVars {
		if value, ok := raw.(string); ok {
			r.ansibleVars[key] = value
		}
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
	return r.resolveWithStack(in, map[string]bool{})
}

func (r *resolver) resolveWithStack(in string, stack map[string]bool) (string, error) {
	var resolveErr error
	out := placeholderRE.ReplaceAllStringFunc(in, func(match string) string {
		m := placeholderRE.FindStringSubmatch(match)
		if len(m) != 2 {
			return match
		}
		key := m[1]
		switch key {
		case "spire_agent_socket_path":
			return r.spireAgentSocketPath
		}
		if v, ok := r.ansibleVars[key]; ok {
			if stack[key] {
				resolveErr = fmt.Errorf("nomad renderer: recursive ansible var %q in %q", key, in)
				return match
			}
			nextStack := map[string]bool{}
			for k, value := range stack {
				nextStack[k] = value
			}
			nextStack[key] = true
			resolved, err := r.resolveWithStack(v, nextStack)
			if err != nil {
				resolveErr = err
				return match
			}
			return resolved
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
	// braces, bare `{{` on a literal value, etc.).
	if strings.Contains(out, "{{") || strings.Contains(out, "}}") {
		return "", fmt.Errorf("nomad renderer: unresolved braces in %q", in)
	}
	return out, nil
}
