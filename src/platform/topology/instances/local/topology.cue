package topology

import (
	"list"
	s "guardianintelligence.org/forge-metal/topology/schema"
	"strings"
)

topology: s.#Topology & {
	services: {
		clickhouse: {
			host:               "127.0.0.1"
			secure_native_port: 9440
		}
		otelcol: {
			host:        "127.0.0.1"
			grpc_port:   4317
			http_port:   4318
			statsd_port: 8125
		}
		forgejo: {
			host:     "127.0.0.1"
			port:     3000
			ssh_port: 2222
		}
		grafana: {
			host: "127.0.0.1"
			port: 4300
		}
		temporal_web: {
			host: "127.0.0.1"
			port: 4301
		}
		verdaccio: {
			host:        "127.0.0.1"
			listen_host: "0.0.0.0"
			port:        4873
		}
		tigerbeetle: {
			host: "127.0.0.1"
			port: 3320
		}
		postgresql: {
			host: "127.0.0.1"
			port: 5432
		}
		nats: {
			host:            "127.0.0.1"
			port:            4222
			monitoring_port: 8222
		}
		temporal: {
			host:                              "127.0.0.1"
			grpc_port:                         7233
			http_port:                         7243
			internal_frontend_grpc_port:       7236
			internal_frontend_http_port:       7246
			history_grpc_port:                 7234
			matching_grpc_port:                7235
			worker_grpc_port:                  7239
			frontend_membership_port:          6933
			internal_frontend_membership_port: 6936
			history_membership_port:           6934
			matching_membership_port:          6935
			worker_membership_port:            6939
			metrics_port:                      9001
			pprof_port:                        7936
		}
		billing: {
			host:          "127.0.0.1"
			port:          4242
			internal_port: 4255
		}
		sandbox_rental: {
			host:          "127.0.0.1"
			port:          4243
			internal_port: 4263
		}
		identity_service: {
			host:          "127.0.0.1"
			port:          4248
			internal_port: 4241
		}
		governance_service: {
			host:          "127.0.0.1"
			port:          4250
			internal_port: 4254
		}
		secrets_service: {
			host:          "127.0.0.1"
			port:          4251
			internal_port: 4253
		}
		profile_service: {
			host:          "127.0.0.1"
			port:          4258
			internal_port: 4259
		}
		notifications_service: {
			host: "127.0.0.1"
			port: 4260
		}
		projects_service: {
			host:          "127.0.0.1"
			port:          4264
			internal_port: 4265
		}
		source_code_hosting_service: {
			host:          "127.0.0.1"
			port:          4261
			internal_port: 4262
		}
		console: {
			host: "127.0.0.1"
			port: 4244
		}
		electric: {
			host: "127.0.0.1"
			port: 3010
		}
		zitadel: {
			host: "127.0.0.1"
			port: 8085
		}
		openbao: {
			host:         "127.0.0.1"
			port:         8200
			cluster_port: 8201
		}
		spire_server: {
			host: "127.0.0.1"
			port: 8081
		}
		spire_bundle_endpoint: {
			host: "127.0.0.1"
			port: 8082
		}
		stalwart: {
			host:      "127.0.0.1"
			smtp_port: 25
			http_port: 8090
		}
		mailbox_service: {
			host: "127.0.0.1"
			port: 4246
		}
		object_storage_service: {
			host:       "127.0.0.1"
			port:       4256
			admin_port: 4257
		}
		garage: {
			host: "127.0.0.1"
			nodes: [{
				instance:   0
				s3_port:    3900
				rpc_port:   3901
				admin_port: 3903
			}, {
				instance:   1
				s3_port:    3910
				rpc_port:   3911
				admin_port: 3913
			}, {
				instance:   2
				s3_port:    3920
				rpc_port:   3921
				admin_port: 3923
			}]
		}
		electric_mail: {
			host: "127.0.0.1"
			port: 3011
		}
		electric_notifications: {
			host: "127.0.0.1"
			port: 3012
		}
		platform: {
			host: "127.0.0.1"
			port: 4249
		}
		company: {
			host: "127.0.0.1"
			port: 4252
		}
		firecracker_host_service: {
			host:      "10.255.0.1"
			http_port: 18080
		}
	}
}

_controlPlaneServices: [
	"billing",
	"company",
	"console",
	"governance_service",
	"identity_service",
	"mailbox_service",
	"notifications_service",
	"object_storage_service",
	"platform",
	"profile_service",
	"projects_service",
	"sandbox_rental",
	"secrets_service",
	"source_code_hosting_service",
]

_portEntries: [
	for serviceName, service in topology.services
	for fieldName, value in service
	if fieldName == "port" || strings.HasSuffix(fieldName, "_port") {
		service: serviceName
		field:   fieldName
		port:    value
	},
	for node in topology.services.garage.nodes
	for fieldName, value in node
	if strings.HasSuffix(fieldName, "_port") {
		service: "garage"
		field:   "node_\(node.instance).\(fieldName)"
		port:    value
	},
]

_ports: [for entry in _portEntries {entry.port}]
_uniquePorts: true & list.UniqueItems(_ports)

_controlPlanePortChecks: [
	for name in _controlPlaneServices
	for fieldName, value in topology.services[name]
	if fieldName == "port" || strings.HasSuffix(fieldName, "_port") {
		port: value & >=4240 & <=4269
	},
]

ansible: {
	services: topology.services
}
