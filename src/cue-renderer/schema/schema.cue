package schema

#Host:        string & !=""
#ServiceHost: #Host & !="0.0.0.0" & !="::"
#Port:        int & >=1 & <=65535 & !=4245 & !=4247
#FileMode:    string & =~"^0[0-7]{3}$"

#ComponentKind: "service" | "frontend" | "resource" | "protocol_backend" | "privileged_daemon"
#Protocol:      "http" | "https" | "grpc" | "tcp" | "smtp" | "ssh" | "statsd" | "clickhouse_native"
#Exposure:      "loopback" | "public" | "wireguard" | "guest_host"

#Artifact: {
	kind:         "go_binary" | "node_app" | "upstream_binary" | "static_binary" | "guest_rootfs" | "none" | *"none"
	package:      string | *""
	output:       string | *""
	role:         string | *""
	bazel_label?: string & =~"^//"
}

#Runtime: {
	systemd:   string | *""
	user:      string | *""
	group:     string | *""
	spiffe_id: string | *""
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
}

#PostgresBinding: {
	database:         string | *""
	owner:            string | *""
	connection_limit: int & >=0 | *0
	pool: {
		max_conns:                 int & >0 | *8    @go(MaxConns)
		min_conns:                 int & >=0 | *1   @go(MinConns)
		conn_max_lifetime_seconds: int & >0 | *1800 @go(ConnMaxLifetimeSeconds)
		conn_max_idle_seconds:     int & >0 | *300  @go(ConnMaxIdleSeconds)
	}
	password_ref: #PostgresPasswordRef | *{kind: "none"} @go(PasswordRef)
}

#PostgresPasswordRef: {
	kind: "none"
} | {
	kind:   "ansible_var"
	name:   string & !=""
	no_log: bool | *true
} | {
	kind:      "secret_ref"
	expose_as: string & !=""
	no_log:    bool | *true
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
}

#Probe: {
	path: string & =~"^/"
}

#Probes: {
	healthz?: #Probe
	readyz?:  #Probe
}

#Interface: {
	kind:        "huma_api" | "frontend_http" | "resource_protocol" | "admin_api" | "metrics" | "protocol" | "guest_bootstrap_api" | "privileged_daemon_api"
	endpoint:    string
	path_prefix: string | *""
	openapi:     string | *""
	auth:        "none" | "zitadel_jwt" | "spiffe_mtls" | "shared_secret" | "operator" | *"none"
	probes?:     #Probes
}

#Gateway: {
	kind: "caddy" | "firecracker_host" | "direct"
	host: #Host | *""
}

#Target: {
	component: string
	interface: string
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
}

#Edge: {
	from:    string
	to:      #Target
	auth:    "none" | "zitadel_jwt" | "spiffe_mtls" | "shared_secret" | "operator"
	purpose: string | *""
}

#Policy: {
	kind:   "waf" | "csp" | "nftables" | "spire" | "body_limit"
	values: _
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
	}
	host_groups: {
		[string]: [...string]
	} @go(HostGroups)
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
	// endpoints. Component endpoints with exposure=public are merged in
	// by the host_firewall.cue comprehension, not by the renderer.
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

#NftablesHostInputRule: {
	kind:    "accept_iifname"
	iifname: string & !=""
} | {
	kind:     "accept_guest_iifname_endpoints"
	iifname:  string & !=""
	saddr:    string & !=""
	daddr:    string & !=""
	protocol: "tcp" | "udp"
	endpoints: [...#NftablesEndpointRef]
} | {
	kind:   "accept_protocol_family"
	family: "icmp" | "icmpv6"
} | {
	kind:     "accept_port_set"
	protocol: "tcp" | "udp"
	ports: [...#Port] | *[]
	endpoints: [...#NftablesEndpointRef] | *[]
} | {
	kind:     "accept_rate_limited_port"
	protocol: "tcp" | "udp"
	port:     #Port
	meter:    string & !=""
	rate:     string & !=""
	burst:    int & >0
}

#NftablesHostInputChain: {
	accept_established_related: bool | *true @go(AcceptEstablishedRelated)
	drop_invalid:               bool | *true @go(DropInvalid)
	rules: [...#NftablesHostInputRule]
}

#NftablesHostChain: {
	target: string & =~"^/etc/nftables\\.d/[A-Za-z0-9._-]+\\.nft$"
	table:  string & =~"^[A-Za-z0-9_]+$"
	policy: "drop" | "accept" | *"drop"
	input:  #NftablesHostInputChain
}

// Firecracker guest networking: the FORWARD chain that routes
// guest egress out the uplink and blocks guest-to-guest, plus the
// POSTROUTING NAT chain that masquerades guest packets onto the
// uplink IP. The uplink interface is auto-resolved by the
// firecracker Ansible role at deploy time (some deployments take
// the default route's interface, others pin it explicitly), so the
// renderer emits a literal `__VERSELF_UPLINK__` placeholder and the
// Ansible role substitutes the resolved value with one `replace`
// task before reloading nftables.
#NftablesFirecrackerForwardRule:
	{kind: "guest_to_guest_drop"} |
	{
		kind:       "rate_limited_log_then_drop"
		protocol:   "tcp" | "udp"
		port:       #Port
		log_prefix: string & !="" @go(LogPrefix)
		rate:       string & !=""
	} |
	{kind: "guest_egress"} |
	{kind: "return_traffic"} |
	{kind: "catch_all_drop"}

#NftablesFirecrackerChain: {
	target:     string & =~"^/etc/nftables\\.d/[A-Za-z0-9._-]+\\.nft$"
	table:      string & =~"^[A-Za-z0-9_]+$"
	guest_cidr: string & !="" @go(GuestCIDR)
	uplink_placeholder: string & !="" | *"__VERSELF_UPLINK__" @go(UplinkPlaceholder)
	forward: [...#NftablesFirecrackerForwardRule]
}

#NftablesTopology: {
	host?:        #NftablesHostChain
	firecracker?: #NftablesFirecrackerChain
	rulesets: {
		[string]: #NftablesRuleset
	}
}

#FirecrackerConfig: {
	guest_pool_cidr: string & !="" @go(GuestPoolCIDR)

	// images declares the composable image zvols vm-orchestrator-cli seeds
	// at deploy time. Each entry projects to one ExecStart line in the
	// vm-orchestrator-seed.service oneshot unit; the daemon owns staging,
	// dd/mkfs, snapshotting, and dependent-clone teardown.
	images: [...#FirecrackerSeedImage] @go(Images)
}

#FirecrackerSeedImage: {
	ref: string & !="" @go(Ref)

	// tier names which layer this image belongs to. The seed oneshot
	// orders ExecStart= entries by tier so substrate is always materialized
	// before any toolchain image that might depend on its layout, and so
	// a future customer-uploaded image is structurally distinguishable
	// from the platform's own toolchains.
	tier:             "substrate" | "platform_toolchain" | "customer_uploaded" @go(Tier)
	size_bytes:       int & >0                                                  @go(SizeBytes)
	volblocksize:     string | *"16K"                                           @go(VolBlockSize)
	strategy:         "dd_from_file" | "mkfs_ext4"                              @go(Strategy)
	source_path:      string | *""                                              @go(SourcePath)
	filesystem_label: string | *""                                              @go(FilesystemLabel)
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
	// Typed config consumed by Go renderers. Each typed section has a
	// dedicated projection in internal/render/<name>/ and may declare its
	// own Ansible-vars projections by referencing fields from inside
	// ansible_vars below.
	wireguard:   #WireGuardConfig
	postgres:    #PostgresConfig
	nftables:    #NftablesConfig
	firecracker: #FirecrackerConfig
	spire:       #SpireConfig

	// ansible_vars is the explicit Ansible-vars surface. Every key here
	// becomes a top-level group_vars/all entry; the ops renderer projects
	// this map verbatim. Authors spell out the literal Ansible variable
	// name. Cross-section references are CUE values (type-checked) rather
	// than Jinja strings, so a typo surfaces at CUE evaluation time.
	ansible_vars: {[string]: _} @go(AnsibleVars)
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

#ComponentDirectory: {
	path:  string & =~"^/"
	owner: string & !=""
	group: string & !=""
	mode:  #FileMode
}

#SecretGeneration: {
	kind:   "password"
	length: int & >0
	chars:  string & !=""
}

#SecretRef: {
	name:   string & !=""
	path:   string & =~"^/"
	owner:  string & !=""
	group:  string & !=""
	mode:   #FileMode
	no_log: bool | *true @go(NoLog)
	restart_units: [...string] | *[] @go(RestartUnits)
	expose_as?: string & !="" @go(ExposeAs)
	source: {
		kind:     "generated"
		generate: #SecretGeneration
	} | {
		kind:        "ansible_var"
		ansible_var: string & !="" @go(AnsibleVar)
	} | {
		kind:       "remote_src"
		remote_src: string & =~"^/" @go(RemoteSrc)
	}
}

#ClickHouseGrant: {
	action: "INSERT" | "SELECT" | "ALTER" | "CREATE" | "DROP"
	table:  string & =~"^[A-Za-z0-9_]+\\.[A-Za-z0-9_]+$"
}

#ClickHouseBinding: {
	user:            string & !=""
	spiffe_identity: string & !="" @go(SpiffeIdentity)
	grants: [...#ClickHouseGrant] | *[]
}

#ZitadelProjectRole: {
	key:          string & !=""
	display_name: string & !="" @go(DisplayName)
	group:        string & !=""
}

#ZitadelAuth: {
	kind: "none"
} | {
	kind:                   "owned_project"
	project_name:           string & !="" @go(ProjectName)
	project_role_assertion: bool          @go(ProjectRoleAssertion)
	project_role_check:     bool          @go(ProjectRoleCheck)
	roles: [...#ZitadelProjectRole] | *[]
} | {
	kind:         "identity_project_audience"
	project_name: string & !="" @go(ProjectName)
}

#SystemdCredential: {
	name: string & !=""
	path: string & =~"^/"
}

#SystemdHardening: {
	capability_bounding_set: string | *""                                     @go(CapabilityBoundingSet)
	protect_home:            bool | *true                                     @go(ProtectHome)
	protect_system:          "strict" | "full" | "true" | "false" | *"strict" @go(ProtectSystem)
	private_devices:         bool | *true                                     @go(PrivateDevices)
	private_tmp:             bool | *true                                     @go(PrivateTmp)
	protect_clock:           bool | *true                                     @go(ProtectClock)
	protect_control_groups:  bool | *true                                     @go(ProtectControlGroups)
	protect_kernel_logs:     bool | *true                                     @go(ProtectKernelLogs)
	protect_kernel_modules:  bool | *true                                     @go(ProtectKernelModules)
	protect_kernel_tunables: bool | *true                                     @go(ProtectKernelTunables)
	lock_personality:        bool | *true                                     @go(LockPersonality)
	no_new_privileges:       bool | *true                                     @go(NoNewPrivileges)
	restrict_address_families: [...string] | *["AF_INET", "AF_INET6", "AF_UNIX"] @go(RestrictAddressFamilies)
	restrict_namespaces?:      bool                @go(RestrictNamespaces)
	restrict_realtime:         bool | *true        @go(RestrictRealtime)
	restrict_suid_sgid:        bool | *true        @go(RestrictSUIDSGID)
	system_call_architectures: string | *"native"  @go(SystemCallArchitectures)
	umask:                     #FileMode | *"0077" @go(UMask)
	read_write_paths: [...string & =~"^/"] | *[] @go(ReadWritePaths)
}

#ReadinessProbe: {
	kind:            "tcp"
	endpoint:        string & !=""
	timeout_seconds: int & >0 | *5 @go(TimeoutSeconds)
} | {
	kind:            "http"
	endpoint:        string & !=""
	path:            string & =~"^/"
	status_code:     int & >=100 & <=599 | *200 @go(StatusCode)
	timeout_seconds: int & >0 | *5              @go(TimeoutSeconds)
	scheme:          "http" | "https" | *"http"
	ca_path?:        string & =~"^/" @go(CAPath)
}

#SystemdUnit: {
	name:        string & !=""
	description: string & !=""
	user:        string & !=""
	group:       string & !=""
	uid?:        int & >0
	home:        string | *""
	create_home: bool | *false @go(CreateHome)
	exec:        string & !=""
	type:        "simple" | "exec" | "forking" | "oneshot" | "dbus" | "notify" | "idle" | *"simple"
	after: [...string] | *[]
	wants: [...string] | *[]
	requires: [...string] | *["verself-firewall.target"]
	supplementary_groups: [...string] | *[] @go(SupplementaryGroups)
	bind_read_only_paths: [...string] | *[] @go(BindReadOnlyPaths)
	load_credentials: [...#SystemdCredential] | *[] @go(LoadCredentials)
	environment: {[string]: string} | *{}
	restart:     "always" | "on-failure" | "no" | *"on-failure"
	restart_sec: int & >=0 | *5 @go(RestartSec)
	hardening: #SystemdHardening
	readiness: [...#ReadinessProbe] | *[]
	wanted_by: [...string] | *["multi-user.target"]                    @go(WantedBy)
	requires_spiffe_sock: bool | *false @go(RequiresSpiffeSock)
}

#SandboxGithubAppBootstrap: {
	enabled:      bool
	app_id:       string & =~"^[0-9]+$" @go(AppID)
	slug:         string & !=""
	client_id:    string & !=""          @go(ClientID)
	api_base_url: string & =~"^https://" @go(APIBaseURL)
	web_base_url: string & =~"^https://" @go(WebBaseURL)
}

#ComponentBootstrapConfig: {
	sandbox_github_app?: #SandboxGithubAppBootstrap @go(SandboxGithubApp)
}

#BootstrapHookName:
	"billing_stripe_webhook" |
	"identity_zitadel_actions" |
	"secrets_platform_org" |
	"openbao_tenancy" |
	"object_storage_tls" |
	"object_storage_garage_proxy" |
	"sandbox_vm_socket_acl"

#BootstrapHookClass:
	"external_provider" |
	"identity_provider" |
	"identity_lookup" |
	"secret_backend" |
	"substrate_bridge" |
	"storage_provider" |
	"security_audit"

#ComponentBootstrapHook: {
	name:   #BootstrapHookName
	class:  #BootstrapHookClass
	reason: string & !=""
}

#ComponentConverge: {
	enabled:    bool | *false
	deploy_tag: string | *"" @go(DeployTag)
	order:      int | *0
	directories: [...#ComponentDirectory] | *[]
	secret_refs: [...#SecretRef] | *[] @go(SecretRefs)
	clickhouse?: #ClickHouseBinding
	auth: #ZitadelAuth | *{kind: "none"}
	systemd: {
		units: [...#SystemdUnit] | *[]
	}
	bootstrap: [...#ComponentBootstrapHook] | *[]
	bootstrap_config: #ComponentBootstrapConfig | *{} @go(BootstrapConfig)
}

// #Deployment is the supervisor-shape contract for a component's runtime.
// supervisor selects between the legacy systemd path (default) and Nomad.
// The update / drain / resources knobs map directly to Nomad's JSON job
// spec (https://developer.hashicorp.com/nomad/api-docs/json-jobs); they
// are inert when supervisor == "systemd".
//
// Single rolling-restart is the only mode here. Blue/green and canary
// arrive as per-component CUE additions on top of Nomad's update {}
// stanza, not as enum knobs in the schema.
#Deployment: {
	supervisor: "systemd" | "nomad" | *"systemd"
	count:      int & >0 | *1
	update: {
		max_parallel:      int & >0 | *1                   @go(MaxParallel)
		min_healthy_time:  string & !="" | *"30s"          @go(MinHealthyTime)
		healthy_deadline:  string & !="" | *"5m"           @go(HealthyDeadline)
		progress_deadline: string & !="" | *"10m"          @go(ProgressDeadline)
		auto_revert:       bool | *true                    @go(AutoRevert)
	}
	drain: {
		kill_signal:  string & !="" | *"SIGTERM"           @go(KillSignal)
		kill_timeout: string & !="" | *"30s"               @go(KillTimeout)
	}
	resources: {
		cpu_mhz:   int & >0 | *500                         @go(CPUMHz)
		memory_mb: int & >0 | *256                         @go(MemoryMB)
	}
}

#Component: {
	kind:        #ComponentKind
	host:        #ServiceHost | *"127.0.0.1"
	listen_host: #Host | *""
	artifact:    #Artifact
	runtime:     #Runtime
	endpoints: {
		[string]: #Endpoint
	}
	interfaces: {
		[string]: #Interface
	}
	identities: {
		[string]: #WorkloadIdentity
	}
	tools?: {
		[string]: #Artifact
	}
	processes?: {
		[string]: #Process
	}
	probes?:    #Probes
	garage?:    #GarageCluster
	temporal?:  #TemporalCluster
	postgres:   #PostgresBinding
	electric?:  #ElectricSync
	converge:   #ComponentConverge | *{}
	deployment: #Deployment | *{}
}

#Topology: {
	components: {
		[string]: #Component
	}
	gateways: {
		[string]: #Gateway
	}
	routes: [...#Route]
	edges: [...#Edge]
	nftables: #NftablesTopology
	policies: {
		[string]: #Policy
	}
}
