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

#GarageNode: {
	instance:   int & >=0
	s3_port:    #Port
	rpc_port:   #Port
	admin_port: #Port
	...
}

#GarageCluster: {
	instances: {
		count: int & >=1
		port_plan: {
			stride:     int & >0
			s3_base:    #Port
			rpc_base:   #Port
			admin_base: #Port
			...
		}
		...
	}
	nodes: [...#GarageNode]
	...
}

#TemporalRPCService: {
	grpc_port:       #Port
	membership_port: #Port
	...
}

#TemporalFrontendService: {
	grpc_port:       #Port
	http_port:       #Port
	membership_port: #Port
	...
}

#TemporalCluster: {
	frontend:          #TemporalFrontendService
	internal_frontend: #TemporalFrontendService
	history:           #TemporalRPCService
	matching:          #TemporalRPCService
	worker:            #TemporalRPCService

	diagnostics: {
		metrics_port: #Port
		pprof_port:   #Port
		...
	}
	...
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
	policies: {
		[string]: #Policy
		...
	}
	...
}
