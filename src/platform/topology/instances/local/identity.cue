package topology

import (
	"list"
	s "guardianintelligence.org/forge-metal/topology/schema"
)

identity: s.#Identity & {
	spire_trust_domain:        "spiffe.{{ verself_domain }}"
	spire_server_bind_address: "127.0.0.1"
	spire_server_bind_port:    "{{ services.spire_server.port }}"
	spire_server_socket_path:  "/run/spire-server/private/api.sock"
	spire_agent_socket_path:   "/run/spire-agent/sockets/agent.sock"
	spire_workload_group:      "spire_workload"
	spire_agent_id:            "spiffe://{{ spire_trust_domain }}/node/single-node"

	spire_bundle_endpoint_bind_address: "127.0.0.1"
	spire_bundle_endpoint_bind_port:    "{{ services.spire_bundle_endpoint.port }}"
	spire_jwt_bundle_endpoint_url:      "https://{{ spire_bundle_endpoint_bind_address }}:{{ spire_bundle_endpoint_bind_port }}"
	spire_jwt_issuer_url:               "{{ spire_jwt_bundle_endpoint_url }}"

	workloads: {
		identity_service: {
			variable:  "spire_identity_service_id"
			spiffe_id: "spiffe://{{ spire_trust_domain }}/svc/identity-service"
		}
		profile_service: {
			variable:  "spire_profile_service_id"
			spiffe_id: "spiffe://{{ spire_trust_domain }}/svc/profile-service"
		}
		notifications_service: {
			variable:  "spire_notifications_service_id"
			spiffe_id: "spiffe://{{ spire_trust_domain }}/svc/notifications-service"
		}
		projects_service: {
			variable:  "spire_projects_service_id"
			spiffe_id: "spiffe://{{ spire_trust_domain }}/svc/projects-service"
		}
		source_code_hosting_service: {
			variable:  "spire_source_code_hosting_service_id"
			spiffe_id: "spiffe://{{ spire_trust_domain }}/svc/source-code-hosting-service"
		}
		nats: {
			variable:  "spire_nats_id"
			spiffe_id: "spiffe://{{ spire_trust_domain }}/svc/nats"
		}
		governance_service: {
			variable:  "spire_governance_service_id"
			spiffe_id: "spiffe://{{ spire_trust_domain }}/svc/governance-service"
		}
		billing_service: {
			variable:  "spire_billing_service_id"
			spiffe_id: "spiffe://{{ spire_trust_domain }}/svc/billing-service"
		}
		sandbox_rental: {
			variable:  "spire_sandbox_rental_id"
			spiffe_id: "spiffe://{{ spire_trust_domain }}/svc/sandbox-rental-service"
		}
		secrets_service: {
			variable:  "spire_secrets_service_id"
			spiffe_id: "spiffe://{{ spire_trust_domain }}/svc/secrets-service"
		}
		mailbox_service: {
			variable:  "spire_mailbox_service_id"
			spiffe_id: "spiffe://{{ spire_trust_domain }}/svc/mailbox-service"
		}
		object_storage_admin: {
			variable:  "spire_object_storage_admin_id"
			spiffe_id: "spiffe://{{ spire_trust_domain }}/svc/object-storage-admin"
		}
		object_storage_service: {
			variable:  "spire_object_storage_service_id"
			spiffe_id: "spiffe://{{ spire_trust_domain }}/svc/object-storage-service"
		}
		temporal_server: {
			variable:  "spire_temporal_server_id"
			spiffe_id: "spiffe://{{ spire_trust_domain }}/svc/temporal-server"
		}
		temporal_web: {
			variable:  "spire_temporal_web_id"
			spiffe_id: "spiffe://{{ spire_trust_domain }}/svc/temporal-web"
		}
		grafana: {
			variable:  "spire_grafana_id"
			spiffe_id: "spiffe://{{ spire_trust_domain }}/svc/grafana"
		}
		otelcol: {
			variable:  "spire_otelcol_id"
			spiffe_id: "spiffe://{{ spire_trust_domain }}/svc/otelcol"
		}
		clickhouse_server: {
			variable:  "spire_clickhouse_server_id"
			spiffe_id: "spiffe://{{ spire_trust_domain }}/svc/clickhouse-server"
		}
		clickhouse_operator: {
			variable:  "spire_clickhouse_operator_id"
			spiffe_id: "spiffe://{{ spire_trust_domain }}/svc/clickhouse-operator"
		}
	}

	openbao_spiffe_jwt_mount:  "spiffe-jwt"
	openbao_workload_audience: "openbao"
}

_spiffeIDs: [for _, workload in identity.workloads {workload.spiffe_id}]
_uniqueSpiffeIDs: true & list.UniqueItems(_spiffeIDs)

ansible: {
	spire_trust_domain:        identity.spire_trust_domain
	spire_server_bind_address: identity.spire_server_bind_address
	spire_server_bind_port:    identity.spire_server_bind_port
	spire_server_socket_path:  identity.spire_server_socket_path
	spire_agent_socket_path:   identity.spire_agent_socket_path
	spire_workload_group:      identity.spire_workload_group
	spire_agent_id:            identity.spire_agent_id

	spire_identity_service_id:            identity.workloads.identity_service.spiffe_id
	spire_profile_service_id:             identity.workloads.profile_service.spiffe_id
	spire_notifications_service_id:       identity.workloads.notifications_service.spiffe_id
	spire_projects_service_id:            identity.workloads.projects_service.spiffe_id
	spire_source_code_hosting_service_id: identity.workloads.source_code_hosting_service.spiffe_id
	spire_nats_id:                        identity.workloads.nats.spiffe_id
	spire_governance_service_id:          identity.workloads.governance_service.spiffe_id
	spire_billing_service_id:             identity.workloads.billing_service.spiffe_id
	spire_sandbox_rental_id:              identity.workloads.sandbox_rental.spiffe_id
	spire_secrets_service_id:             identity.workloads.secrets_service.spiffe_id
	spire_mailbox_service_id:             identity.workloads.mailbox_service.spiffe_id
	spire_object_storage_admin_id:        identity.workloads.object_storage_admin.spiffe_id
	spire_object_storage_service_id:      identity.workloads.object_storage_service.spiffe_id
	spire_temporal_server_id:             identity.workloads.temporal_server.spiffe_id
	spire_temporal_web_id:                identity.workloads.temporal_web.spiffe_id
	spire_grafana_id:                     identity.workloads.grafana.spiffe_id
	spire_otelcol_id:                     identity.workloads.otelcol.spiffe_id
	spire_clickhouse_server_id:           identity.workloads.clickhouse_server.spiffe_id
	spire_clickhouse_operator_id:         identity.workloads.clickhouse_operator.spiffe_id

	spire_bundle_endpoint_bind_address: identity.spire_bundle_endpoint_bind_address
	spire_bundle_endpoint_bind_port:    identity.spire_bundle_endpoint_bind_port
	spire_jwt_bundle_endpoint_url:      identity.spire_jwt_bundle_endpoint_url
	spire_jwt_issuer_url:               identity.spire_jwt_issuer_url

	openbao_spiffe_jwt_mount:         identity.openbao_spiffe_jwt_mount
	openbao_tenancy_spiffe_jwt_mount: identity.openbao_spiffe_jwt_mount
	openbao_workload_audience:        identity.openbao_workload_audience
}
