package topology

import (
	"list"
	s "guardianintelligence.org/forge-metal/topology/schema"
)

data: s.#Data & {
	postgresql: {
		max_connections:                300
		superuser_reserved_connections: 10
		connection_headroom:            30
		role_connection_limits: {
			billing:                     30
			sandbox_rental:              25
			electric:                    23
			electric_mail:               23
			zitadel:                     10
			frontend_auth:               10
			grafana:                     8
			stalwart:                    8
			identity_service:            8
			governance_service:          13
			profile_service:             8
			notifications_service:       8
			projects_service:            8
			source_code_hosting_service: 8
			electric_notifications:      10
			mailbox_service:             8
			object_storage_service:      8
			temporal:                    40
		}
	}

	object_storage: {
		service_uid: 960
		admin_uid:   961
	}
}

_postgresqlConnectionLimitValues: [
	for _, limit in data.postgresql.role_connection_limits {
		limit
	},
]
_postgresqlConnectionLimitTotal:  list.Sum(_postgresqlConnectionLimitValues)
_postgresqlConnectionBudgetLimit: data.postgresql.max_connections -
	data.postgresql.superuser_reserved_connections -
					data.postgresql.connection_headroom
_postgresqlConnectionBudgetFits: true & (_postgresqlConnectionLimitTotal <= _postgresqlConnectionBudgetLimit)

_objectStorageUIDs: [data.object_storage.service_uid, data.object_storage.admin_uid]
_uniqueObjectStorageUIDs: true & list.UniqueItems(_objectStorageUIDs)

ansible: {
	postgresql_max_connections:                data.postgresql.max_connections
	postgresql_superuser_reserved_connections: data.postgresql.superuser_reserved_connections
	postgresql_connection_headroom:            data.postgresql.connection_headroom
	postgresql_connection_limit_total:         _postgresqlConnectionLimitTotal
	postgresql_role_connection_limits:         data.postgresql.role_connection_limits

	object_storage_service_uid: data.object_storage.service_uid
	object_storage_admin_uid:   data.object_storage.admin_uid
}
