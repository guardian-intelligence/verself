package sitefacts

site: "prod"
installationID: "inst_5NZSEA08R8P3HN566DNH8D301M"

domains: {
	product: "verself.sh"
	company: "guardianintelligence.org"
}

cloudflare: {
	productZone: "verself.sh"
	companyZone: "guardianintelligence.org"
	dnsRecords: [
		{kind: "browser_origin", record: "@", zone: "product"},
		{kind: "browser_origin", record: "@", zone: "company"},
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
	hostAlias: "vs-dev-w0"
	publicIPv4: "206.223.228.101"
}

spiffe: trustDomain: "spiffe.verself.sh"

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
	appID: "3370540"
	appSlug: "verself-runner"
	oauthClientID: "Iv23liDpxGOmBSQwSJ5i"
	appSettingsURL: "https://github.com/organizations/guardian-intelligence/settings/apps/verself-runner"
	runnerClassPrefix: "verself-"
}

email: {
	agentSenderAddress: "agents@guardianintelligence.org"
	operatorMailboxAddress: "anveio@guardianintelligence.org"
	operatorForwardDestination: "im.shovonhasan@gmail.com"
	resendSendingDomains: [
		{domain: "notify.verself.sh", zone: "verself.sh", rootDMARCDomain: "verself.sh"},
		{domain: "guardianintelligence.org", zone: "guardianintelligence.org", rootDMARCDomain: "guardianintelligence.org"},
	]
	cloudflareRouting: {
		zone: "guardianintelligence.org"
		rules: [
			{match: "anveio@guardianintelligence.org", forwardTo: "im.shovonhasan@gmail.com"},
		]
	}
}

dogfood: {
	orgID: "org_B7HWGKW0SH7G4EXW9XT8TCT60C"
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
