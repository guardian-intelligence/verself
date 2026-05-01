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
	unit:  string & !=""
	user:  string & !=""
	group: string & !=""

	artifact: #Artifact

	endpoints: [...string] | *[]
	identities: [...string] | *[]
	supplementary_groups: [...string] | *[]
	after: [...string] | *[]
	wants: [...string] | *[]
	environment: {[string]: string} | *{}
	privileged: bool | *false
	restart_units: [...string] | *[]
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

// Operator is a human (or human-equivalent automation) with one or more
// trusted devices. Each device's WireGuard pubkey is projected into the
// wg-ops peer list; each device receives short-lived SSH certs from the
// OpenBao SSH CA via OIDC. The device name flows into every cert's KeyID
// (`verself-<principal>-<device>`) so verself.host_auth_events can attribute
// any accepted SSH event to a specific device.
#OperatorDevice: {
	// Device name used in the cert KeyID, the wg AllowedIPs comment, and
	// the operator-side ~/.ssh/config.d/verself.conf alias. Lowercase
	// kebab — the regex matches what sshd prints into journald and what
	// the detect-recent-intrusions query parses out of cert_id.
	name:       string & =~"^[a-z][a-z0-9-]*$" & !="" @go(Name)
	wg_pubkey:  string & =~"^[A-Za-z0-9+/]{43}=$"     @go(WGPublicKey)
	wg_address: string & =~"^[0-9.]+$"                @go(WGAddress)
}

#Operator: {
	// Zitadel user_id (sub) of this operator. Optional today: a single
	// platform-org operator authenticates via OIDC with project-role
	// gating; per-user binding becomes mandatory when a second operator
	// joins.
	zitadel_user_id: string | *"" @go(ZitadelUserID)
	devices: {
		[string]: #OperatorDevice
	}
}

// Workload pool: pre-allocated WireGuard slots reserved for ephemeral
// workloads (Devin / Cursor / CI VMs). Slot priv/pub keys are generated
// once by the openbao role on first deploy and persisted in OpenBao KV
// at kv/workload-pool/slots/<index>/{wg-private-key,wg-public-key};
// claiming a slot is a metadata write, not a kernel reconfigure. Slot
// pubkeys flow into the wg-ops kernel config via a credstore file the
// wireguard role reads at template-render time. The pool size caps
// concurrent workloads — bump `slot_count` and redeploy to grow.
#WorkloadPool: {
	// Number of reserved slots. Slot indices run 0..slot_count-1.
	slot_count: int & >0 & <=64 @go(SlotCount)
	// First WireGuard address handed to slot index 0; subsequent slots
	// claim consecutive addresses (index N → base + N). Must not collide
	// with operator-device addresses or the wg-ops gateway address.
	slot_address_base: string & =~"^[0-9.]+$" @go(SlotAddressBase)
	// AppRole secret-id TTL minted by `aspect operator enroll-workload`.
	// 15 minutes covers a normal VM bring-up; the AppRole token (24h)
	// is what gates the workload's actual cert lifetime.
	enroll_secret_id_ttl_seconds: int & >0 & <=3600 | *900 @go(EnrollSecretIDTTLSeconds)
	// Workload Vault token TTL — caps the SSH cert lifetime issued by
	// the bootstrap binary (24h).
	workload_token_ttl_seconds: int & >0 & <=86400 | *86400 @go(WorkloadTokenTTLSeconds)
}

#WorkloadsConfig: {
	pool: #WorkloadPool
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

#NomadArtifactDelivery: {
	kind: "garage_s3_private_origin"

	storage: {
		provider:   "garage"
		bucket:     string & !=""
		key_prefix: string & =~"^[A-Za-z0-9][A-Za-z0-9._/-]*$" @go(KeyPrefix)
		region:     string & !=""
	}

	origin: {
		scheme:         "https"
		hostname:       #ServiceHost
		port:           #Port
		placement:      "node_local"
		resolution:     "per_node_hosts_file" | "private_dns"
		listen_host:    #Host @go(ListenHost)
		public_dns:     false @go(PublicDNS)
		public_ingress: false @go(PublicIngress)
		tls: {
			server_name:    #ServiceHost    @go(ServerName)
			ca_bundle_path: string & =~"^/" @go(CABundlePath)
		} @go(TLS)
	}

	nomad_getter: {
		protocol:           "s3"
		source_prefix:      string & =~"^s3::https://" @go(SourcePrefix)
		checksum_algorithm: "sha256"                   @go(ChecksumAlgorithm)
		options: {[string]: string}
		credentials: {
			source:                "host_environment"
			environment_file:      string & =~"^/" @go(EnvironmentFile)
			access_key_id_env:     string & !=""   @go(AccessKeyIDEnv)
			secret_access_key_env: string & !=""   @go(SecretAccessKeyEnv)
		}
	} @go(NomadGetter)

	publisher: {
		credentials: {
			source:                "controller_environment"
			environment_file:      string & =~"^/" @go(EnvironmentFile)
			access_key_id_env:     string & !=""   @go(AccessKeyIDEnv)
			secret_access_key_env: string & !=""   @go(SecretAccessKeyEnv)
		}
	}
}

#ArtifactConfig: {
	nomad: #NomadArtifactDelivery
}

#NftablesEndpointRef: {
	component: string & !=""
	endpoint:  string & !=""
}

#NftablesSkuid: (string & =~"^[A-Za-z_][A-Za-z0-9_-]*$") | (int & >=0)

#NftablesInputRule: {
	kind: "drop_non_loopback"
	endpoints: [...#NftablesEndpointRef]
} | {
	// accept_iifname_endpoints opens specific endpoint ports for traffic
	// arriving on a named ingress interface. The complementary
	// drop_non_loopback rule that follows it then drops everything else.
	// Used to expose a substrate API to wg-ops peers without putting it
	// on the public NIC.
	kind:    "accept_iifname_endpoints"
	iifname: string & !=""
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
	target:             string & =~"^/etc/nftables\\.d/[A-Za-z0-9._-]+\\.nft$"
	table:              string & =~"^[A-Za-z0-9_]+$"
	guest_cidr:         string & !=""                         @go(GuestCIDR)
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
	size_bytes:       int & >0                                                 @go(SizeBytes)
	volblocksize:     string | *"16K"                                          @go(VolBlockSize)
	strategy:         "dd_from_file" | "mkfs_ext4"                             @go(Strategy)
	source_path:      string | *""                                             @go(SourcePath)
	filesystem_label: string | *""                                             @go(FilesystemLabel)
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

// SSH CA: short-lived user-certificate issuance via OpenBao's ssh secrets
// engine, fronted by a Zitadel-OIDC auth method. The on-host sshd is
// configured with TrustedUserCAKeys + AuthorizedKeysFile=none so the
// only way to authenticate is to present a cert this CA signed.
//
// Each principal corresponds to one OpenBao SSH role plus one entry in
// /etc/ssh/principals/<default_user>. The schema enforces:
//   - source_address_cidrs is mandatory (no certs valid from "anywhere").
//   - max_ttl_seconds is bounded at 24h (a leaked cert can't outlive a day).
//   - automation principals must declare a non-empty force_command, since
//     a non-human issuer with an unrestricted shell defeats the point.
//   - workload principals (the bootstrap-token Devin/Cursor path) skip the
//     force_command requirement: they need a normal interactive shell,
//     bound by the AppRole-issued 24h Vault token TTL and wg-ops CIDR.
#SSHRoleKind: "operator" | "automation" | "workload" | "breakglass"

#SSHPrincipal: {
	name: string & !="" @go(Name)
	role: #SSHRoleKind  @go(Role)

	// Maximum cert lifetime in seconds. The CA refuses to sign for longer.
	// Hard ceiling 24h; operator certs are typically 15min, automation 60s.
	max_ttl_seconds: int & >0 & <=86400 @go(MaxTTLSeconds)

	// CIDRs the cert may be presented from. The CA stamps the source
	// constraint into the cert; sshd verifies on every connection.
	// The shape `[string, ...string]` requires at least one element so a
	// naked principal (no CIDRs) fails CUE evaluation at instance time.
	source_address_cidrs: [string & !="", ...string & !=""] @go(SourceAddressCIDRs)

	// force_command, when non-empty, replaces any user-requested command
	// with this one. permit_pty=false denies TTY allocation. Together
	// these constrain what an automation principal can do once
	// authenticated.
	force_command:          string | *""  @go(ForceCommand)
	permit_pty:             bool | *true  @go(PermitPty)
	permit_port_forwarding: bool | *false @go(PermitPortForwarding)

	// Automation principals MUST declare a force_command. Empty
	// force_command on automation fails CUE evaluation.
	if role == "automation" {
		force_command: !=""
	}
}

#SSHOIDCConfig: {
	// Zitadel discovery URL — the CA's OIDC auth method validates tokens
	// against this issuer's JWKS.
	discovery_url: string & =~"^https://" @go(DiscoveryURL)

	// Zitadel project that owns the SSH-CA OIDC application registration.
	// The zitadel_oauth_app role creates this if missing.
	project_name: string & !="" @go(ProjectName)

	// OpenBao OIDC auth method allows redirect URIs matching these.
	// `bao login -method=oidc` binds to one of these ports as the local
	// callback target; multiple ports tolerate "another bao login is
	// already holding 8250" without operator intervention.
	allowed_redirect_uris: [...string & =~"^http://localhost:[0-9]+/oidc/callback$"] @go(AllowedRedirectURIs)

	// Zitadel project role required for operator-cert issuance. The
	// OIDC method's role binds bound_claims["urn:zitadel:iam:org:project:roles"]
	// to this name; missing the role means no Vault token, means no cert.
	operator_project_role: string & !="" @go(OperatorProjectRole)
}

#SSHCAConfig: {
	// Display name used in the SSH cert authority field and OpenBao mount
	// description. Cosmetic; doesn't affect the trust chain.
	ca_name: string & !="" @go(CAName)

	// OpenBao mount path for the SSH secrets engine. All issuance API
	// calls are scoped under /v1/<mount>/sign/<role>.
	mount: string & =~"^[a-z][a-z0-9_-]*$" @go(Mount)

	// Default OS user the certs authorize on the host. Today this is
	// always "ubuntu" because the bare-metal box has no per-human Unix
	// accounts, only the role-based sudoer.
	default_user: string & !="" @go(DefaultUser)

	// Path on the host where the CA's public key is materialised. Pointed
	// at by sshd_config TrustedUserCAKeys.
	ca_pubkey_path: string & =~"^/" @go(CAPubkeyPath)

	// Path on the host where the per-user principals file is written. The
	// %u in sshd_config AuthorizedPrincipalsFile expands to default_user.
	principals_file: string & =~"^/" @go(PrincipalsFile)

	oidc: #SSHOIDCConfig

	// At least one principal must be declared; otherwise no cert can be
	// issued and the host is locked out by construction. CUE has no
	// "non-empty struct" shorthand the way it has for lists, so the
	// instance-level test in load_ssh_ca_test.go asserts an empty
	// principals map fails evaluation.
	principals: {
		[string]: #SSHPrincipal
	}
}

#BareMetalConfig: {
	// Public IPv4 address of the bare-metal node. Owned here (not in the
	// Ansible inventory) so the cloudflare_dns role and any future
	// public-DNS consumer read a typed CUE value rather than a string
	// shadow of `ansible_host`. Validated as v4 dotted-quad and refused
	// when it lands inside RFC 1918 / loopback ranges.
	public_ipv4: string & =~"^([0-9]+\\.){3}[0-9]+$" @go(PublicIPv4)
	// Internal alias used by ansible inventory and by the operator-side
	// generated ~/.ssh/config drop-in. SSH connects to this alias and the
	// drop-in maps it to the wg-ops address.
	host_alias: string & =~"^[a-z][a-z0-9-]*$" @go(HostAlias)
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
	ssh_ca:      #SSHCAConfig @go(SshCA)
	bare_metal:  #BareMetalConfig @go(BareMetal)
	artifacts:   #ArtifactConfig

	// operators declares the trusted set of human operators and their
	// devices. Devices project into wg-ops peers and into the
	// known-cert-id allowlist that detect-recent-intrusions consults.
	operators: {
		[string]: #Operator
	}

	// workloads.pool reserves WireGuard slots for ephemeral workloads
	// (Devin/Cursor/CI VMs). Slot priv keys live in OpenBao KV and are
	// generated on first deploy; slot pubkeys are read back and merged
	// into the wg-ops peer list at substrate convergence time.
	workloads: #WorkloadsConfig

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
	audience:               string & !="" @go(Audience)
	project_role_assertion: bool          @go(ProjectRoleAssertion)
	project_role_check:     bool          @go(ProjectRoleCheck)
	roles: [...#ZitadelProjectRole] | *[]
} | {
	kind:         "identity_project_audience"
	project_name: string & !="" @go(ProjectName)
	audience:     string & !="" @go(Audience)
}

#BrowserOIDCBootstrap: {
	app_name:     string & !="" @go(AppName)
	project_name: string & !="" @go(ProjectName)
	redirect_uris: [...string & =~"^https://"] @go(RedirectURIs)
	post_logout_redirect_uris: [...string & =~"^https://"] @go(PostLogoutRedirectURIs)
	credstore_dir:   string & =~"^/" @go(CredstoreDir)
	credstore_group: string & !=""   @go(CredstoreGroup)
	role_assertions: bool | *false   @go(RoleAssertions)
	grant_types: [...string & !=""] | *[
		"OIDC_GRANT_TYPE_AUTHORIZATION_CODE",
		"OIDC_GRANT_TYPE_REFRESH_TOKEN",
	] @go(GrantTypes)
	project_roles: [...#ZitadelProjectRole] | *[] @go(ProjectRoles)
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

#WorkloadUnit: {
	name:        string & !=""
	description: string & !=""
	user:        string & !=""
	group:       string & !=""
	uid?:        int & >0
	home:        string | *""
	create_home: bool | *false @go(CreateHome)
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
	hardening:   #SystemdHardening
	readiness: [...#ReadinessProbe] | *[]
	wanted_by: [...string] | *["multi-user.target"] @go(WantedBy)
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
	browser_oidc?:       #BrowserOIDCBootstrap      @go(BrowserOIDC)
}

#BootstrapHookName:
	"billing_stripe_webhook" |
	"identity_zitadel_actions" |
	"openbao_tenancy" |
	"object_storage_tls" |
	"object_storage_garage_proxy" |
	"sandbox_vm_socket_acl" |
	"browser_oidc"

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

#ComponentWorkload: {
	order: int | *0
	directories: [...#ComponentDirectory] | *[]
	secret_refs: [...#SecretRef] | *[] @go(SecretRefs)
	clickhouse?: #ClickHouseBinding
	auth: #ZitadelAuth | *{kind: "none"}
	// units describes the runnable processes Nomad manages. The executable
	// comes from the component or named process artifact; this block carries
	// runtime facts, environment, endpoint ownership, and substrate needs.
	units: [...#WorkloadUnit] | *[]
	bootstrap: [...#ComponentBootstrapHook] | *[]
	bootstrap_config: #ComponentBootstrapConfig | *{} @go(BootstrapConfig)
}

// #Deployment is the supervisor-shape contract for a component's runtime.
// The update / drain / resources knobs map directly to Nomad's JSON job
// spec (https://developer.hashicorp.com/nomad/api-docs/json-jobs).
//
// Single rolling-restart is the only mode here. Blue/green and canary
// arrive as per-component CUE additions on top of Nomad's update {}
// stanza, not as enum knobs in the schema.
#Deployment: {
	supervisor: "systemd" | "nomad" | *"systemd"
	count:      int & >0 | *1
	update: {
		max_parallel:      int & >0 | *1          @go(MaxParallel)
		min_healthy_time:  string & !="" | *"30s" @go(MinHealthyTime)
		healthy_deadline:  string & !="" | *"5m"  @go(HealthyDeadline)
		progress_deadline: string & !="" | *"10m" @go(ProgressDeadline)
		auto_revert:       bool | *true           @go(AutoRevert)
	}
	drain: {
		kill_signal:  string & !="" | *"SIGTERM" @go(KillSignal)
		kill_timeout: string & !="" | *"30s"     @go(KillTimeout)
	}
	resources: {
		cpu_mhz:   int & >0 | *500 @go(CPUMHz)
		memory_mb: int & >0 | *256 @go(MemoryMB)
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
	probes?:   #Probes
	garage?:   #GarageCluster
	temporal?: #TemporalCluster
	postgres:  #PostgresBinding
	electric?: #ElectricSync
	workload: #ComponentWorkload | *{}
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
