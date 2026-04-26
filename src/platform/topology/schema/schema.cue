package schema

#Host:        string & !=""
#ServiceHost: #Host & !="0.0.0.0" & !="::"
#Port:        int & >=1 & <=65535 & !=4245

#GarageNode: close({
	instance:   int & >=0
	s3_port:    #Port
	rpc_port:   #Port
	admin_port: #Port
})

#DeployTag:  string & !=""
#DomainName: string & !=""
#Subdomain:  string & !=""
#SPIFFEID:   string & =~"^spiffe://"

#UID: int & >=100 & <=65535

#GoBinary: close({
	name:            string & !=""
	package:         string & !=""
	output_path:     string & !=""
	cgo_enabled:     "0" | "1"
	version_ldflags: bool
	restart_handlers: [...string]
	deploy_tags: [...#DeployTag]
})

#ServerTool: close({
	version: string & !=""
	format:  string & !=""
	deploy_tags: [...#DeployTag]

	url?:    string & !=""
	sha256?: string & =~"^[0-9a-f]{64}$"

	extract_binary?:   string & !=""
	extract_dir?:      string & !=""
	binary_name?:      string & !=""
	install_dir?:      string & !=""
	plugin_dir?:       string & !=""
	strip_components?: int & >=0

	symlinks?: [...string]
	binaries?: [...string]
	bin_symlinks?: [...string]

	plugins?: [...string]
	xcaddy_version?: string & !=""
})

#Artifacts: close({
	go_binaries: [...#GoBinary]
	server_tools: [string]: #ServerTool
})

#PublicOrigin: close({
	subdomain: #Subdomain
	domain:    #DomainName
	dns:       bool | *true
})

#Exposure: close({
	verself_domain:  #DomainName
	platform_domain: #DomainName
	company_domain:  #DomainName

	origins: [string]: #PublicOrigin

	resend_subdomain:      #Subdomain
	resend_domain:         #DomainName
	resend_sender_address: string & !=""
	resend_sender_name:    string & !=""

	stalwart_subdomain: #Subdomain
	stalwart_domain:    #DomainName

	cloudflare_dns_record_type: "A" | "AAAA"
	cloudflare_dns_ttl:         int & >=1
})

#Identity: close({
	spire_trust_domain:                 string & !=""
	spire_server_bind_address:          #ServiceHost
	spire_server_bind_port:             string & !=""
	spire_server_socket_path:           string & !=""
	spire_agent_socket_path:            string & !=""
	spire_workload_group:               string & !=""
	spire_agent_id:                     #SPIFFEID
	spire_bundle_endpoint_bind_address: #ServiceHost
	spire_bundle_endpoint_bind_port:    string & !=""
	spire_jwt_bundle_endpoint_url:      string & !=""
	spire_jwt_issuer_url:               string & !=""

	workloads: [string]: close({
		variable:  string & !=""
		spiffe_id: #SPIFFEID
	})

	openbao_spiffe_jwt_mount:  string & !=""
	openbao_workload_audience: string & !=""
})

#Data: close({
	postgresql: close({
		max_connections:                int & >=1
		superuser_reserved_connections: int & >=0
		connection_headroom:            int & >=0
		role_connection_limits: [string]: int & >=1
	})

	object_storage: close({
		service_uid: #UID
		admin_uid:   #UID
	})
})

#Network: close({
	wireguard: close({
		interface: string & !=""
		port:      #Port
		network:   string & !=""
	})

	firecracker: close({
		guest_pool_cidr: string & !=""
		host_service_ip: #ServiceHost
	})

	nftables: close({
		public_tcp_ports: [...#Port]
		public_udp_ports: [...#Port]
		trusted_interfaces: [...string & !=""]
		firecracker_guest_cidr:      string & !=""
		firecracker_host_service_ip: #ServiceHost
		firecracker_guest_tcp_ports: [...#Port]
		ssh_public:                bool
		ssh_rate:                  string & !=""
		ssh_burst:                 int & >=1
		remove_legacy_tables:      bool
		legacy_table_name_pattern: string & !=""
	})
})

#Service: close({
	host:         #ServiceHost
	listen_host?: #Host

	port?:                              #Port
	admin_port?:                        #Port
	cluster_port?:                      #Port
	frontend_membership_port?:          #Port
	grpc_port?:                         #Port
	history_grpc_port?:                 #Port
	history_membership_port?:           #Port
	http_port?:                         #Port
	internal_frontend_grpc_port?:       #Port
	internal_frontend_http_port?:       #Port
	internal_frontend_membership_port?: #Port
	internal_port?:                     #Port
	matching_grpc_port?:                #Port
	matching_membership_port?:          #Port
	metrics_port?:                      #Port
	monitoring_port?:                   #Port
	pprof_port?:                        #Port
	secure_native_port?:                #Port
	smtp_port?:                         #Port
	ssh_port?:                          #Port
	statsd_port?:                       #Port
	worker_grpc_port?:                  #Port
	worker_membership_port?:            #Port

	nodes?: [...#GarageNode]
})

#Services: [string]: #Service

#Topology: close({
	services: #Services
})
