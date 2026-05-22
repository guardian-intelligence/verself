package secretsclient

const (
	BillingStripeSecretKeyName         = "billing-service.stripe.secret_key"
	BillingStripeWebhookSecretName     = "billing-service.stripe.webhook_secret"
	GitHubIntegrationPrivateKeyName    = "github-integration-service.github.private_key"
	GitHubIntegrationWebhookSecretName = "github-integration-service.github.webhook_secret"
	IAMSpiceDBPresharedKeyName         = "iam-service.spicedb.grpc_preshared_key"
	IAMZitadelAdminTokenName           = "iam-service.zitadel.admin_token"
	SandboxRunnerBootstrapSecretPrefix = "sandbox-rental-service.runner-bootstrap."
	SandboxForgejoAutomationTokenName  = "sandbox-rental-service.forgejo.automation_token"
	SourceForgejoAutomationTokenName   = "source-code-hosting-service.forgejo.automation_token"
	MailboxResendAPIKeyName            = "mailbox-service.resend.api_key"
	NotificationsResendAPIKeyName      = MailboxResendAPIKeyName
	MailboxStalwartAdminPasswordName   = "mailbox-service.stalwart.admin_password"
	ObjectStorageGarageAdminTokenName  = "object-storage-service.garage.admin_token"
)
