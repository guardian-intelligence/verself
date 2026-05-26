package workload

// Service names double as the SVID path segment (svc/<name>) and as the
// audit-ledger credential name. Every Verself workload registers with SPIRE
// under this convention, so peer identities are deterministic from the name.
const (
	ServiceAnalytics           = "analytics-service"
	ServiceBilling             = "billing-service"
	ServiceClickHouseOperator  = "clickhouse-operator"
	ServiceDistribution        = "distribution-service"
	ServiceDistributionRelease = "distribution-release"
	ServiceGrafana             = "grafana"
	ServiceGovernance          = "governance-service"
	ServiceGitHubIntegration   = "github-integration-service"
	ServiceIAM                 = "iam-service"
	ServiceEmail               = "email-service"
	ServiceNATS                = "nats"
	ServiceNotifications       = "notifications-service"
	ServiceObjectStorage       = "object-storage-service"
	ServiceObjectStorageAdmin  = "object-storage-admin"
	ServiceOTelCollector       = "otelcol"
	ServiceProfile             = "profile-service"
	ServiceProjects            = "projects-service"
	ServiceSandboxRental       = "sandbox-rental-service"
	ServiceSecrets             = "secrets-service"
	ServiceSourceCodeHosting   = "source-code-hosting-service"
	ServiceTemporalServer      = "temporal-server"
)
