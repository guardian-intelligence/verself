package sitefacts

site: "dev"
installationID: "inst_dev_01JZ0000000000000000000000"

domains: {
	product: "dev.verself.sh"
	company: "dev.guardianintelligence.org"
}

cloudflare: {
	productZone: "verself.sh"
	companyZone: "guardianintelligence.org"
	dnsRecords: []
}

bareMetal: {
	hostAlias: "vs-dev-w0"
	publicIPv4: "0.0.0.0"
}

spiffe: trustDomain: "dev.verself.sh"

deployment: {
	githubAllowedRepositories: ""
	githubAllowedRefs: ""
	githubAllowedWorkflowRefs: ""
	repoURL: "https://github.com/guardian-intelligence/verself.git"
}

objectStorage: {
	s3Endpoint: "https://c3eaeffaadf7d4847684d4775c16d598.r2.cloudflarestorage.com"
	deploymentArtifactsBucket: "verself-deployment-artifacts"
}

githubIntegration: {
	appID: "0"
	appSlug: "verself-runner-dev"
	oauthClientID: ""
	appSettingsURL: "https://github.com/organizations/guardian-intelligence/settings/apps/verself-runner-dev"
	runnerClassPrefix: "verself-dev-"
}

dogfood: {
	orgID: "org_dev_01JZ0000000000000000000000"
	serviceDiscoveryCanaryOrgSlug: "guardian-intelligence"
}

serviceSubdomains: {
	"billing_service": "billing.api"
	"deployment_service": "deployments.api"
	"distribution_service": "distribution.api"
	"email_service": "email.api"
	"forgejo": "git"
	"github_integration_service": "github.api"
	"governance_service": "governance.api"
	"iam_service": "iam.api"
	"notifications_service": "notifications.api"
	"npm_registry": "npm"
	"pomerium": "access"
	"profile_service": "profile.api"
	"projects_service": "projects.api"
	"resend": "notify"
	"sandbox_rental_service": "sandbox.api"
	"secrets_service": "secrets.api"
	"source_code_hosting_service": "source.api"
	"stalwart": "mail"
}
