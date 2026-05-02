package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	defaultEdgeSite   = "prod"
	edgeManifestV1    = "verself.edge.v1"
	publicHAProxyGate = "public_haproxy"
)

var (
	haproxyGUIDRe            = regexp.MustCompile(`^[A-Za-z0-9_.:-]{1,127}$`)
	haproxySectionLineRe     = regexp.MustCompile(`(?m)^\s*(frontend|backend)\s+(\S+)\s*$`)
	haproxyServerLineRe      = regexp.MustCompile(`^\s*server\s+\S+`)
	haproxyServerGUIDRe      = regexp.MustCompile(`\bguid\s+([A-Za-z0-9_.:-]+)`)
	haproxyUpstreamKeyRe     = regexp.MustCompile(`str\((VERSELF_UPSTREAM_[A-Z0-9_]+)\),map\(`)
	haproxyNomadBackendKeyRe = regexp.MustCompile(`nomad_upstream_backend\('(VERSELF_UPSTREAM_[A-Z0-9_]+)'`)
	haproxyNomadBackendRe    = regexp.MustCompile(`nomad_upstream_backend\('VERSELF_UPSTREAM_[A-Z0-9_]+',\s*'[^']+',\s*'([A-Za-z0-9_.:-]+)'\)`)
	haproxyShmStatsFileRe    = regexp.MustCompile(`(?m)^\s*shm-stats-file\s+(.+?)\s*$`)
	haproxyExposeExpLineRe   = regexp.MustCompile(`(?m)^\s*expose-experimental-directives\s*$`)
)

type edgeConfig struct {
	repoRoot string
	site     string
	format   string
}

type edgeSources struct {
	Routes          string `json:"routes" yaml:"routes"`
	Endpoints       string `json:"endpoints" yaml:"endpoints"`
	Ops             string `json:"ops" yaml:"ops"`
	NomadIndex      string `json:"nomad_index" yaml:"nomad_index"`
	NomadJobsDir    string `json:"nomad_jobs_dir" yaml:"nomad_jobs_dir"`
	HAProxyDefaults string `json:"haproxy_defaults" yaml:"haproxy_defaults"`
	HAProxyTasks    string `json:"haproxy_tasks" yaml:"haproxy_tasks"`
	HAProxyTemplate string `json:"haproxy_template" yaml:"haproxy_template"`
}

type edgeManifest struct {
	Version                 string                `json:"version" yaml:"version"`
	Site                    string                `json:"site" yaml:"site"`
	Sources                 edgeSources           `json:"sources" yaml:"sources"`
	Frontends               []edgeHAProxyObject   `json:"frontends" yaml:"frontends"`
	Backends                []edgeHAProxyObject   `json:"backends" yaml:"backends"`
	Servers                 []edgeHAProxyObject   `json:"servers" yaml:"servers"`
	Routes                  []edgeRouteProjection `json:"routes" yaml:"routes"`
	NomadUpstreams          []edgeNomadService    `json:"nomad_upstreams" yaml:"nomad_upstreams"`
	TemplateUpstreamKeys    []string              `json:"template_upstream_keys" yaml:"template_upstream_keys"`
	SpecialBackendServerIDs []string              `json:"special_backend_server_guids" yaml:"special_backend_server_guids"`
	Summary                 map[string]int        `json:"summary" yaml:"summary"`
}

type edgeHAProxyObject struct {
	Kind string `json:"kind" yaml:"kind"`
	Name string `json:"name" yaml:"name"`
	GUID string `json:"guid" yaml:"guid"`
}

type edgeRouteProjection struct {
	FQDN       string `json:"fqdn" yaml:"fqdn"`
	Backend    string `json:"backend" yaml:"backend"`
	GUID       string `json:"guid" yaml:"guid"`
	Kind       string `json:"kind" yaml:"kind"`
	Component  string `json:"component" yaml:"component"`
	Interface  string `json:"interface" yaml:"interface"`
	Endpoint   string `json:"endpoint" yaml:"endpoint"`
	Upstream   string `json:"upstream,omitempty" yaml:"upstream,omitempty"`
	NomadJobID string `json:"nomad_job_id,omitempty" yaml:"nomad_job_id,omitempty"`
}

type edgeNomadService struct {
	Key          string `json:"key" yaml:"key"`
	ServiceName  string `json:"service_name" yaml:"service_name"`
	Component    string `json:"component" yaml:"component"`
	JobID        string `json:"job_id" yaml:"job_id"`
	TaskGroup    string `json:"task_group" yaml:"task_group"`
	Task         string `json:"task" yaml:"task"`
	PortLabel    string `json:"port_label" yaml:"port_label"`
	HostNetwork  string `json:"host_network" yaml:"host_network"`
	AddressMode  string `json:"address_mode" yaml:"address_mode"`
	Provider     string `json:"provider" yaml:"provider"`
	NomadDynamic bool   `json:"nomad_dynamic" yaml:"nomad_dynamic"`
}

type edgeRoutesFile struct {
	TopologyGateways map[string]edgeGateway `yaml:"topology_gateways"`
	TopologyRoutes   []edgeTopologyRoute    `yaml:"topology_routes"`
}

type edgeGateway struct {
	Host string `yaml:"host"`
	Kind string `yaml:"kind"`
}

type edgeTopologyRoute struct {
	BrowserCORS  string          `yaml:"browser_cors"`
	Gateway      string          `yaml:"gateway"`
	Host         string          `yaml:"host"`
	Kind         string          `yaml:"kind"`
	MaxBodyBytes int             `yaml:"max_body_bytes"`
	PathPrefix   string          `yaml:"path_prefix"`
	Paths        []string        `yaml:"paths"`
	To           edgeRouteTarget `yaml:"to"`
	WAF          string          `yaml:"waf"`
	Zone         string          `yaml:"zone"`
}

type edgeRouteTarget struct {
	Component string `yaml:"component"`
	Interface string `yaml:"interface"`
}

type edgeEndpointsFile struct {
	TopologyEndpoints map[string]edgeComponentEndpoints `yaml:"topology_endpoints"`
}

type edgeComponentEndpoints struct {
	Host       string                   `yaml:"host"`
	Endpoints  map[string]edgeEndpoint  `yaml:"endpoints"`
	Interfaces map[string]edgeInterface `yaml:"interfaces"`
}

type edgeEndpoint struct {
	Address     string `yaml:"address"`
	BindAddress string `yaml:"bind_address"`
	Exposure    string `yaml:"exposure"`
	Host        string `yaml:"host"`
	ListenHost  string `yaml:"listen_host"`
	Port        int    `yaml:"port"`
	Protocol    string `yaml:"protocol"`
}

type edgeInterface struct {
	Auth       string `yaml:"auth"`
	Endpoint   string `yaml:"endpoint"`
	Kind       string `yaml:"kind"`
	OpenAPI    string `yaml:"openapi"`
	PathPrefix string `yaml:"path_prefix"`
}

type edgeOpsFile struct {
	CompanyDomain string `yaml:"company_domain"`
	VerselfDomain string `yaml:"verself_domain"`
}

type edgeNomadIndex struct {
	Components []edgeIndexComponent `json:"components"`
}

type edgeIndexComponent struct {
	Component string `json:"component"`
	JobID     string `json:"job_id"`
}

type edgeNomadJobFile struct {
	Job edgeNomadJob `json:"Job"`
}

type edgeNomadJob struct {
	ID         string               `json:"ID"`
	Name       string               `json:"Name"`
	Type       string               `json:"Type"`
	TaskGroups []edgeNomadTaskGroup `json:"TaskGroups"`
}

type edgeNomadTaskGroup struct {
	Name     string             `json:"Name"`
	Networks []edgeNomadNetwork `json:"Networks"`
	Tasks    []edgeNomadTask    `json:"Tasks"`
}

type edgeNomadNetwork struct {
	Mode         string                 `json:"Mode"`
	DynamicPorts []edgeNomadDynamicPort `json:"DynamicPorts"`
}

type edgeNomadDynamicPort struct {
	HostNetwork string `json:"HostNetwork"`
	Label       string `json:"Label"`
}

type edgeNomadTask struct {
	Name     string             `json:"Name"`
	Services []edgeNomadTaskSvc `json:"Services"`
}

type edgeNomadTaskSvc struct {
	AddressMode string `json:"AddressMode"`
	Name        string `json:"Name"`
	PortLabel   string `json:"PortLabel"`
	Provider    string `json:"Provider"`
}

func cmdEdge(args []string) error {
	if len(args) == 0 {
		printEdgeUsage(os.Stderr)
		return fmt.Errorf("missing edge subcommand")
	}
	switch args[0] {
	case "check":
		return cmdEdgeCheck(args[1:])
	case "manifest":
		return cmdEdgeManifest(args[1:])
	case "-h", "--help", "help":
		printEdgeUsage(os.Stdout)
		return nil
	default:
		printEdgeUsage(os.Stderr)
		return fmt.Errorf("unknown edge subcommand: %s", args[0])
	}
}

func cmdEdgeCheck(args []string) error {
	cfg, err := parseEdgeFlags("edge check", args)
	if err != nil {
		return err
	}
	manifest, issues, err := buildEdgeManifest(cfg)
	if err != nil {
		return err
	}
	if len(issues) > 0 {
		for _, issue := range issues {
			fmt.Fprintln(os.Stderr, issue)
		}
		return exitError{code: 1}
	}
	_, _ = fmt.Fprintf(os.Stdout,
		"edge contract ok: site=%s routes=%d nomad_upstreams=%d template_upstream_keys=%d guid_objects=%d\n",
		manifest.Site,
		len(manifest.Routes),
		len(manifest.NomadUpstreams),
		len(manifest.TemplateUpstreamKeys),
		len(manifest.Frontends)+len(manifest.Backends)+len(manifest.Servers),
	)
	return nil
}

func cmdEdgeManifest(args []string) error {
	cfg, err := parseEdgeFlags("edge manifest", args)
	if err != nil {
		return err
	}
	manifest, issues, err := buildEdgeManifest(cfg)
	if err != nil {
		return err
	}
	if len(issues) > 0 {
		for _, issue := range issues {
			fmt.Fprintln(os.Stderr, issue)
		}
		return exitError{code: 1}
	}
	return writeEdgeManifest(os.Stdout, cfg.format, manifest)
}

func parseEdgeFlags(name string, args []string) (edgeConfig, error) {
	cfg := edgeConfig{
		site:   defaultEdgeSite,
		format: "text",
	}
	fs := flagSet(name)
	fs.StringVar(&cfg.repoRoot, "repo-root", "", "Path to the verself-sh checkout root.")
	fs.StringVar(&cfg.site, "site", defaultEdgeSite, "Deployment site whose Nomad jobs should be checked.")
	fs.StringVar(&cfg.format, "format", "text", "Manifest output format: text, json, or yaml.")
	if err := fs.Parse(args); err != nil {
		return edgeConfig{}, err
	}
	if fs.NArg() != 0 {
		return edgeConfig{}, fmt.Errorf("%s: unexpected positional args: %s", name, strings.Join(fs.Args(), " "))
	}
	if cfg.repoRoot == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return edgeConfig{}, fmt.Errorf("%s: cwd: %w", name, err)
		}
		cfg.repoRoot = cwd
	}
	abs, err := filepath.Abs(cfg.repoRoot)
	if err != nil {
		return edgeConfig{}, fmt.Errorf("%s: resolve --repo-root: %w", name, err)
	}
	cfg.repoRoot = abs
	if cfg.site == "" {
		return edgeConfig{}, fmt.Errorf("%s: --site is required", name)
	}
	switch cfg.format {
	case "text", "json", "yaml":
	default:
		return edgeConfig{}, fmt.Errorf("%s: unsupported --format=%s", name, cfg.format)
	}
	return cfg, nil
}

func printEdgeUsage(w *os.File) {
	_, _ = fmt.Fprint(w, `aspect-operator edge <subcommand> [flags]

Subcommands:
  check      Validate topology routes, Nomad services, HAProxy upstream keys, and GUIDs
  manifest   Emit the derived edge contract manifest

Common flags:
  --repo-root <path>  verself-sh checkout root
  --site <site>       deployment site (default: prod)
  --format <format>   manifest format: text, json, yaml
`)
}

func buildEdgeManifest(cfg edgeConfig) (*edgeManifest, []string, error) {
	sources := cfg.sources()
	var routesFile edgeRoutesFile
	if err := readYAMLFile(sources.Routes, &routesFile); err != nil {
		return nil, nil, err
	}
	var endpointsFile edgeEndpointsFile
	if err := readYAMLFile(sources.Endpoints, &endpointsFile); err != nil {
		return nil, nil, err
	}
	var opsFile edgeOpsFile
	if err := readYAMLFile(sources.Ops, &opsFile); err != nil {
		return nil, nil, err
	}
	var index edgeNomadIndex
	if err := readJSONFile(sources.NomadIndex, &index); err != nil {
		return nil, nil, err
	}
	templateBytes, err := os.ReadFile(sources.HAProxyTemplate)
	if err != nil {
		return nil, nil, fmt.Errorf("read %s: %w", sources.HAProxyTemplate, err)
	}
	template := string(templateBytes)
	defaultsBytes, err := os.ReadFile(sources.HAProxyDefaults)
	if err != nil {
		return nil, nil, fmt.Errorf("read %s: %w", sources.HAProxyDefaults, err)
	}
	defaults := string(defaultsBytes)
	tasksBytes, err := os.ReadFile(sources.HAProxyTasks)
	if err != nil {
		return nil, nil, fmt.Errorf("read %s: %w", sources.HAProxyTasks, err)
	}
	tasks := string(tasksBytes)

	issues := make([]string, 0)
	componentJobs := collectComponentJobs(index, &issues)
	nomadUpstreams, serviceByKey, err := collectNomadUpstreams(sources.NomadJobsDir, componentJobs, &issues)
	if err != nil {
		return nil, nil, err
	}

	manifest := &edgeManifest{
		Version:        edgeManifestV1,
		Site:           cfg.site,
		Sources:        sources,
		NomadUpstreams: sortNomadServices(nomadUpstreams),
		Summary:        map[string]int{},
	}

	validateHAProxyTemplate(sources, template, defaults, tasks, &issues)
	manifest.TemplateUpstreamKeys = collectTemplateUpstreamKeys(template, serviceByKey, &issues)
	manifest.SpecialBackendServerIDs = collectSpecialBackendServerGUIDs(template)
	manifest.Frontends = collectTemplateObjects("frontend", template)
	manifest.Backends = collectTemplateObjects("backend", template)
	manifest.Servers = collectTemplateServerObjects(template)
	for _, guid := range manifest.SpecialBackendServerIDs {
		manifest.Servers = append(manifest.Servers, edgeHAProxyObject{Kind: "server", Name: guid, GUID: guid})
	}

	routeObjects := deriveRouteObjects(routesFile, endpointsFile, opsFile, componentJobs, serviceByKey, &issues)
	for _, route := range routeObjects.routes {
		manifest.Routes = append(manifest.Routes, route)
		manifest.Backends = append(manifest.Backends, edgeHAProxyObject{Kind: "backend", Name: route.Backend, GUID: route.GUID})
	}
	manifest.Servers = append(manifest.Servers, routeObjects.servers...)

	sortEdgeObjects(manifest.Frontends)
	sortEdgeObjects(manifest.Backends)
	sortEdgeObjects(manifest.Servers)
	sort.Slice(manifest.Routes, func(i, j int) bool {
		if manifest.Routes[i].FQDN == manifest.Routes[j].FQDN {
			return manifest.Routes[i].Backend < manifest.Routes[j].Backend
		}
		return manifest.Routes[i].FQDN < manifest.Routes[j].FQDN
	})
	validateGUIDObjects(manifest, &issues)

	manifest.Summary["frontends"] = len(manifest.Frontends)
	manifest.Summary["backends"] = len(manifest.Backends)
	manifest.Summary["servers"] = len(manifest.Servers)
	manifest.Summary["public_routes"] = len(manifest.Routes)
	manifest.Summary["nomad_upstreams"] = len(manifest.NomadUpstreams)
	manifest.Summary["template_upstream_keys"] = len(manifest.TemplateUpstreamKeys)

	sort.Strings(issues)
	return manifest, issues, nil
}

func (cfg edgeConfig) sources() edgeSources {
	return edgeSources{
		Routes:          filepath.Join(cfg.repoRoot, "src/host-configuration/ansible/group_vars/all/generated/routes.yml"),
		Endpoints:       filepath.Join(cfg.repoRoot, "src/host-configuration/ansible/group_vars/all/generated/endpoints.yml"),
		Ops:             filepath.Join(cfg.repoRoot, "src/host-configuration/ansible/group_vars/all/generated/ops.yml"),
		NomadIndex:      filepath.Join(cfg.repoRoot, "src/deployment-tools/nomad/sites", cfg.site, "jobs/index.json"),
		NomadJobsDir:    filepath.Join(cfg.repoRoot, "src/deployment-tools/nomad/sites", cfg.site, "jobs"),
		HAProxyDefaults: filepath.Join(cfg.repoRoot, "src/host-configuration/ansible/roles/haproxy/defaults/main.yml"),
		HAProxyTasks:    filepath.Join(cfg.repoRoot, "src/host-configuration/ansible/roles/haproxy/tasks/main.yml"),
		HAProxyTemplate: filepath.Join(cfg.repoRoot, "src/host-configuration/ansible/roles/haproxy/templates/haproxy.cfg.j2"),
	}
}

func readYAMLFile(path string, target any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	if err := yaml.Unmarshal(b, target); err != nil {
		return fmt.Errorf("parse %s: %w", path, err)
	}
	return nil
}

func readJSONFile(path string, target any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	if err := json.Unmarshal(b, target); err != nil {
		return fmt.Errorf("parse %s: %w", path, err)
	}
	return nil
}

func writeEdgeManifest(w io.Writer, format string, manifest *edgeManifest) error {
	switch format {
	case "text":
		if _, err := fmt.Fprintf(w, "version: %s\nsite: %s\n", manifest.Version, manifest.Site); err != nil {
			return err
		}
		keys := make([]string, 0, len(manifest.Summary))
		for key := range manifest.Summary {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			if _, err := fmt.Fprintf(w, "%s: %d\n", key, manifest.Summary[key]); err != nil {
				return err
			}
		}
		return nil
	case "json":
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(manifest)
	case "yaml":
		enc := yaml.NewEncoder(w)
		if err := enc.Encode(manifest); err != nil {
			_ = enc.Close()
			return err
		}
		return enc.Close()
	default:
		return fmt.Errorf("unsupported manifest format %q", format)
	}
}

func collectComponentJobs(index edgeNomadIndex, issues *[]string) map[string]string {
	out := make(map[string]string, len(index.Components))
	for _, component := range index.Components {
		if component.Component == "" || component.JobID == "" {
			*issues = append(*issues, "nomad index component entries must include component and job_id")
			continue
		}
		if prior, exists := out[component.Component]; exists && prior != component.JobID {
			*issues = append(*issues, fmt.Sprintf("component %s maps to both Nomad job %s and %s", component.Component, prior, component.JobID))
			continue
		}
		out[component.Component] = component.JobID
	}
	return out
}

func collectNomadUpstreams(jobsDir string, componentJobs map[string]string, issues *[]string) ([]edgeNomadService, map[string]edgeNomadService, error) {
	entries, err := os.ReadDir(jobsDir)
	if err != nil {
		return nil, nil, fmt.Errorf("read %s: %w", jobsDir, err)
	}
	componentByJob := make(map[string]string, len(componentJobs))
	for component, jobID := range componentJobs {
		componentByJob[jobID] = component
	}
	var services []edgeNomadService
	byKey := map[string]edgeNomadService{}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".nomad.json") {
			continue
		}
		path := filepath.Join(jobsDir, entry.Name())
		var jobFile edgeNomadJobFile
		if err := readJSONFile(path, &jobFile); err != nil {
			return nil, nil, err
		}
		jobID := jobFile.Job.ID
		if jobID == "" {
			*issues = append(*issues, fmt.Sprintf("%s: Job.ID is required", path))
			continue
		}
		component := componentByJob[jobID]
		for _, group := range jobFile.Job.TaskGroups {
			dynamicPorts := collectDynamicPorts(group)
			for _, task := range group.Tasks {
				for _, svc := range task.Services {
					upstream, ok := nomadServiceToUpstream(jobID, component, group.Name, task.Name, svc, dynamicPorts, issues)
					if !ok {
						continue
					}
					if prior, exists := byKey[upstream.Key]; exists {
						*issues = append(*issues, fmt.Sprintf("Nomad services %s and %s both map to %s", prior.ServiceName, upstream.ServiceName, upstream.Key))
						continue
					}
					services = append(services, upstream)
					byKey[upstream.Key] = upstream
				}
			}
		}
	}
	return services, byKey, nil
}

func collectDynamicPorts(group edgeNomadTaskGroup) map[string]edgeNomadDynamicPort {
	out := map[string]edgeNomadDynamicPort{}
	for _, network := range group.Networks {
		for _, port := range network.DynamicPorts {
			if port.Label == "" {
				continue
			}
			out[port.Label] = port
		}
	}
	return out
}

func nomadServiceToUpstream(jobID, component, group, task string, svc edgeNomadTaskSvc, dynamicPorts map[string]edgeNomadDynamicPort, issues *[]string) (edgeNomadService, bool) {
	if svc.Name == "" {
		*issues = append(*issues, fmt.Sprintf("Nomad job %s task %s has a service registration without a name", jobID, task))
		return edgeNomadService{}, false
	}
	if svc.Provider != "" && svc.Provider != "nomad" {
		*issues = append(*issues, fmt.Sprintf("Nomad service %s uses provider %q; expected nomad", svc.Name, svc.Provider))
		return edgeNomadService{}, false
	}
	key := upstreamKeyFromNomadService(svc.Name)
	if key == "" {
		*issues = append(*issues, fmt.Sprintf("Nomad service %s cannot be converted to a VERSELF_UPSTREAM_* key", svc.Name))
		return edgeNomadService{}, false
	}
	port, ok := dynamicPorts[svc.PortLabel]
	if !ok {
		*issues = append(*issues, fmt.Sprintf("Nomad service %s uses port label %q without a matching dynamic port", svc.Name, svc.PortLabel))
		return edgeNomadService{}, false
	}
	if port.HostNetwork != "loopback" {
		*issues = append(*issues, fmt.Sprintf("Nomad service %s advertises host network %q; HAProxy upstreams must stay on loopback", svc.Name, port.HostNetwork))
	}
	return edgeNomadService{
		Key:          key,
		ServiceName:  svc.Name,
		Component:    component,
		JobID:        jobID,
		TaskGroup:    group,
		Task:         task,
		PortLabel:    svc.PortLabel,
		HostNetwork:  port.HostNetwork,
		AddressMode:  svc.AddressMode,
		Provider:     svc.Provider,
		NomadDynamic: true,
	}, true
}

func upstreamKeyFromNomadService(serviceName string) string {
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

func collectTemplateUpstreamKeys(template string, serviceByKey map[string]edgeNomadService, issues *[]string) []string {
	keys := mergeStringSets(
		uniqueMatches(template, haproxyUpstreamKeyRe),
		uniqueMatches(template, haproxyNomadBackendKeyRe),
	)
	for _, key := range keys {
		if _, ok := serviceByKey[key]; !ok {
			*issues = append(*issues, fmt.Sprintf("HAProxy template references %s but no Nomad service registration maps to that key", key))
		}
	}
	return keys
}

func mergeStringSets(groups ...[]string) []string {
	seen := map[string]struct{}{}
	for _, group := range groups {
		for _, value := range group {
			seen[value] = struct{}{}
		}
	}
	out := make([]string, 0, len(seen))
	for value := range seen {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func collectSpecialBackendServerGUIDs(template string) []string {
	return uniqueMatches(template, haproxyNomadBackendRe)
}

func uniqueMatches(input string, re *regexp.Regexp) []string {
	seen := map[string]struct{}{}
	for _, match := range re.FindAllStringSubmatch(input, -1) {
		if len(match) < 2 {
			continue
		}
		seen[match[1]] = struct{}{}
	}
	out := make([]string, 0, len(seen))
	for value := range seen {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

type edgeRouteObjects struct {
	routes  []edgeRouteProjection
	servers []edgeHAProxyObject
}

func deriveRouteObjects(routesFile edgeRoutesFile, endpointsFile edgeEndpointsFile, ops edgeOpsFile, componentJobs map[string]string, serviceByKey map[string]edgeNomadService, issues *[]string) edgeRouteObjects {
	if ops.VerselfDomain == "" {
		*issues = append(*issues, "ops.yml must define verself_domain")
	}
	if ops.CompanyDomain == "" {
		*issues = append(*issues, "ops.yml must define company_domain")
	}
	gateway, ok := routesFile.TopologyGateways[publicHAProxyGate]
	if !ok {
		*issues = append(*issues, "topology_gateways.public_haproxy is required")
	} else if gateway.Kind != "haproxy" {
		*issues = append(*issues, fmt.Sprintf("topology_gateways.public_haproxy.kind=%q; expected haproxy", gateway.Kind))
	}

	backendSeen := map[string]struct{}{}
	defaultHostSeen := map[string]string{}
	pathSeen := map[string]string{}
	var out edgeRouteObjects
	for _, route := range routesFile.TopologyRoutes {
		if route.Gateway != publicHAProxyGate {
			continue
		}
		fqdn := routeFQDN(route, ops)
		backend := routeBackendName(route)
		if _, exists := backendSeen[backend]; exists {
			*issues = append(*issues, fmt.Sprintf("duplicate HAProxy backend name %s derived from public routes", backend))
		}
		backendSeen[backend] = struct{}{}
		if len(route.Paths) == 0 {
			if prior := defaultHostSeen[fqdn]; prior != "" {
				*issues = append(*issues, fmt.Sprintf("public host %s has two default backends: %s and %s", fqdn, prior, backend))
			}
			defaultHostSeen[fqdn] = backend
		}
		for _, path := range route.Paths {
			if path == "" {
				*issues = append(*issues, fmt.Sprintf("route %s has an empty exact path", backend))
				continue
			}
			key := fqdn + "\x00" + path
			if prior := pathSeen[key]; prior != "" {
				*issues = append(*issues, fmt.Sprintf("public host %s path %s has two backends: %s and %s", fqdn, path, prior, backend))
			}
			pathSeen[key] = backend
		}
		projection := edgeRouteProjection{
			FQDN:      fqdn,
			Backend:   backend,
			GUID:      backend,
			Kind:      route.Kind,
			Component: route.To.Component,
			Interface: route.To.Interface,
		}
		component, iface, endpoint, ok := routeTargetEndpoint(route, endpointsFile, issues)
		if ok {
			projection.Endpoint = iface.Endpoint
			if jobID := componentJobs[route.To.Component]; jobID != "" {
				key := upstreamKeyFromRoute(route.To.Component, iface.Endpoint)
				projection.Upstream = key
				projection.NomadJobID = jobID
				service, exists := serviceByKey[key]
				if !exists {
					*issues = append(*issues, fmt.Sprintf("route %s targets Nomad component %s endpoint %s but %s is not registered by any authored Nomad job", backend, route.To.Component, iface.Endpoint, key))
				} else {
					if service.JobID != jobID {
						*issues = append(*issues, fmt.Sprintf("route %s expects %s from Nomad job %s but service %s belongs to %s", backend, key, jobID, service.ServiceName, service.JobID))
					}
					if service.PortLabel != iface.Endpoint {
						*issues = append(*issues, fmt.Sprintf("route %s expects endpoint %s but Nomad service %s uses port label %s", backend, iface.Endpoint, service.ServiceName, service.PortLabel))
					}
				}
			} else if component.Host == "" || endpoint.Address == "" {
				*issues = append(*issues, fmt.Sprintf("route %s targets non-Nomad component %s.%s without a concrete topology address", backend, route.To.Component, route.To.Interface))
			}
		}
		out.routes = append(out.routes, projection)
		for _, serverGUID := range routeServerGUIDs(route, backend) {
			out.servers = append(out.servers, edgeHAProxyObject{Kind: "server", Name: serverGUID, GUID: serverGUID})
		}
	}
	return out
}

func routeTargetEndpoint(route edgeTopologyRoute, endpointsFile edgeEndpointsFile, issues *[]string) (edgeComponentEndpoints, edgeInterface, edgeEndpoint, bool) {
	component, ok := endpointsFile.TopologyEndpoints[route.To.Component]
	if !ok {
		*issues = append(*issues, fmt.Sprintf("public route %s.%s targets missing component %s", route.Host, route.Zone, route.To.Component))
		return edgeComponentEndpoints{}, edgeInterface{}, edgeEndpoint{}, false
	}
	iface, ok := component.Interfaces[route.To.Interface]
	if !ok {
		*issues = append(*issues, fmt.Sprintf("public route %s.%s targets missing interface %s.%s", route.Host, route.Zone, route.To.Component, route.To.Interface))
		return component, edgeInterface{}, edgeEndpoint{}, false
	}
	endpoint, ok := component.Endpoints[iface.Endpoint]
	if !ok {
		*issues = append(*issues, fmt.Sprintf("public route %s.%s targets %s.%s endpoint %s, which is not defined", route.Host, route.Zone, route.To.Component, route.To.Interface, iface.Endpoint))
		return component, iface, edgeEndpoint{}, false
	}
	return component, iface, endpoint, true
}

func routeFQDN(route edgeTopologyRoute, ops edgeOpsFile) string {
	domain := ops.VerselfDomain
	if route.Zone == "company" {
		domain = ops.CompanyDomain
	}
	if route.Host == "@" {
		return domain
	}
	return strings.ToLower(route.Host + "." + domain)
}

func routeBackendName(route edgeTopologyRoute) string {
	host := strings.ReplaceAll(route.Host, "@", "apex")
	host = strings.ReplaceAll(host, ".", "_")
	zone := strings.ReplaceAll(route.Zone, "-", "_")
	return "be_route_" + zone + "_" + host + "_" + route.To.Component + "_" + route.To.Interface
}

func upstreamKeyFromRoute(component, endpoint string) string {
	return "VERSELF_UPSTREAM_" + strings.ToUpper(component) + "_" + strings.ToUpper(endpoint)
}

func routeServerGUIDs(route edgeTopologyRoute, backend string) []string {
	switch {
	case route.Kind == "operator_origin":
		return []string{backend + "_srv_pomerium"}
	case route.To.Component == "zitadel":
		return []string{backend + "_srv_zitadel"}
	case route.To.Component == "stalwart":
		return []string{backend + "_srv_mailbox", backend + "_srv_stalwart"}
	default:
		return []string{backend + "_srv_dyn"}
	}
}

func validateHAProxyTemplate(sources edgeSources, template, defaults, tasks string, issues *[]string) {
	if !haproxyExposeExpLineRe.MatchString(template) {
		*issues = append(*issues, sources.HAProxyTemplate+": global section must enable expose-experimental-directives for shm-stats-file")
	}
	if !haproxyShmStatsFileRe.MatchString(template) {
		*issues = append(*issues, sources.HAProxyTemplate+": global section must set shm-stats-file for reload-persistent counters")
	} else if match := haproxyShmStatsFileRe.FindStringSubmatch(template); len(match) == 2 {
		if strings.TrimSpace(match[1]) != "{{ haproxy_shm_stats_file }}" {
			*issues = append(*issues, sources.HAProxyTemplate+": shm-stats-file must reference haproxy_shm_stats_file so ownership is converged before root validation")
		}
	}
	if !strings.Contains(defaults, "haproxy_shm_stats_file: /var/lib/haproxy/") {
		*issues = append(*issues, sources.HAProxyDefaults+": haproxy_shm_stats_file must live under /var/lib/haproxy because the HAProxy systemd unit runs as the haproxy user")
	}
	requiredTaskLines := []string{
		`path: "{{ haproxy_shm_stats_file }}"`,
		"state: touch",
		"owner: haproxy",
		"group: haproxy",
		`mode: "0600"`,
		"access_time: preserve",
		"modification_time: preserve",
	}
	for _, line := range requiredTaskLines {
		if !strings.Contains(tasks, line) {
			*issues = append(*issues, sources.HAProxyTasks+": HAProxy shared-memory stats file must be precreated as haproxy:haproxy 0600 before config validation")
			break
		}
	}
	lines := strings.Split(template, "\n")
	for i, line := range lines {
		section := haproxySectionLineRe.FindStringSubmatch(line)
		if len(section) == 3 {
			if firstDirective := firstSectionDirective(lines, i+1); !strings.HasPrefix(firstDirective, "guid ") {
				*issues = append(*issues, fmt.Sprintf("%s:%d: %s %s must set guid as its first directive", sources.HAProxyTemplate, i+1, section[1], section[2]))
			}
		}
		trimmed := strings.TrimSpace(line)
		if haproxyServerLineRe.MatchString(line) && !strings.Contains(" "+trimmed+" ", " guid ") {
			*issues = append(*issues, fmt.Sprintf("%s:%d: server line must set guid", sources.HAProxyTemplate, i+1))
		}
	}
}

func firstSectionDirective(lines []string, start int) string {
	for _, line := range lines[start:] {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		return trimmed
	}
	return ""
}

func collectTemplateObjects(kind, template string) []edgeHAProxyObject {
	var objects []edgeHAProxyObject
	for _, match := range haproxySectionLineRe.FindAllStringSubmatch(template, -1) {
		if len(match) != 3 || match[1] != kind {
			continue
		}
		name := match[2]
		if strings.ContainsAny(name, "{}%") {
			continue
		}
		objects = append(objects, edgeHAProxyObject{Kind: kind, Name: name, GUID: name})
	}
	return objects
}

func collectTemplateServerObjects(template string) []edgeHAProxyObject {
	var out []edgeHAProxyObject
	for _, line := range strings.Split(template, "\n") {
		if !haproxyServerLineRe.MatchString(line) {
			continue
		}
		if guidOffset := strings.Index(line, " guid "); guidOffset >= 0 && strings.Contains(line[guidOffset:], "{{") {
			continue
		}
		match := haproxyServerGUIDRe.FindStringSubmatch(line)
		if len(match) != 2 {
			continue
		}
		guid := match[1]
		out = append(out, edgeHAProxyObject{Kind: "server", Name: guid, GUID: guid})
	}
	return out
}

func validateGUIDObjects(manifest *edgeManifest, issues *[]string) {
	seen := map[string]edgeHAProxyObject{}
	for _, object := range append(append(append([]edgeHAProxyObject{}, manifest.Frontends...), manifest.Backends...), manifest.Servers...) {
		if !haproxyGUIDRe.MatchString(object.GUID) {
			*issues = append(*issues, fmt.Sprintf("HAProxy %s %s has invalid guid %q", object.Kind, object.Name, object.GUID))
			continue
		}
		if prior, exists := seen[object.GUID]; exists {
			*issues = append(*issues, fmt.Sprintf("HAProxy guid %s is used by both %s %s and %s %s", object.GUID, prior.Kind, prior.Name, object.Kind, object.Name))
			continue
		}
		seen[object.GUID] = object
	}
}

func sortNomadServices(in []edgeNomadService) []edgeNomadService {
	sort.Slice(in, func(i, j int) bool {
		return in[i].Key < in[j].Key
	})
	return in
}

func sortEdgeObjects(in []edgeHAProxyObject) {
	sort.Slice(in, func(i, j int) bool {
		if in[i].Kind == in[j].Kind {
			return in[i].Name < in[j].Name
		}
		return in[i].Kind < in[j].Kind
	})
}
