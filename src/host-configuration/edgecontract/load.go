package edgecontract

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

type inputs struct {
	routes    RoutesFile
	endpoints EndpointsFile
	ops       OpsFile
	clusters  ClustersFile
	defaults  HAProxyDefaults
	index     NomadIndex
}

func DefaultSources(cfg Config) Sources {
	return Sources{
		Routes:              filepath.Join(cfg.RepoRoot, "src/host-configuration/ansible/group_vars/all/topology/routes.yml"),
		Endpoints:           filepath.Join(cfg.RepoRoot, "src/host-configuration/ansible/group_vars/all/topology/endpoints.yml"),
		Ops:                 filepath.Join(cfg.RepoRoot, "src/host-configuration/ansible/group_vars/all/topology/ops.yml"),
		Clusters:            filepath.Join(cfg.RepoRoot, "src/host-configuration/ansible/group_vars/all/topology/clusters.yml"),
		NomadIndex:          filepath.Join(cfg.RepoRoot, "src/deployment-tools/nomad/sites", cfg.Site, "jobs/index.json"),
		NomadJobsDir:        filepath.Join(cfg.RepoRoot, "src/deployment-tools/nomad/sites", cfg.Site, "jobs"),
		HAProxyDefaults:     filepath.Join(cfg.RepoRoot, "src/host-configuration/ansible/roles/haproxy/defaults/main.yml"),
		HAProxyTemplate:     filepath.Join(cfg.RepoRoot, "src/host-configuration/ansible/roles/haproxy/templates/haproxy.cfg.j2"),
		PublicHostsMap:      filepath.Join(cfg.RepoRoot, "src/host-configuration/ansible/roles/haproxy/templates/public-hosts.map.j2"),
		InitialUpstreamsMap: filepath.Join(cfg.RepoRoot, "src/host-configuration/ansible/roles/haproxy/templates/upstreams.map.j2"),
	}
}

func Build(cfg Config) (*Bundle, error) {
	sources := DefaultSources(cfg)
	in, err := loadInputs(sources)
	if err != nil {
		return nil, err
	}
	plan, issues, err := compilePlan(cfg, sources, in)
	if err != nil {
		return nil, err
	}
	artifacts := Artifacts{
		HAProxyTemplate:     RenderHAProxyTemplate(plan),
		PublicHostsMap:      RenderPublicHostsMap(plan),
		InitialUpstreamsMap: RenderInitialUpstreamsMap(plan),
	}
	manifest := BuildManifest(sources, plan)
	sort.Strings(issues)
	return &Bundle{
		Sources:   sources,
		Plan:      plan,
		Artifacts: artifacts,
		Manifest:  manifest,
		Issues:    issues,
	}, nil
}

func (b *Bundle) ArtifactIssues() []string {
	return artifactIssues(b.Sources, b.Artifacts)
}

func (b *Bundle) WriteArtifacts() error {
	if err := os.WriteFile(b.Sources.HAProxyTemplate, []byte(b.Artifacts.HAProxyTemplate), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", b.Sources.HAProxyTemplate, err)
	}
	if err := os.WriteFile(b.Sources.PublicHostsMap, []byte(b.Artifacts.PublicHostsMap), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", b.Sources.PublicHostsMap, err)
	}
	if err := os.WriteFile(b.Sources.InitialUpstreamsMap, []byte(b.Artifacts.InitialUpstreamsMap), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", b.Sources.InitialUpstreamsMap, err)
	}
	return nil
}

func loadInputs(sources Sources) (inputs, error) {
	var in inputs
	if err := readYAMLFile(sources.Routes, &in.routes); err != nil {
		return inputs{}, err
	}
	if err := readYAMLFile(sources.Endpoints, &in.endpoints); err != nil {
		return inputs{}, err
	}
	if err := readYAMLFile(sources.Ops, &in.ops); err != nil {
		return inputs{}, err
	}
	if err := readYAMLFile(sources.Clusters, &in.clusters); err != nil {
		return inputs{}, err
	}
	if err := readYAMLFile(sources.HAProxyDefaults, &in.defaults); err != nil {
		return inputs{}, err
	}
	if err := readJSONFile(sources.NomadIndex, &in.index); err != nil {
		return inputs{}, err
	}
	return in, nil
}

func compilePlan(cfg Config, sources Sources, in inputs) (Plan, []string, error) {
	var issues []string
	componentJobs := collectComponentJobs(in.index, &issues)
	nomadUpstreams, serviceByKey, err := collectNomadUpstreams(sources.NomadJobsDir, componentJobs, &issues)
	if err != nil {
		return Plan{}, nil, err
	}
	plan := Plan{
		Site:           cfg.Site,
		Domains:        Domains{Product: in.ops.VerselfDomain, Company: in.ops.CompanyDomain},
		Defaults:       in.defaults,
		Artifacts:      in.ops.TopologyArtifacts,
		Garage:         in.clusters.TopologyClusters.Garage,
		Components:     in.endpoints.TopologyEndpoints,
		NomadUpstreams: nomadUpstreams,
		Frontends: []HAProxyObject{
			haproxyObject("frontend", "fe_http"),
			haproxyObject("frontend", "fe_https"),
			haproxyObject("frontend", "fe_haproxy_metrics"),
			haproxyObject("frontend", "fe_firecracker_host_http"),
			haproxyObject("frontend", "fe_nomad_artifacts"),
		},
	}
	if in.ops.VerselfDomain == "" {
		issuef(&issues, "ops.yml must define verself_domain")
	}
	if in.ops.CompanyDomain == "" {
		issuef(&issues, "ops.yml must define company_domain")
	}
	if in.defaults.ShmStatsFile == "" {
		issuef(&issues, "haproxy defaults must define haproxy_shm_stats_file")
	} else if !strings.HasPrefix(in.defaults.ShmStatsFile, "/var/lib/haproxy/") {
		issuef(&issues, "haproxy_shm_stats_file must live under /var/lib/haproxy")
	}
	gateway, ok := in.routes.TopologyGateways[PublicHAProxy]
	if !ok {
		issuef(&issues, "topology_gateways.public_haproxy is required")
	} else {
		plan.GatewayHost = gateway.Host
		if gateway.Kind != "haproxy" {
			issuef(&issues, "topology_gateways.public_haproxy.kind=%q; expected haproxy", gateway.Kind)
		}
	}
	plan.Routes = compileRoutes(in.routes, in.endpoints, plan.Domains, componentJobs, serviceByKey, in.defaults, &issues)
	plan.Backends = compileBackends(plan)
	plan.Servers = compileServers(plan)
	plan.UpstreamKeys = compileUpstreamKeys(plan)
	sortPlan(&plan)
	validatePlan(plan, &issues)
	return plan, issues, nil
}

func compileRoutes(routesFile RoutesFile, endpointsFile EndpointsFile, domains Domains, componentJobs map[string]string, serviceByKey map[UpstreamKey]NomadService, defaults HAProxyDefaults, issues *[]string) []PublicRoute {
	backendSeen := map[BackendID]struct{}{}
	defaultHostSeen := map[string]BackendID{}
	pathSeen := map[string]BackendID{}
	var routes []PublicRoute
	for _, route := range routesFile.TopologyRoutes {
		if route.Gateway != PublicHAProxy {
			continue
		}
		fqdn := routeFQDN(route, domains)
		backend := RouteBackendID(route)
		if _, exists := backendSeen[backend]; exists {
			issuef(issues, "duplicate HAProxy backend name %s derived from public routes", backend)
		}
		backendSeen[backend] = struct{}{}
		if len(route.Paths) == 0 {
			if prior := defaultHostSeen[fqdn]; prior != "" {
				issuef(issues, "public host %s has two default backends: %s and %s", fqdn, prior, backend)
			}
			defaultHostSeen[fqdn] = backend
		}
		for _, path := range route.Paths {
			if path == "" {
				issuef(issues, "route %s has an empty exact path", backend)
				continue
			}
			key := fqdn + "\x00" + path
			if prior := pathSeen[key]; prior != "" {
				issuef(issues, "public host %s path %s has two backends: %s and %s", fqdn, path, prior, backend)
			}
			pathSeen[key] = backend
		}
		component, iface, endpoint, ok := routeTargetEndpoint(route, endpointsFile, issues)
		if !ok {
			routes = append(routes, PublicRoute{
				FQDN: fqdn, Backend: backend, Kind: route.Kind,
				Component: route.To.Component, Interface: route.To.Interface,
				Paths: route.Paths, PathPrefix: route.PathPrefix, BodyLimit: route.MaxBodyBytes,
			})
			continue
		}
		protocol := ProxyHTTP
		for _, h2c := range defaults.H2CComponents {
			if h2c == route.To.Component {
				protocol = ProxyH2C
				break
			}
		}
		compiled := PublicRoute{
			FQDN:       fqdn,
			Backend:    backend,
			Kind:       route.Kind,
			Component:  route.To.Component,
			Interface:  route.To.Interface,
			Endpoint:   iface.Endpoint,
			Paths:      route.Paths,
			PathPrefix: route.PathPrefix,
			BodyLimit:  route.MaxBodyBytes,
			Protocol:   protocol,
		}
		if jobID := componentJobs[route.To.Component]; jobID != "" {
			key := ComponentUpstreamKey(route.To.Component, iface.Endpoint)
			compiled.Upstream = key
			compiled.NomadJobID = jobID
			service, exists := serviceByKey[key]
			if !exists {
				issuef(issues, "route %s targets Nomad component %s endpoint %s but %s is not registered by any authored Nomad job", backend, route.To.Component, iface.Endpoint, key)
			} else {
				if service.JobID != jobID {
					issuef(issues, "route %s expects %s from Nomad job %s but service %s belongs to %s", backend, key, jobID, service.ServiceName, service.JobID)
				}
				if service.PortLabel != iface.Endpoint {
					issuef(issues, "route %s expects endpoint %s but Nomad service %s uses port label %s", backend, iface.Endpoint, service.ServiceName, service.PortLabel)
				}
			}
		} else if component.Host == "" || endpoint.Address == "" {
			issuef(issues, "route %s targets non-Nomad component %s.%s without a concrete topology address", backend, route.To.Component, route.To.Interface)
		}
		routes = append(routes, compiled)
	}
	return routes
}

func compileBackends(plan Plan) []Backend {
	backends := []Backend{
		{ID: "be_edge_public_rates", Profile: SecurityNone},
		{ID: "be_edge_auth_rates", Profile: SecurityNone},
		{ID: "be_edge_webhook_rates", Profile: SecurityNone},
		{ID: "be_not_found", Profile: SecurityNone},
		{ID: "be_forbidden", Profile: SecurityNone},
		{ID: "be_billing_stripe_webhook", Profile: SecurityAPI, BodyLimit: 65536, Target: dynamicTarget("VERSELF_UPSTREAM_BILLING_PUBLIC_HTTP", ProxyH2C, "be_billing_stripe_webhook_srv_dyn")},
		{ID: "be_sandbox_github_actions_webhook", Profile: SecurityAPI, BodyLimit: 1048576, Target: dynamicTarget("VERSELF_UPSTREAM_SANDBOX_RENTAL_PUBLIC_HTTP", ProxyH2C, "be_sandbox_github_actions_webhook_srv_dyn")},
		{ID: "be_sandbox_forgejo_actions_webhook", Profile: SecurityAPI, BodyLimit: 1048576, Target: dynamicTarget("VERSELF_UPSTREAM_SANDBOX_RENTAL_PUBLIC_HTTP", ProxyH2C, "be_sandbox_forgejo_actions_webhook_srv_dyn")},
		{ID: "be_sandbox_github_installation_callback", Profile: SecurityAPI, Target: dynamicTarget("VERSELF_UPSTREAM_SANDBOX_RENTAL_PUBLIC_HTTP", ProxyH2C, "be_sandbox_github_installation_callback_srv_dyn")},
		{ID: "be_sandbox_small_json_mutation", Profile: SecurityAPI, BodyLimit: 8192, Target: dynamicTarget("VERSELF_UPSTREAM_SANDBOX_RENTAL_PUBLIC_HTTP", ProxyH2C, "be_sandbox_small_json_mutation_srv_dyn")},
		{ID: "be_sandbox_execution_submit", Profile: SecurityAPI, BodyLimit: 65536, Target: dynamicTarget("VERSELF_UPSTREAM_SANDBOX_RENTAL_PUBLIC_HTTP", ProxyH2C, "be_sandbox_execution_submit_srv_dyn")},
		{ID: "be_sandbox_execution_schedule_create", Profile: SecurityAPI, BodyLimit: 65536, Target: dynamicTarget("VERSELF_UPSTREAM_SANDBOX_RENTAL_PUBLIC_HTTP", ProxyH2C, "be_sandbox_execution_schedule_create_srv_dyn")},
		{ID: "be_source_forgejo_webhook", Profile: SecurityAPI, BodyLimit: 1048576, Target: dynamicTarget("VERSELF_UPSTREAM_SOURCE_CODE_HOSTING_SERVICE_PUBLIC_HTTP", ProxyH2C, "be_source_forgejo_webhook_srv_dyn")},
		{ID: "be_zitadel_action_api_credentials", Profile: SecurityAPI, BodyLimit: 65536, Target: dynamicTarget("VERSELF_UPSTREAM_IDENTITY_SERVICE_PUBLIC_HTTP", ProxyH2C, "be_zitadel_action_api_credentials_srv_dyn")},
		{ID: "be_firecracker_sandbox_h2c", Profile: SecurityNone, Target: dynamicTarget("VERSELF_UPSTREAM_SANDBOX_RENTAL_PUBLIC_HTTP", ProxyH2C, "be_firecracker_sandbox_h2c_srv_dyn")},
		{ID: "be_firecracker_forgejo", Profile: SecurityNone},
		{ID: "be_garage_nomad_artifacts", Profile: SecurityNone},
	}
	for _, route := range plan.Routes {
		profile := profileForRoute(route)
		backends = append(backends, Backend{
			ID:        route.Backend,
			Profile:   profile,
			BodyLimit: route.BodyLimit,
			Target: BackendTarget{
				Upstream: route.Upstream,
				Protocol: route.Protocol,
			},
		})
	}
	return backends
}

func dynamicTarget(key UpstreamKey, protocol ProxyProtocol, serverGUID ServerGUID) BackendTarget {
	return BackendTarget{Upstream: key, Protocol: protocol, ServerGUID: serverGUID}
}

func profileForRoute(route PublicRoute) SecurityProfile {
	switch {
	case route.Kind == OriginPublicAPI:
		return SecurityAPI
	case route.Kind == OriginBrowser && route.Interface == "frontend":
		return SecurityBrowser
	case route.Kind == OriginBrowser:
		return SecurityAPI
	case route.Kind == OriginOperator:
		return SecurityOperator
	default:
		return SecurityProtocol
	}
}

func compileServers(plan Plan) []HAProxyObject {
	var servers []HAProxyObject
	add := func(guid ServerGUID) {
		if guid != "" {
			servers = append(servers, HAProxyObject{Kind: "server", Name: string(guid), GUID: string(guid)})
		}
	}
	for _, backend := range plan.Backends {
		add(backend.Target.ServerGUID)
	}
	add("be_firecracker_forgejo_srv_forgejo")
	for _, node := range plan.Garage.Nodes {
		add(ServerGUID(fmt.Sprintf("be_garage_nomad_artifacts_srv_garage_%d", node.Instance)))
	}
	for _, route := range plan.Routes {
		for _, guid := range routeServerGUIDs(route) {
			add(guid)
		}
	}
	return servers
}

func compileUpstreamKeys(plan Plan) []UpstreamKey {
	var keys []UpstreamKey
	for _, route := range plan.Routes {
		keys = append(keys, route.Upstream)
		if route.Component == "stalwart" {
			keys = append(keys, "VERSELF_UPSTREAM_MAILBOX_SERVICE_PUBLIC_HTTP")
		}
	}
	for _, backend := range plan.Backends {
		keys = append(keys, backend.Target.Upstream)
	}
	return dedupeUpstreamKeys(keys)
}

func routeServerGUIDs(route PublicRoute) []ServerGUID {
	switch {
	case route.Kind == OriginOperator:
		return []ServerGUID{StaticServerGUID(route.Backend, "pomerium")}
	case route.Component == "zitadel":
		return []ServerGUID{StaticServerGUID(route.Backend, "zitadel")}
	case route.Component == "stalwart":
		return []ServerGUID{StaticServerGUID(route.Backend, "mailbox"), StaticServerGUID(route.Backend, "stalwart")}
	default:
		return []ServerGUID{DynamicServerGUID(route.Backend)}
	}
}

func routeTargetEndpoint(route TopologyRoute, endpointsFile EndpointsFile, issues *[]string) (ComponentEndpoint, Interface, Endpoint, bool) {
	component, ok := endpointsFile.TopologyEndpoints[route.To.Component]
	if !ok {
		issuef(issues, "public route %s.%s targets missing component %s", route.Host, route.Zone, route.To.Component)
		return ComponentEndpoint{}, Interface{}, Endpoint{}, false
	}
	iface, ok := component.Interfaces[route.To.Interface]
	if !ok {
		issuef(issues, "public route %s.%s targets missing interface %s.%s", route.Host, route.Zone, route.To.Component, route.To.Interface)
		return component, Interface{}, Endpoint{}, false
	}
	endpoint, ok := component.Endpoints[iface.Endpoint]
	if !ok {
		issuef(issues, "public route %s.%s targets %s.%s endpoint %s, which is not defined", route.Host, route.Zone, route.To.Component, route.To.Interface, iface.Endpoint)
		return component, iface, Endpoint{}, false
	}
	return component, iface, endpoint, true
}

func collectComponentJobs(index NomadIndex, issues *[]string) map[string]string {
	out := make(map[string]string, len(index.Components))
	for _, component := range index.Components {
		if component.Component == "" || component.JobID == "" {
			issuef(issues, "nomad index component entries must include component and job_id")
			continue
		}
		if prior, exists := out[component.Component]; exists && prior != component.JobID {
			issuef(issues, "component %s maps to both Nomad job %s and %s", component.Component, prior, component.JobID)
			continue
		}
		out[component.Component] = component.JobID
	}
	return out
}

func collectNomadUpstreams(jobsDir string, componentJobs map[string]string, issues *[]string) ([]NomadService, map[UpstreamKey]NomadService, error) {
	entries, err := os.ReadDir(jobsDir)
	if err != nil {
		return nil, nil, fmt.Errorf("read %s: %w", jobsDir, err)
	}
	componentByJob := make(map[string]string, len(componentJobs))
	for component, jobID := range componentJobs {
		componentByJob[jobID] = component
	}
	var services []NomadService
	byKey := map[UpstreamKey]NomadService{}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".nomad.json") {
			continue
		}
		path := filepath.Join(jobsDir, entry.Name())
		var jobFile NomadJobFile
		if err := readJSONFile(path, &jobFile); err != nil {
			return nil, nil, err
		}
		jobID := jobFile.Job.ID
		if jobID == "" {
			issuef(issues, "%s: Job.ID is required", path)
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
					if prior, exists := byKey[UpstreamKey(upstream.Key)]; exists {
						issuef(issues, "Nomad services %s and %s both map to %s", prior.ServiceName, upstream.ServiceName, upstream.Key)
						continue
					}
					services = append(services, upstream)
					byKey[UpstreamKey(upstream.Key)] = upstream
				}
			}
		}
	}
	sort.Slice(services, func(i, j int) bool {
		return services[i].Key < services[j].Key
	})
	return services, byKey, nil
}

func collectDynamicPorts(group NomadTaskGroup) map[string]NomadDynamicPort {
	out := map[string]NomadDynamicPort{}
	for _, network := range group.Networks {
		for _, port := range network.DynamicPorts {
			if port.Label != "" {
				out[port.Label] = port
			}
		}
	}
	return out
}

func nomadServiceToUpstream(jobID, component, group, task string, svc NomadTaskSvc, dynamicPorts map[string]NomadDynamicPort, issues *[]string) (NomadService, bool) {
	if svc.Name == "" {
		issuef(issues, "Nomad job %s task %s has a service registration without a name", jobID, task)
		return NomadService{}, false
	}
	if svc.Provider != "" && svc.Provider != "nomad" {
		issuef(issues, "Nomad service %s uses provider %q; expected nomad", svc.Name, svc.Provider)
		return NomadService{}, false
	}
	key := NomadServiceUpstreamKey(svc.Name)
	if key == "" {
		issuef(issues, "Nomad service %s cannot be converted to a VERSELF_UPSTREAM_* key", svc.Name)
		return NomadService{}, false
	}
	port, ok := dynamicPorts[svc.PortLabel]
	if !ok {
		issuef(issues, "Nomad service %s uses port label %q without a matching dynamic port", svc.Name, svc.PortLabel)
		return NomadService{}, false
	}
	if port.HostNetwork != "loopback" {
		issuef(issues, "Nomad service %s advertises host network %q; HAProxy upstreams must stay on loopback", svc.Name, port.HostNetwork)
	}
	return NomadService{
		Key:          string(key),
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

func validatePlan(plan Plan, issues *[]string) {
	seen := map[string]HAProxyObject{}
	for _, object := range append(append(append([]HAProxyObject{}, plan.Frontends...), backendObjects(plan.Backends)...), plan.Servers...) {
		if object.GUID == "" {
			issuef(issues, "HAProxy %s %s has an empty guid", object.Kind, object.Name)
			continue
		}
		if prior, exists := seen[object.GUID]; exists {
			issuef(issues, "HAProxy guid %s is used by both %s %s and %s %s", object.GUID, prior.Kind, prior.Name, object.Kind, object.Name)
			continue
		}
		seen[object.GUID] = object
	}
}

func backendObjects(backends []Backend) []HAProxyObject {
	out := make([]HAProxyObject, 0, len(backends))
	for _, backend := range backends {
		out = append(out, haproxyObject("backend", string(backend.ID)))
	}
	return out
}

func sortPlan(plan *Plan) {
	sort.Slice(plan.Routes, func(i, j int) bool {
		if plan.Routes[i].FQDN == plan.Routes[j].FQDN {
			return plan.Routes[i].Backend < plan.Routes[j].Backend
		}
		return plan.Routes[i].FQDN < plan.Routes[j].FQDN
	})
	sort.Slice(plan.Backends, func(i, j int) bool {
		return plan.Backends[i].ID < plan.Backends[j].ID
	})
	sort.Slice(plan.Frontends, func(i, j int) bool {
		return plan.Frontends[i].Name < plan.Frontends[j].Name
	})
	sort.Slice(plan.Servers, func(i, j int) bool {
		return plan.Servers[i].Name < plan.Servers[j].Name
	})
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
