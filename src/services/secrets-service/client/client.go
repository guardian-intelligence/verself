package secretsclient

const (
	BillingStripeSecretKeyName             = "billing-service.stripe.secret_key"
	BillingStripeWebhookSecretName         = "billing-service.stripe.webhook_secret"
	GitHubIntegrationPrivateKeyName        = "github-integration-service.github.private_key"
	GitHubIntegrationWebhookSecretName     = "github-integration-service.github.webhook_secret"
	GitHubIntegrationOAuthClientSecretName = "github-integration-service.github.oauth_client_secret"
	GitHubIntegrationUserTokenSecretPrefix = "github-integration-service.github-user-token."
	IAMEmailIdentityHMACKeyName            = "iam-service.email_identity.hmac_key"
	IAMSpiceDBPresharedKeyName             = "iam-service.spicedb.grpc_preshared_key"
	IAMZitadelAdminTokenName               = "iam-service.zitadel.admin_token"
	SandboxRunnerBootstrapSecretPrefix     = "sandbox-rental-service.runner-bootstrap."
	SourceForgejoAutomationTokenName       = "source-code-hosting-service.forgejo.automation_token"
	EmailResendAPIKeyName                  = "email-service.resend.api_key"
	NotificationsResendAPIKeyName          = EmailResendAPIKeyName
	EmailStalwartAdminPasswordName         = "email-service.stalwart.admin_password"
	ObjectStorageGarageAdminTokenName      = "object-storage-service.garage.admin_token"
)
