// Package nomad projects a CUE component with `deployment.supervisor:
// "nomad"` into a JSON job spec consumable by Nomad's HTTP API.
//
// Output layout: one file per opted-in component at
// `jobs/<component>.nomad.json`, anchored under the cache root that
// `aspect render --site=<site>` populates.
//
// Spec shape: a single TaskGroup per unit declared in
// `workload.units`. The unit block is the cross-supervisor authoring
// contract (env vars, dependency wiring); this renderer just rewrites
// it into Nomad JSON. Readiness checks and the rollout policy are
// runtime contracts owned by each service — see
// src/<service>/.../nomad-overrides.json. raw_exec is the only
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
	"sort"
	"strings"
	"time"

	"cuelang.org/go/cue"

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
	DependsOn  []string        `json:"depends_on,omitempty"`
	Artifacts  []IndexArtifact `json:"artifacts"`
}

type IndexArtifact struct {
	BazelLabel         string `json:"bazel_label"`
	NomadArtifactLabel string `json:"nomad_artifact_label"`
	Output             string `json:"output"`
}

type indexArtifactDelivery struct {
	Kind                 string              `json:"kind"`
	Bucket               string              `json:"bucket"`
	KeyPrefix            string              `json:"key_prefix"`
	GetterSourcePrefix   string              `json:"getter_source_prefix"`
	GetterOptions        map[string]string   `json:"getter_options"`
	GetterCredentials    indexCredentials    `json:"getter_credentials"`
	PublisherCredentials indexCredentials    `json:"publisher_credentials"`
	ChecksumAlgorithm    string              `json:"checksum_algorithm"`
	Public               bool                `json:"public"`
	Origin               indexArtifactOrigin `json:"origin"`
}

type indexCredentials struct {
	Source             string `json:"source"`
	EnvironmentFile    string `json:"environment_file"`
	AccessKeyIDEnv     string `json:"access_key_id_env"`
	SecretAccessKeyEnv string `json:"secret_access_key_env"`
}

type indexArtifactOrigin struct {
	Scheme        string `json:"scheme"`
	Hostname      string `json:"hostname"`
	Port          int64  `json:"port"`
	Placement     string `json:"placement"`
	Resolution    string `json:"resolution"`
	ListenHost    string `json:"listen_host"`
	PublicDNS     bool   `json:"public_dns"`
	PublicIngress bool   `json:"public_ingress"`
	CABundlePath  string `json:"ca_bundle_path"`
}

type indexFile struct {
	ArtifactDelivery indexArtifactDelivery `json:"artifact_delivery"`
	NomadAddr        string                `json:"nomad_addr"`
	Components       []IndexEntry          `json:"components"`
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
	artifactDelivery, err := artifactDeliveryIndex(loaded)
	if err != nil {
		return err
	}
	var index = indexFile{
		ArtifactDelivery: artifactDelivery,
		NomadAddr:        "http://" + resolver.endpointAddrs["topology_endpoints.nomad.endpoints.http.address"],
	}
	unitOwners, err := nomadUnitOwners(components)
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

		artifacts, err := taskArtifacts(component)
		if err != nil {
			return fmt.Errorf("%s: %w", component.Name, err)
		}
		dependsOn, err := nomadDependencies(component, unitOwners)
		if err != nil {
			return fmt.Errorf("%s: %w", component.Name, err)
		}
		index.Components = append(index.Components, IndexEntry{
			Component:  component.Name,
			JobID:      jobID(component.Name),
			BazelLabel: artifacts[0].BazelLabel,
			Output:     artifacts[0].Output,
			DependsOn:  dependsOn,
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

func artifactDeliveryIndex(loaded load.Loaded) (indexArtifactDelivery, error) {
	delivery := loaded.Config.Artifacts.Nomad
	if delivery.Kind != "garage_s3_private_origin" {
		return indexArtifactDelivery{}, fmt.Errorf("config.artifacts.nomad.kind=%q: only garage_s3_private_origin is supported", delivery.Kind)
	}
	if delivery.Origin.PublicDNS || delivery.Origin.PublicIngress {
		return indexArtifactDelivery{}, fmt.Errorf("config.artifacts.nomad.origin must be private; public_dns=%v public_ingress=%v", delivery.Origin.PublicDNS, delivery.Origin.PublicIngress)
	}
	if delivery.Origin.Scheme != "https" {
		return indexArtifactDelivery{}, fmt.Errorf("config.artifacts.nomad.origin.scheme=%q: Nomad artifacts require https", delivery.Origin.Scheme)
	}
	if delivery.NomadGetter.Protocol != "s3" || !strings.HasPrefix(delivery.NomadGetter.SourcePrefix, "s3::https://") {
		return indexArtifactDelivery{}, fmt.Errorf("config.artifacts.nomad.nomad_getter.source_prefix=%q: expected s3::https://...", delivery.NomadGetter.SourcePrefix)
	}
	if delivery.Storage.Bucket == "" || delivery.Storage.KeyPrefix == "" {
		return indexArtifactDelivery{}, fmt.Errorf("config.artifacts.nomad.storage bucket and key_prefix are required")
	}
	options := make(map[string]string, len(delivery.NomadGetter.Options))
	for key, value := range delivery.NomadGetter.Options {
		options[key] = value
	}
	return indexArtifactDelivery{
		Kind:               delivery.Kind,
		Bucket:             delivery.Storage.Bucket,
		KeyPrefix:          delivery.Storage.KeyPrefix,
		GetterSourcePrefix: strings.TrimRight(delivery.NomadGetter.SourcePrefix, "/"),
		GetterOptions:      options,
		GetterCredentials: indexCredentials{
			Source:             delivery.NomadGetter.Credentials.Source,
			EnvironmentFile:    delivery.NomadGetter.Credentials.EnvironmentFile,
			AccessKeyIDEnv:     delivery.NomadGetter.Credentials.AccessKeyIDEnv,
			SecretAccessKeyEnv: delivery.NomadGetter.Credentials.SecretAccessKeyEnv,
		},
		PublisherCredentials: indexCredentials{
			Source:             delivery.Publisher.Credentials.Source,
			EnvironmentFile:    delivery.Publisher.Credentials.EnvironmentFile,
			AccessKeyIDEnv:     delivery.Publisher.Credentials.AccessKeyIDEnv,
			SecretAccessKeyEnv: delivery.Publisher.Credentials.SecretAccessKeyEnv,
		},
		ChecksumAlgorithm: delivery.NomadGetter.ChecksumAlgorithm,
		Public:            false,
		Origin: indexArtifactOrigin{
			Scheme:        delivery.Origin.Scheme,
			Hostname:      string(delivery.Origin.Hostname),
			Port:          int64(delivery.Origin.Port),
			Placement:     delivery.Origin.Placement,
			Resolution:    delivery.Origin.Resolution,
			ListenHost:    string(delivery.Origin.ListenHost),
			PublicDNS:     delivery.Origin.PublicDNS,
			PublicIngress: delivery.Origin.PublicIngress,
			CABundlePath:  delivery.Origin.TLS.CABundlePath,
		},
	}, nil
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

func nomadUnitOwners(components []projection.NamedMap) (map[string]string, error) {
	out := map[string]string{}
	for _, component := range components {
		deployment, _ := component.Value["deployment"].(map[string]any)
		supervisor, _ := deployment["supervisor"].(string)
		if supervisor != "nomad" {
			continue
		}
		units, err := workloadUnitValues(component)
		if err != nil {
			return nil, err
		}
		for _, unit := range units {
			unitName, err := cueString(unit, "name")
			if err != nil {
				return nil, fmt.Errorf("%s.workload.units.name: %w", component.Name, err)
			}
			if unitName == "" {
				return nil, fmt.Errorf("%s.workload.units: name required", component.Name)
			}
			owner := jobID(component.Name)
			out[unitName] = owner
			out[unitName+".service"] = owner
		}
	}
	return out, nil
}

func nomadDependencies(component projection.NamedMap, unitOwners map[string]string) ([]string, error) {
	units, err := workloadUnitValues(component)
	if err != nil {
		return nil, err
	}
	self := jobID(component.Name)
	deps := map[string]struct{}{}
	for _, unit := range units {
		for _, field := range []string{"after", "wants"} {
			values, err := cueStringList(unit, field)
			if err != nil {
				return nil, fmt.Errorf("%s.workload.units.%s: %w", component.Name, field, err)
			}
			for _, raw := range values {
				dep, ok := unitOwners[raw]
				if !ok || dep == self {
					continue
				}
				deps[dep] = struct{}{}
			}
		}
	}
	return sortedKeys(deps), nil
}

func workloadUnitValues(component projection.NamedMap) ([]cue.Value, error) {
	value := component.CUE.LookupPath(cue.ParsePath("workload.units"))
	if err := value.Err(); err != nil {
		return nil, fmt.Errorf("lookup %s.workload.units: %w", component.Name, err)
	}
	iter, err := value.List()
	if err != nil {
		return nil, fmt.Errorf("list %s.workload.units: %w", component.Name, err)
	}
	var units []cue.Value
	for iter.Next() {
		units = append(units, iter.Value())
	}
	if len(units) == 0 {
		return nil, fmt.Errorf("%s.workload.units: nomad supervisor requires at least one unit", component.Name)
	}
	return units, nil
}

func workloadUnitMaps(component projection.NamedMap) ([]map[string]any, error) {
	values, err := workloadUnitValues(component)
	if err != nil {
		return nil, err
	}
	units := make([]map[string]any, 0, len(values))
	for i, value := range values {
		var unit map[string]any
		if err := value.Decode(&unit); err != nil {
			return nil, fmt.Errorf("decode %s.workload.units[%d]: %w", component.Name, i, err)
		}
		units = append(units, unit)
	}
	return units, nil
}

func cueString(value cue.Value, field string) (string, error) {
	var out string
	if err := value.LookupPath(cue.ParsePath(field)).Decode(&out); err != nil {
		return "", err
	}
	return out, nil
}

func cueStringList(value cue.Value, field string) ([]string, error) {
	fieldValue := value.LookupPath(cue.ParsePath(field))
	if err := fieldValue.Err(); err != nil {
		return nil, err
	}
	var out []string
	if err := fieldValue.Decode(&out); err != nil {
		return nil, err
	}
	return out, nil
}

func sortedKeys(m map[string]struct{}) []string {
	out := make([]string, 0, len(m))
	for key := range m {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

func buildJobSpec(component projection.NamedMap, deployment map[string]any, resolver *resolver) (map[string]any, error) {
	units, err := workloadUnitMaps(component)
	if err != nil {
		return nil, err
	}

	taskGroups := make([]map[string]any, 0, len(units))
	for i, unit := range units {
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

	scoped := resolver.withSelf(component.Name)
	resolvedEnv := make(map[string]string, len(env))
	for _, key := range serviceenv.SortedKeys(env) {
		resolved, err := scoped.resolve(env[key])
		if err != nil {
			return nil, fmt.Errorf("%s.env.%s: %w", component.Name, key, err)
		}
		resolvedEnv[key] = resolved
	}
	envOut, templates, err := upstreamTemplates(resolvedEnv)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", component.Name, err)
	}

	count := int64(1)
	if c, ok := deployment["count"].(int64); ok && c > 0 {
		count = c
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
	dynamicPorts, _, err := dynamicPorts(component, endpoints, unit)
	if err != nil {
		return nil, err
	}
	services := taskServices(jobID(component.Name), dynamicPorts)

	// TaskGroup.Update is a runtime contract spliced onto the spec by
	// src/deployment-tooling/nomad/resolve_jobs.py from each component's
	// nomad-overrides.json before spec_sha256 is stamped. The renderer
	// reserves the key so the resolver always updates an existing
	// field rather than introducing one — this keeps the canonical-JSON
	// shape stable across topology-only changes.
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
		"Templates":   templates,
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
		"Name":  unitName,
		"Count": count,
		"Tasks": tasks,
		// Update is filled by src/deployment-tooling/nomad/resolve_jobs.py from
		// the per-component nomad-overrides.json; declared here so the
		// resolver overwrites an existing key rather than introducing one.
		"Update": nil,
	}
	if len(dynamicPorts) > 0 {
		group["Networks"] = []map[string]any{
			// host_network "loopback" is registered in nomad.hcl client
			// config and pins DynamicPorts to 127.0.0.1. The Nomad
			// service catalog's auto address-mode advertises 127.0.0.1
			// for `nomadService` template lookups; per-component
			// nftables rules remain the second layer of defence.
			{"Mode": "host", "DynamicPorts": dynamicPortsWithHostNetwork(dynamicPorts, "loopback")},
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

// dynamicPorts returns the DynamicPorts list for the unit's TaskGroup
// network stanza plus the primary endpoint label used for service
// registration. Each entry is `{Label: <ep>}` with no Value: Nomad
// allocates from its dynamic-port range and binds to host_network
// "loopback" (127.0.0.1) per the nomad.hcl client config.
//
// `_ map[string]any` keeps the caller-side signature stable; the
// renderer no longer consults endpoint.port for app components.
func dynamicPorts(component projection.NamedMap, _ map[string]any, unit map[string]any) ([]map[string]any, string, error) {
	ownedEndpoints, err := serviceenv.EndpointsForUnit(component, unit)
	if err != nil {
		return nil, "", err
	}
	if len(ownedEndpoints) == 0 {
		return nil, "", nil
	}
	dynamic := make([]map[string]any, 0, len(ownedEndpoints))
	primary := ""
	for _, name := range ownedEndpoints {
		dynamic = append(dynamic, map[string]any{"Label": name})
		if primary == "" || name == "public_http" {
			primary = name
		}
	}
	return dynamic, primary, nil
}

func dynamicPortsWithHostNetwork(dynamic []map[string]any, hostNetwork string) []map[string]any {
	out := make([]map[string]any, 0, len(dynamic))
	for _, p := range dynamic {
		copy := map[string]any{}
		for k, v := range p {
			copy[k] = v
		}
		copy["HostNetwork"] = hostNetwork
		out = append(out, copy)
	}
	return out
}

// taskServices emits one Nomad service registration per port label so
// `nomadService` template lookups resolve from any other Nomad job.
// AddressMode "auto" picks the host_network address (127.0.0.1) on
// the loopback host_network this renderer pins via dynamicPorts.
//
// Service.Name is RFC 1123-shaped (`<jobid>-<endpoint-with-dashes>`)
// because Nomad rejects underscores in service names. PortLabel
// keeps the CUE-style endpoint label since it is not RFC 1123-bound
// and matches the entry the network stanza emits.
func taskServices(jobID string, ports []map[string]any) []map[string]any {
	if len(ports) == 0 {
		return nil
	}
	services := make([]map[string]any, 0, len(ports))
	for _, p := range ports {
		label, _ := p["Label"].(string)
		services = append(services, map[string]any{
			"Name":        jobID + "-" + strings.ReplaceAll(label, "_", "-"),
			"PortLabel":   label,
			"Provider":    "nomad",
			"AddressMode": "auto",
		})
	}
	return services
}

// upstreamTemplates extracts cross-Nomad-component env references
// from the resolved env map into a Nomad `template` stanza. Each
// referenced env value contains one or more `__VERSELF_NSRV__*`
// sentinels left by the resolver; this routine rewrites them into
// `{{ range nomadService "..." }}{{ .Address }}:{{ .Port }}{{ end }}`
// fragments and writes one consolidated `secrets/upstreams.env`
// template that Nomad materialises into the task's runtime env.
//
// Returns the leftover env map (cross-Nomad refs removed) and a
// single-element template list (or nil if no refs were present).
func upstreamTemplates(env map[string]string) (map[string]string, []map[string]any, error) {
	if len(env) == 0 {
		return env, nil, nil
	}
	leftover := make(map[string]string, len(env))
	templated := map[string]string{}
	for key, value := range env {
		if !strings.Contains(value, nomadServicePrefix) {
			leftover[key] = value
			continue
		}
		expanded, err := expandNomadServiceMarkers(value)
		if err != nil {
			return nil, nil, fmt.Errorf("env %s: %w", key, err)
		}
		templated[key] = expanded
	}
	if len(templated) == 0 {
		return leftover, nil, nil
	}
	keys := make([]string, 0, len(templated))
	for key := range templated {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var body strings.Builder
	for _, key := range keys {
		body.WriteString(key)
		body.WriteByte('=')
		body.WriteString(templated[key])
		body.WriteByte('\n')
	}
	tmpl := []map[string]any{{
		"EmbeddedTmpl": body.String(),
		"DestPath":     "secrets/upstreams.env",
		"Envvars":      true,
		"ChangeMode":   "restart",
	}}
	return leftover, tmpl, nil
}

func expandNomadServiceMarkers(value string) (string, error) {
	expanded := nomadServiceMarkerRE.ReplaceAllStringFunc(value, func(match string) string {
		m := nomadServiceMarkerRE.FindStringSubmatch(match)
		if len(m) != 3 {
			return match
		}
		serviceName, kind := m[1], m[2]
		// `with`/`else` produces a parseable placeholder when the
		// referenced service has not registered yet — without the
		// fallback, mutually-dependent services (e.g. billing ↔
		// secrets-service) cycle on RequireURL("") at boot. The
		// placeholder `127.0.0.1:1` is a parseable but-deliberately-
		// unreachable target; the consumer fails individual calls
		// fast, and Nomad's template `change_mode = restart` cycles
		// the task once the real upstream registers.
		switch kind {
		case "addr":
			return `{{- with nomadService "` + serviceName + `" }}{{ range . }}{{ .Address }}:{{ .Port }}{{ end }}{{- else }}127.0.0.1:1{{- end }}`
		case "port":
			return `{{- with nomadService "` + serviceName + `" }}{{ range . }}{{ .Port }}{{ end }}{{- else }}1{{- end }}`
		}
		return match
	})
	if strings.Contains(expanded, nomadServicePrefix) {
		return "", fmt.Errorf("unexpanded nomad-service sentinel in %q", value)
	}
	return expanded, nil
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
// serviceenv-derived strings.
//
// Substrate-component endpoints (postgres, nats, temporal, ...) keep
// their CUE-pinned ports and resolve to a literal `127.0.0.1:NNNN`
// at render time.
//
// Nomad-supervised components have dynamic ports allocated by the
// scheduler and registered in Nomad's service catalog. Their
// references resolve in two ways:
//
//   - Self-references (the env's own component): substitute to
//     `${NOMAD_PORT_<ep>}` (or `127.0.0.1:${NOMAD_PORT_<ep>}` for
//     `.address`). Nomad's runtime expands `${NOMAD_PORT_*}` against
//     the alloc's port assignment when the task starts.
//
//   - Cross-component references: substitute to a sentinel that
//     buildTaskGroup later moves out of the Env map and into a
//     `Templates` stanza. Nomad's template engine resolves the
//     `nomadService` directive at runtime against the catalog and
//     materialises the result as task env, restarting the task on
//     change.
type resolver struct {
	spireAgentSocketPath string
	ansibleVars          map[string]string
	endpointAddrs        map[string]string // substrate addresses, literal
	endpointPorts        map[string]string // substrate ports, literal
	componentHosts       map[string]string // "topology_endpoints.<c>.host"
	nomadEndpoints       map[string]nomadEndpointRef
	selfComponent        string // jobID of the component currently being rendered
}

type nomadEndpointRef struct {
	component string // CUE component name
	endpoint  string // endpoint label
	isPort    bool   // true for `.port`, false for `.address`
}

// nomadServicePrefix marks an env value that referenced another
// Nomad-supervised component. buildTaskGroup detects the prefix,
// rewrites the value into a `nomadService` template, and moves the
// entry from Env into a Templates stanza.
const nomadServicePrefix = "__VERSELF_NSRV__"

// nomadServiceMarker forms one sentinel for the named cross-Nomad
// reference. The kind is "addr" or "port".
func nomadServiceMarker(component, endpoint, kind string) string {
	return nomadServicePrefix + nomadServiceName(component, endpoint) + "__" + kind + "__"
}

// nomadServiceName builds the catalog name a Nomad service registers
// under: `<jobid>-<endpoint-with-dashes>`. Nomad enforces RFC 1123
// (alphanumeric + dash only, ≤63 chars) on service names, so the
// endpoint's CUE-style underscores are normalised here. Port labels
// keep their original form since they are not RFC 1123-bound.
func nomadServiceName(component, endpoint string) string {
	return jobID(component) + "-" + strings.ReplaceAll(endpoint, "_", "-")
}

// nomadServiceMarkerRE captures the (service-name, kind) pair from
// any sentinel embedded in a resolved env value. The captured name
// is RFC 1123-shaped — letters, digits, dashes only.
var nomadServiceMarkerRE = regexp.MustCompile(`__VERSELF_NSRV__([a-z0-9-]+?)__(addr|port)__`)

func newResolver(loaded load.Loaded) (*resolver, error) {
	nomadSet := map[string]bool{}
	for compName, comp := range loaded.Topology.Components {
		supervisor, _ := comp.Deployment["supervisor"].(string)
		if supervisor == "nomad" {
			nomadSet[compName] = true
		}
	}
	r := &resolver{
		spireAgentSocketPath: loaded.Config.Spire.AgentSocketPath,
		ansibleVars:          map[string]string{},
		endpointAddrs:        map[string]string{},
		endpointPorts:        map[string]string{},
		componentHosts:       map[string]string{},
		nomadEndpoints:       map[string]nomadEndpointRef{},
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
			addrKey := "topology_endpoints." + compName + ".endpoints." + epName + ".address"
			portKey := "topology_endpoints." + compName + ".endpoints." + epName + ".port"
			if nomadSet[compName] {
				r.nomadEndpoints[addrKey] = nomadEndpointRef{component: compName, endpoint: epName, isPort: false}
				r.nomadEndpoints[portKey] = nomadEndpointRef{component: compName, endpoint: epName, isPort: true}
				continue
			}
			r.endpointAddrs[addrKey] = fmt.Sprintf("%s:%d", string(host), ep.Port)
			r.endpointPorts[portKey] = fmt.Sprintf("%d", ep.Port)
		}
	}
	return r, nil
}

// withSelf returns a resolver scoped to a single component being
// rendered. Self-references resolve to `${NOMAD_PORT_*}` runtime
// expressions; cross-Nomad references resolve to template sentinels.
func (r *resolver) withSelf(componentName string) *resolver {
	clone := *r
	clone.selfComponent = jobID(componentName)
	return &clone
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
		if endp, ok := r.nomadEndpoints[key]; ok {
			if r.selfComponent != "" && jobID(endp.component) == r.selfComponent {
				if endp.isPort {
					return "${NOMAD_PORT_" + endp.endpoint + "}"
				}
				return "127.0.0.1:${NOMAD_PORT_" + endp.endpoint + "}"
			}
			if endp.isPort {
				return nomadServiceMarker(endp.component, endp.endpoint, "port")
			}
			return nomadServiceMarker(endp.component, endp.endpoint, "addr")
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
