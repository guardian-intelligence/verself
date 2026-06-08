package sitefacts

site: "gamma"
installationID: "inst_gamma_01JZ0000000000000000000000"

domains: {
	product: "gamma.verself.sh"
	company: "gamma.guardianintelligence.org"
}

cloudflare: {
	productZone: "verself.sh"
	companyZone: "guardianintelligence.org"
	dnsRecords: [
		{kind: "browser_origin", record: "@", zone: "product"},
		{kind: "public_api_origin", record: "billing.api", zone: "product"},
		{kind: "public_api_origin", record: "deployments.api", zone: "product"},
		{kind: "public_api_origin", record: "distribution.api", zone: "product"},
		{kind: "public_api_origin", record: "oci", zone: "product"},
		{kind: "public_api_origin", record: "sandbox.api", zone: "product"},
		{kind: "public_api_origin", record: "iam.api", zone: "product"},
		{kind: "public_api_origin", record: "profile.api", zone: "product"},
		{kind: "public_api_origin", record: "notifications.api", zone: "product"},
		{kind: "public_api_origin", record: "projects.api", zone: "product"},
		{kind: "public_api_origin", record: "source.api", zone: "product"},
		{kind: "public_api_origin", record: "governance.api", zone: "product"},
		{kind: "public_api_origin", record: "github.api", zone: "product"},
		{kind: "public_api_origin", record: "secrets.api", zone: "product"},
		{kind: "public_api_origin", record: "email.api", zone: "product"},
		{kind: "operator_origin", record: "dashboard", zone: "product"},
		{kind: "operator_origin", record: "access", zone: "product"},
		{kind: "protocol_origin", record: "git", zone: "product"},
		{kind: "protocol_origin", record: "mail", zone: "product"},
		{kind: "protocol_origin", record: "npm", zone: "product"},
		{kind: "protocol_origin", record: "oci", zone: "product"},
	]
}

bareMetal: {
	hostAlias: "vs-gamma-w0"
	publicIPv4: "206.223.228.87"
}

spiffe: trustDomain: "gamma.verself.sh"

deployment: {
	githubAllowedRepositories: "guardian-intelligence/verself"
	githubAllowedRefs: "refs/heads/main"
	githubAllowedWorkflowRefs: "guardian-intelligence/verself/.github/workflows/gamma-deploy.yml@refs/heads/main"
	repoURL: "https://github.com/guardian-intelligence/verself.git"
}

objectStorage: {
	s3Endpoint: "https://c3eaeffaadf7d4847684d4775c16d598.r2.cloudflarestorage.com"
	deploymentArtifactsBucket: "verself-deployment-artifacts"
}

githubIntegration: {
	appID: "3918896"
	appSlug: "verself-runner-gamma"
	oauthClientID: "Iv23lidNdRYiMW9kU27r"
	appSettingsURL: "https://github.com/organizations/guardian-intelligence/settings/apps/verself-runner-gamma"
	runnerClassPrefix: "verself-gamma-"
}

dogfood: {
	orgID: "org_gamma_01JZ0000000000000000000000"
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
