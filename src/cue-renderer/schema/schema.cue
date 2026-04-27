package schema

#Host:        string & !=""
#ServiceHost: #Host & !="0.0.0.0" & !="::"
#Port:        int & >=1 & <=65535 & !=4245 & !=4247

#ComponentKind: "service" | "frontend" | "resource" | "protocol_backend" | "privileged_daemon"
#Protocol:      "http" | "https" | "grpc" | "tcp" | "smtp" | "ssh" | "statsd" | "clickhouse_native"
#Exposure:      "loopback" | "public" | "wireguard" | "guest_host"

#Artifact: {
	kind:         "go_binary" | "node_app" | "upstream_binary" | "static_binary" | "guest_rootfs" | "none" | *"none"
	package:      string | *""
	output:       string | *""
	role:         string | *""
	bazel_label?: string & =~"^//"
	...
}

#Runtime: {
	systemd:   string | *""
	user:      string | *""
	group:     string | *""
	spiffe_id: string | *""
	...
}

#Process: {
	systemd: string & !=""
	user:    string & !=""
	group:   string & !=""

	artifact: #Artifact

	endpoints: [...string] | *[]
	identities: [...string] | *[]
	supplementary_groups: [...string] | *[]
	after: [...string] | *[]
	wants: [...string] | *[]
	environment: {[string]: string} | *{}
	privileged: bool | *false
	restart_units: [...string] | *[systemd]
	readiness_endpoint?:   string
	requires_spiffe_sock?: bool | *false
	...
}

#UIDPolicy: {
	kind: "allocated"
} | {
	kind:  "fixed"
	value: int & >0
}

#WorkloadIdentity: {
	path:        string & =~"^/"
	ansible_var: string | *""
	entry_id:    string & !=""
	user:        string & !=""
	group:       string & !=""
	uid_policy: #UIDPolicy | *{kind: "allocated"}
	selector:              "unix:uid" | *"unix:uid"
	x509_svid_ttl_seconds: int & >0 | *3600
	restart_units: [...string]
	...
}

#PostgresBinding: {
	database:         string | *""
	owner:            string | *""
	connection_limit: int & >=0 | *0
	...
}

#ElectricSync: {
	instance:         string & !=""
	pg_role:          string & !=""
	pg_conn_limit:    int & >0
	source_database:  string & !=""
	writer_role:      string & !=""
	publication_name: string & !=""
	publication_tables: [...string]
	storage_dir:           string & =~"^/"
	credstore_dir:         string & =~"^/"
	nftables_table:        string & !=""
	nftables_file:         string & =~"^/"
	db_pool_size:          int & >0
	replication_stream_id: string | *instance
	extra_systemd_after: [...string] | *[]
	...
}

#Endpoint: {
	protocol:    #Protocol
	host:        #ServiceHost | *"127.0.0.1"
	listen_host: #Host | *""
	port:        #Port
	exposure:    #Exposure
	if listen_host == "0.0.0.0" {
		wildcard_listen_reason: string & !="" @go(WildcardListenReason)
	}
	...
}

#Probe: {
	path: string & =~"^/"
	...
}

#Probes: {
	healthz?: #Probe
	readyz?:  #Probe
	...
}

#Interface: {
	kind:        "huma_api" | "frontend_http" | "resource_protocol" | "admin_api" | "metrics" | "protocol" | "guest_bootstrap_api" | "privileged_daemon_api"
	endpoint:    string
	path_prefix: string | *""
	openapi:     string | *""
	auth:        "none" | "zitadel_jwt" | "spiffe_mtls" | "shared_secret" | "operator" | *"none"
	probes?:     #Probes
	...
}

#Gateway: {
	kind: "caddy" | "firecracker_host" | "direct"
	host: #Host | *""
	...
}

#Target: {
	component: string
	interface: string
	...
}

#Route: {
	kind:        "browser_origin" | "public_api_origin" | "protocol_origin" | "operator_origin" | "guest_host_route" | "webhook_route"
	gateway:     string
	zone:        "product" | "company" | *"product"
	host:        string | *""
	path_prefix: string | *""
	paths: [...string] | *[]
	to:             #Target
	waf:            "blocking" | "detection" | "off" | *"off"
	max_body_bytes: int | *0
	browser_cors:   "none" | "same_origin" | "not_browser_reachable" | *"not_browser_reachable"
	...
}

#Edge: {
	from:    string
	to:      #Target
	auth:    "none" | "zitadel_jwt" | "spiffe_mtls" | "shared_secret" | "operator"
	purpose: string | *""
	...
}

#Policy: {
	kind:   "waf" | "csp" | "nftables" | "spire" | "body_limit"
	values: _
	...
}

#WireGuardPeer: {
	public_key:  string & !="" @go(PublicKey)
	allowed_ips: string & !=""
}

#WireGuardTunnel: {
	interface:      string & !=""
	port:           #Port
	network:        string & !=""
	address:        #Host
	address_prefix: int & >=0 & <=128 @go(AddressPrefix)
	peers: [...#WireGuardPeer]
}

#WireGuardConfig: {
	tunnels: {
		[string]: #WireGuardTunnel
		...
	}
	host_groups: {
		[string]: [...string]
		...
	} @go(HostGroups)
}

#RetiredRuntime: {
	unit:  string & !=""
	user:  string & !=""
	group: string & !=""
	paths: [...string & =~"^/"]
}

#PostgresConfig: {
	max_connections:                int & >0  @go(MaxConnections)
	superuser_reserved_connections: int & >=0 @go(SuperuserReservedConnections)
}

#NftablesSSHConfig: {
	public: bool | *true
	rate:   string & !="" | *"3/minute"
	burst:  int & >0 | *5
}

#NftablesConfig: {
	// Listener ports owned by substrate components not yet modeled as CUE
	// endpoints. Component endpoints with exposure=public are added by the
	// renderer.
	public_tcp_ports: [...#Port] | *[80, 443] @go(PublicTCPPorts)
	ssh: #NftablesSSHConfig
}

#NftablesEndpointRef: {
	component: string & !=""
	endpoint:  string & !=""
}

#NftablesSkuid: (string & =~"^[A-Za-z_][A-Za-z0-9_-]*$") | (int & >=0)

#NftablesInputRule: {
	kind: "drop_non_loopback"
	endpoints: [...#NftablesEndpointRef]
}

#NftablesOutputRule: {
	kind: "accept_loopback_all"
} | {
	kind: "accept_loopback_endpoints"
	endpoints: [...#NftablesEndpointRef]
	skuid?: #NftablesSkuid
} | {
	kind: "drop_loopback_endpoints"
	endpoints: [...#NftablesEndpointRef]
} | {
	kind:     "accept_port"
	protocol: "tcp" | "udp"
	port:     #Port
	oifname?: string & !=""
} | {
	kind:   "drop_ip_daddr_set"
	family: "ip" | "ip6"
	addrs: [...string & !=""]
} | {
	kind: "accept_non_tcp_udp"
}

#NftablesOutputChain: {
	user?:       #NftablesSkuid
	established: bool | *true
	final:       "drop" | "none" | *"drop"
	rules: [...#NftablesOutputRule]
}

#NftablesRuleset: {
	target:    string & =~"^/etc/nftables\\.d/[A-Za-z0-9._-]+\\.nft$"
	table:     string & =~"^[A-Za-z0-9_]+$"
	component: string & !="" | *""
	input: [...#NftablesInputRule] | *[]
	output?: #NftablesOutputChain
}

#NftablesTopology: {
	rulesets: {
		[string]: #NftablesRuleset
	}
}

#FirecrackerConfig: {
	guest_pool_cidr: string & !="" @go(GuestPoolCIDR)
}

#SpireConfig: {
	trust_domain:                 string & !=""   @go(TrustDomain)
	server_bind_address:          #Host           @go(ServerBindAddress)
	server_socket_path:           string & =~"^/" @go(ServerSocketPath)
	agent_socket_path:            string & =~"^/" @go(AgentSocketPath)
	workload_group:               string & !=""   @go(WorkloadGroup)
	agent_id_path:                string & =~"^/" @go(AgentIDPath)
	bundle_endpoint_bind_address: #Host           @go(BundleEndpointBindAddress)
}

#InstanceConfig: {
	verself_version: string & !=""   @go(VerselfVersion)
	verself_bin:     string & =~"^/" @go(VerselfBin)

	domains: {
		verself_domain:  string & !=""
		platform_domain: string & !=""
		company_domain:  string & !=""
		[string]:        string
	}

	openbao: {[string]: _}
	wireguard: #WireGuardConfig

	object_storage: {
		object_storage_service_uid: int & >0 @go(ObjectStorageServiceUID)
		object_storage_admin_uid:   int & >0 @go(ObjectStorageAdminUID)
	} @go(ObjectStorage)

	retired_product_runtimes: [...#RetiredRuntime] @go(RetiredProductRuntimes)
	postgres:    #PostgresConfig
	nftables:    #NftablesConfig
	firecracker: #FirecrackerConfig
	spire:       #SpireConfig
	temporal: {[string]: _}
	seed_system: {[string]: _} @go(SeedSystem)
}

#GarageNode: {
	instance:   int & >=0
	s3_port:    #Port @go(S3Port)
	rpc_port:   #Port @go(RPCPort)
	admin_port: #Port @go(AdminPort)
}

#GarageCluster: {
	instances: {
		count: int & >=1
		port_plan: {
			stride:     int & >0
			s3_base:    #Port @go(S3Base)
			rpc_base:   #Port @go(RPCBase)
			admin_base: #Port @go(AdminBase)
		} @go(PortPlan)
	}
	nodes: [...#GarageNode]
}

#TemporalRPCService: {
	grpc_port:       #Port @go(GRPCPort)
	membership_port: #Port @go(MembershipPort)
}

#TemporalFrontendService: {
	grpc_port:       #Port @go(GRPCPort)
	http_port:       #Port @go(HTTPPort)
	membership_port: #Port @go(MembershipPort)
}

#TemporalCluster: {
	frontend:          #TemporalFrontendService
	internal_frontend: #TemporalFrontendService @go(InternalFrontend)
	history:           #TemporalRPCService
	matching:          #TemporalRPCService
	worker:            #TemporalRPCService

	diagnostics: {
		metrics_port: #Port @go(MetricsPort)
		pprof_port:   #Port @go(PprofPort)
	}
}

#SmokeTestSpan: {
	name:      string
	kind:      string
	service:   string
	span_name: string @go(SpanName)
	attributes: {[string]: _} | *{}
}

#Component: {
	kind:        #ComponentKind
	host:        #ServiceHost | *"127.0.0.1"
	listen_host: #Host | *""
	artifact:    #Artifact
	runtime:     #Runtime
	endpoints: {
		[string]: #Endpoint
		...
	}
	interfaces: {
		[string]: #Interface
		...
	}
	identities: {
		[string]: #WorkloadIdentity
		...
	}
	tools?: {
		[string]: #Artifact
		...
	}
	processes?: {
		[string]: #Process
		...
	}
	probes: #Probes | *{}
	garage?:   #GarageCluster
	temporal?: #TemporalCluster
	postgres:  #PostgresBinding
	electric?: #ElectricSync
	...
}

#Topology: {
	components: {
		[string]: #Component
		...
	}
	gateways: {
		[string]: #Gateway
		...
	}
	routes: [...#Route]
	edges: [...#Edge]
	nftables: #NftablesTopology
	smoke_tests: {
		spans: [...#SmokeTestSpan] | *[]
	}
	policies: {
		[string]: #Policy
		...
	}
	...
}
