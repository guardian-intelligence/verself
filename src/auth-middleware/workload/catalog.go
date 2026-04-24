package workload

// Service names double as the SVID path segment (svc/<name>) and as the
// audit-ledger credential name. Every Forge Metal workload registers with SPIRE
// under this convention, so peer identities are deterministic from the name.
const (
	ServiceBilling            = "billing-service"
	ServiceClickHouseOperator = "clickhouse-operator"
	ServiceGrafana            = "grafana"
	ServiceGovernance         = "governance-service"
	ServiceIdentity           = "identity-service"
	ServiceMailbox            = "mailbox-service"
	ServiceNATS               = "nats"
	ServiceNotifications      = "notifications-service"
	ServiceObjectStorage      = "object-storage-service"
	ServiceObjectStorageAdmin = "object-storage-admin"
	ServiceOTelCollector      = "otelcol"
	ServiceProfile            = "profile-service"
	ServiceSandboxRental      = "sandbox-rental-service"
	ServiceSecrets            = "secrets-service"
	ServiceTemporalServer     = "temporal-server"
	ServiceTemporalWeb        = "temporal-web"
)
