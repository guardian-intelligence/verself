# GitHub App Runner Setup

Forge Metal's GitHub Actions runner integration is a GitHub App installed on a
customer GitHub Organization. Personal-account installations are deliberately
rejected until the product model has a personal-owner tenant story.

## Create or Edit the App

Create a new app at:

```text
https://github.com/settings/apps/new
```

Edit the current dev app at:

```text
https://github.com/organizations/guardian-intelligence/settings/apps/forge-metal-ci
```

Current dev public identifiers:

```yaml
sandbox_rental_service_github_app_enabled: true
sandbox_rental_service_github_app_id: 3370540
sandbox_rental_service_github_app_slug: forge-metal-ci
sandbox_rental_service_github_app_client_id: "Iv23liDpxGOmBSQwSJ5i"
sandbox_rental_service_github_app_settings_url: "https://github.com/organizations/guardian-intelligence/settings/apps/forge-metal-ci"
```

Use these GitHub App settings:

- Homepage URL: `https://rentasandbox.<domain>/`
- Callback URL: `https://rentasandbox.<domain>/github/installations/callback`
- Request user authorization during installation: enabled
- Webhook: active
- Webhook URL: `https://rentasandbox.<domain>/webhooks/github/actions`
- Webhook content type: JSON
- Installable by: any account for the customer-facing app, but install it on a
  GitHub Organization for the current implementation.

Permissions:

- Repository permissions:
  - Actions: read
  - Contents: read
  - Metadata: read
- Organization permissions:
  - Self-hosted runners: read and write

Webhook events:

- Workflow job

## Install Secrets

The app's public identifiers live in Ansible vars. The secret material must be
present in OpenBao under the platform runtime provider path:

```text
platform/providers/github/sandbox-rental-service
```

Required fields:

```text
private_key
webhook_secret
client_secret
```

The first cutover deploy can migrate an existing
`/etc/credstore/sandbox-rental/github-app-*` set into OpenBao. After the
cutover, the service reads only from OpenBao via SPIFFE-authenticated startup
code.

Then redeploy:

```bash
cd src/platform/ansible
ansible-playbook playbooks/dev-single-node.yml --tags sandbox_rental_service,caddy
```

The sandbox-rental-service role fails before restarting the service when any
required GitHub App public setting is missing, or when the deploy cannot find a
complete GitHub provider secret in OpenBao or a complete legacy credstore set
to migrate.

## Connect a GitHub Organization

Start the Forge Metal side of the install flow as a sandbox org admin:

```bash
source <(src/platform/scripts/assume-persona.sh platform-admin --print)

curl -sS -X POST "https://rentasandbox.<domain>/api/v1/github/installations/connect" \
  -H "Authorization: Bearer ${SANDBOX_RENTAL_ACCESS_TOKEN}" \
  -H "Idempotency-Key: github-install-$(date +%s)" \
  | jq .
```

Open the returned `setup_url` and install the app on the GitHub Organization.
The GitHub callback returns the installation record as JSON until the product UI
adds a polished redirect.

Verify the mapping:

```bash
curl -sS "https://rentasandbox.<domain>/api/v1/github/installations" \
  -H "Authorization: Bearer ${SANDBOX_RENTAL_ACCESS_TOKEN}" \
  | jq .
```

## First Workflow

Use all labels for the first proof:

```yaml
name: forge-metal-ci
on: [push]

permissions:
  contents: read
  id-token: write

jobs:
  hello:
    runs-on: [self-hosted, linux, x64, metal-4vcpu-ubuntu-2404]
    steps:
      - uses: actions/checkout@v5
      - uses: guardian-intelligence/forge-metal/.github/actions/oidc-tracer@main
        with:
          audience: forge-metal-ci
      - run: echo "github-runner-marker $(uname -a)"
```

Expected control-plane order:

1. GitHub `workflow_job` webhook is verified and upserted into
   `github_workflow_jobs`.
2. `github.capacity.reconcile` compares queued unbound demand against active
   allocations for the installation/repo/runner class.
3. Runner allocation creates GitHub runner capacity and then submits a
   `github_runner` execution.
4. GitHub assignment is recorded only when webhook or polling evidence proves
   the runner identity that accepted the job.
5. The execution path emits the same lease/exec evidence as direct execution:
   `rpc.AcquireLease`, `rpc.StartExec`, `rpc.WaitExec`, `rpc.ReleaseLease`, and
   `forge_metal.vm_lease_evidence`.

## OIDC Tracer Bullet

Forge Metal does not host customer secrets for the GitHub CI product. Workflows
use GitHub's standard OpenID Connect path: grant `id-token: write`, ask GitHub
for a job-scoped ID token with the audience required by the target cloud or
secret broker, and exchange that token directly with the customer's AWS, GCP,
Azure, Vault, or OpenBao trust configuration.

The repo canary includes `.github/actions/oidc-tracer` as a local proof action.
It requests a GitHub OIDC token from the runner-provided
`ACTIONS_ID_TOKEN_REQUEST_URL`, verifies the JWT signature against GitHub's JWKS,
and asserts `iss`, `aud`, `sub`, `repository`, `ref`, `sha`, and `run_id`
claims. It prints only sanitized claims and never prints the JWT.

Successful proof means:

1. GitHub issued an OIDC token to a job running on the Forge Metal runner.
2. The token can be verified using public GitHub OIDC metadata and JWKS.
3. The token is bound to the expected repo, ref, SHA, and workflow run.
4. Customers can bring the same cloud-federation policies they use with
   GitHub-hosted runners.

## Primary Sources

- GitHub App registration: https://docs.github.com/en/apps/creating-github-apps/registering-a-github-app/registering-a-github-app
- GitHub App permissions: https://docs.github.com/en/apps/creating-github-apps/registering-a-github-app/choosing-permissions-for-a-github-app
- GitHub App private keys: https://docs.github.com/en/apps/creating-github-apps/authenticating-with-a-github-app/managing-private-keys-for-github-apps
- GitHub App installation auth: https://docs.github.com/apps/creating-github-apps/authenticating-with-a-github-app/authenticating-as-a-github-app-installation
- GitHub self-hosted runner REST API: https://docs.github.com/en/rest/actions/self-hosted-runners
- GitHub workflow job webhook event: https://docs.github.com/en/webhooks/webhook-events-and-payloads#workflow_job
- GitHub self-hosted runner labels in workflows: https://docs.github.com/en/actions/how-tos/manage-runners/self-hosted-runners/use-in-a-workflow
- GitHub OIDC in Actions: https://docs.github.com/actions/reference/openid-connect-reference
