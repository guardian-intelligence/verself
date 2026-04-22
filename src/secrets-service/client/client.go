package secretsclient

const (
	BillingStripeSecretKeyName                  = "billing-service.stripe.secret_key"
	BillingStripeWebhookSecretName              = "billing-service.stripe.webhook_secret"
	SandboxGitHubPrivateKeyName                 = "sandbox-rental-service.github.private_key"
	SandboxGitHubWebhookSecretName              = "sandbox-rental-service.github.webhook_secret"
	SandboxGitHubClientSecretName               = "sandbox-rental-service.github.client_secret"
	MailboxResendAPIKeyName                     = "mailbox-service.resend.api_key"
	MailboxStalwartAdminPasswordName            = "mailbox-service.stalwart.admin_password"
	ObjectStorageGarageAdminTokenName           = "object-storage-service.garage.admin_token"
	ObjectStorageGarageProxyAccessKeyIDName     = "object-storage-service.garage.proxy_access_key_id"
	ObjectStorageGarageProxySecretAccessKeyName = "object-storage-service.garage.proxy_secret_access_key"
)
