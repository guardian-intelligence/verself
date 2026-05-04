package edgecontract

import (
	"fmt"
	"sort"
	"strings"
)

const (
	DefaultSite   = "prod"
	PublicHAProxy = "public_haproxy"
)

type Config struct {
	RepoRoot string
	Site     string
}

type Inputs struct {
	Routes          string `json:"routes" yaml:"routes"`
	Endpoints       string `json:"endpoints" yaml:"endpoints"`
	Ops             string `json:"ops" yaml:"ops"`
	Clusters        string `json:"clusters" yaml:"clusters"`
	NomadIndex      string `json:"nomad_index" yaml:"nomad_index"`
	NomadJobsDir    string `json:"nomad_jobs_dir" yaml:"nomad_jobs_dir"`
	HAProxyDefaults string `json:"haproxy_defaults" yaml:"haproxy_defaults"`
}

type Outputs struct {
	HAProxyTemplate        string `json:"haproxy_template" yaml:"haproxy_template"`
	PublicHostsMap         string `json:"public_hosts_map" yaml:"public_hosts_map"`
	NomadUpstreamsConfig   string `json:"nomad_upstreams_config" yaml:"nomad_upstreams_config"`
	NomadUpstreamsTemplate string `json:"nomad_upstreams_template" yaml:"nomad_upstreams_template"`
}

func (o Outputs) GeneratedArtifacts(artifacts Artifacts) []GeneratedArtifact {
	return []GeneratedArtifact{
		{Name: "HAProxy template", Path: o.HAProxyTemplate, Content: artifacts.HAProxyTemplate},
		{Name: "public hosts map", Path: o.PublicHostsMap, Content: artifacts.PublicHostsMap},
		{Name: "Nomad upstreams config", Path: o.NomadUpstreamsConfig, Content: artifacts.NomadUpstreamsConfig},
		{Name: "Nomad upstreams template", Path: o.NomadUpstreamsTemplate, Content: artifacts.NomadUpstreamsTemplate},
	}
}

type BackendID string
type ServerGUID string
type UpstreamKey string

type OriginKind string
type SecurityProfile string
type ProxyProtocol string

const (
	OriginBrowser   OriginKind = "browser_origin"
	OriginOperator  OriginKind = "operator_origin"
	OriginProtocol  OriginKind = "protocol_origin"
	OriginPublicAPI OriginKind = "public_api_origin"

	SecurityAPI      SecurityProfile = "api"
	SecurityBrowser  SecurityProfile = "browser"
	SecurityNone     SecurityProfile = "none"
	SecurityOperator SecurityProfile = "operator"
	SecurityProtocol SecurityProfile = "protocol"

	ProxyHTTP ProxyProtocol = "http"
	ProxyH2C  ProxyProtocol = "h2c"
)

type Bundle struct {
	Inputs    Inputs
	Outputs   Outputs
	Plan      Plan
	Artifacts Artifacts
	Manifest  Manifest
	Issues    []string
}

type Plan struct {
	Site           string
	GatewayHost    string
	Domains        Domains
	Defaults       HAProxyDefaults
	Artifacts      ArtifactTopology
	Garage         GarageCluster
	Components     map[string]ComponentEndpoint
	NomadUpstreams []NomadService
	Routes         []PublicRoute
	Backends       []Backend
	Frontends      []HAProxyObject
	Servers        []HAProxyObject
	UpstreamKeys   []UpstreamKey
}

type Domains struct {
	Product string
	Company string
}

type HAProxyDefaults struct {
	ShmStatsFile  string   `yaml:"haproxy_shm_stats_file"`
	H2CComponents []string `yaml:"haproxy_h2c_components"`
}

type Artifacts struct {
	HAProxyTemplate        string
	PublicHostsMap         string
	NomadUpstreamsConfig   string
	NomadUpstreamsTemplate string
}

type GeneratedArtifact struct {
	Name    string
	Path    string
	Content string
}

type Manifest struct {
	Site           string          `json:"site" yaml:"site"`
	Inputs         Inputs          `json:"inputs" yaml:"inputs"`
	Outputs        Outputs         `json:"outputs" yaml:"outputs"`
	Frontends      []HAProxyObject `json:"frontends" yaml:"frontends"`
	Backends       []HAProxyObject `json:"backends" yaml:"backends"`
	Servers        []HAProxyObject `json:"servers" yaml:"servers"`
	Routes         []RouteManifest `json:"routes" yaml:"routes"`
	NomadUpstreams []NomadService  `json:"nomad_upstreams" yaml:"nomad_upstreams"`
	UpstreamKeys   []string        `json:"upstream_keys" yaml:"upstream_keys"`
	Summary        map[string]int  `json:"summary" yaml:"summary"`
}

type HAProxyObject struct {
	Kind string `json:"kind" yaml:"kind"`
	Name string `json:"name" yaml:"name"`
	GUID string `json:"guid" yaml:"guid"`
}

type RouteManifest struct {
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

type PublicRoute struct {
	FQDN       string
	Backend    BackendID
	Kind       OriginKind
	Component  string
	Interface  string
	Endpoint   string
	Paths      []string
	PathPrefix string
	BodyLimit  int
	Protocol   ProxyProtocol
	Upstream   UpstreamKey
	NomadJobID string
}

type Backend struct {
	ID        BackendID
	Profile   SecurityProfile
	BodyLimit int
	Target    BackendTarget
}

type BackendTarget struct {
	Upstream   UpstreamKey
	Protocol   ProxyProtocol
	ServerGUID ServerGUID
}

type NomadService struct {
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

type NomadEndpoint struct {
	ServiceName string
	ServiceID   string
	AllocID     string
	JobID       string
	Address     string
	Port        int
}

type RoutesFile struct {
	TopologyGateways map[string]Gateway `yaml:"topology_gateways"`
	TopologyRoutes   []TopologyRoute    `yaml:"topology_routes"`
}

type Gateway struct {
	Host string `yaml:"host"`
	Kind string `yaml:"kind"`
}

type TopologyRoute struct {
	BrowserCORS  string      `yaml:"browser_cors"`
	Gateway      string      `yaml:"gateway"`
	Host         string      `yaml:"host"`
	Kind         OriginKind  `yaml:"kind"`
	MaxBodyBytes int         `yaml:"max_body_bytes"`
	PathPrefix   string      `yaml:"path_prefix"`
	Paths        []string    `yaml:"paths"`
	To           RouteTarget `yaml:"to"`
	WAF          string      `yaml:"waf"`
	Zone         string      `yaml:"zone"`
}

type RouteTarget struct {
	Component string `yaml:"component"`
	Interface string `yaml:"interface"`
}

type EndpointsFile struct {
	TopologyEndpoints map[string]ComponentEndpoint `yaml:"topology_endpoints"`
}

type ComponentEndpoint struct {
	Host       string               `yaml:"host"`
	Endpoints  map[string]Endpoint  `yaml:"endpoints"`
	Interfaces map[string]Interface `yaml:"interfaces"`
}

type Endpoint struct {
	Address     string `yaml:"address"`
	BindAddress string `yaml:"bind_address"`
	Exposure    string `yaml:"exposure"`
	Host        string `yaml:"host"`
	ListenHost  string `yaml:"listen_host"`
	Port        int    `yaml:"port"`
	Protocol    string `yaml:"protocol"`
}

type Interface struct {
	Auth       string `yaml:"auth"`
	Endpoint   string `yaml:"endpoint"`
	Kind       string `yaml:"kind"`
	OpenAPI    string `yaml:"openapi"`
	PathPrefix string `yaml:"path_prefix"`
}

type OpsFile struct {
	CompanyDomain     string           `yaml:"company_domain"`
	VerselfDomain     string           `yaml:"verself_domain"`
	TopologyArtifacts ArtifactTopology `yaml:"topology_artifacts"`
}

type ArtifactTopology struct {
	Nomad NomadArtifact `yaml:"nomad"`
}

type NomadArtifact struct {
	Origin NomadArtifactOrigin `yaml:"origin"`
}

type NomadArtifactOrigin struct {
	Hostname   string `yaml:"hostname"`
	ListenHost string `yaml:"listen_host"`
	Port       int    `yaml:"port"`
}

type ClustersFile struct {
	TopologyClusters ClusterTopology `yaml:"topology_clusters"`
}

type ClusterTopology struct {
	Garage GarageCluster `yaml:"garage"`
}

type GarageCluster struct {
	Host  string       `yaml:"host"`
	Nodes []GarageNode `yaml:"nodes"`
}

type GarageNode struct {
	Instance int `yaml:"instance"`
	S3Port   int `yaml:"s3_port"`
}

type NomadIndex struct {
	Components []IndexComponent `json:"components"`
}

type IndexComponent struct {
	Component string `json:"component"`
	JobID     string `json:"job_id"`
}

type NomadJobFile struct {
	Job NomadJob `json:"Job"`
}

type NomadJob struct {
	ID         string           `json:"ID"`
	TaskGroups []NomadTaskGroup `json:"TaskGroups"`
}

type NomadTaskGroup struct {
	Name     string         `json:"Name"`
	Networks []NomadNetwork `json:"Networks"`
	Tasks    []NomadTask    `json:"Tasks"`
}

type NomadNetwork struct {
	DynamicPorts []NomadDynamicPort `json:"DynamicPorts"`
}

type NomadDynamicPort struct {
	HostNetwork string `json:"HostNetwork"`
	Label       string `json:"Label"`
}

type NomadTask struct {
	Name     string         `json:"Name"`
	Services []NomadTaskSvc `json:"Services"`
}

type NomadTaskSvc struct {
	AddressMode string `json:"AddressMode"`
	Name        string `json:"Name"`
	PortLabel   string `json:"PortLabel"`
	Provider    string `json:"Provider"`
}

func RouteBackendID(route TopologyRoute) BackendID {
	host := strings.ReplaceAll(route.Host, "@", "apex")
	host = strings.ReplaceAll(host, ".", "_")
	zone := strings.ReplaceAll(route.Zone, "-", "_")
	return BackendID("be_route_" + zone + "_" + host + "_" + route.To.Component + "_" + route.To.Interface)
}

func DynamicServerGUID(backend BackendID) ServerGUID {
	return ServerGUID(string(backend) + "_srv_dyn")
}

func StaticServerGUID(backend BackendID, server string) ServerGUID {
	return ServerGUID(string(backend) + "_srv_" + server)
}

func ComponentUpstreamKey(component, endpoint string) UpstreamKey {
	return UpstreamKey("VERSELF_UPSTREAM_" + strings.ToUpper(component) + "_" + strings.ToUpper(endpoint))
}

func NomadServiceUpstreamKey(serviceName string) UpstreamKey {
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
	return UpstreamKey("VERSELF_UPSTREAM_" + strings.ToUpper(strings.ReplaceAll(serviceName, "-", "_")))
}

func routeFQDN(route TopologyRoute, domains Domains) string {
	domain := domains.Product
	if route.Zone == "company" {
		domain = domains.Company
	}
	if route.Host == "@" {
		return domain
	}
	return strings.ToLower(route.Host + "." + domain)
}

func dedupeUpstreamKeys(keys []UpstreamKey) []UpstreamKey {
	seen := map[UpstreamKey]struct{}{}
	for _, key := range keys {
		if key != "" {
			seen[key] = struct{}{}
		}
	}
	out := make([]UpstreamKey, 0, len(seen))
	for key := range seen {
		out = append(out, key)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i] < out[j]
	})
	return out
}

func haproxyObject(kind, name string) HAProxyObject {
	return HAProxyObject{Kind: kind, Name: name, GUID: name}
}

func issuef(issues *[]string, format string, args ...any) {
	*issues = append(*issues, fmt.Sprintf(format, args...))
}
