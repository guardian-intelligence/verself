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
https://github.com/settings/apps/forge-metal-ci
```

Current dev public identifiers:

```yaml
sandbox_rental_service_github_app_enabled: true
sandbox_rental_service_github_app_id: 3370540
sandbox_rental_service_github_app_slug: forge-metal-ci
sandbox_rental_service_github_app_client_id: "Iv23liDpxGOmBSQwSJ5i"
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
  - Metadata: read
- Organization permissions:
  - Self-hosted runners: read and write

Webhook events:

- Workflow job

## Install Secrets

The app's public identifiers live in Ansible vars. The secret material must be
installed on the node:

```text
/etc/credstore/sandbox-rental/github-app-private-key
/etc/credstore/sandbox-rental/github-app-webhook-secret
/etc/credstore/sandbox-rental/github-app-client-secret
```

Then redeploy:

```bash
cd src/platform/ansible
ansible-playbook playbooks/dev-single-node.yml --tags sandbox_rental_service,caddy
```

The sandbox-rental-service role fails before restarting the service when any
required GitHub App public setting or credential file is missing.

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

jobs:
  hello:
    runs-on: [self-hosted, linux, x64, metal-4vcpu-ubuntu-2404]
    steps:
      - uses: actions/checkout@v5
      - run: echo "github-runner-marker $(uname -a)"
```

Expected trace order:

1. `sandbox-rental.github_runner.workflow_job`
2. `sandbox-rental.github_runner.create_jit_config`
3. `sandbox-rental.execution.submit`
4. `river.insert_many`
5. `river.work/execution.advance`
6. `vm-orchestrator.EnsureRun`
7. `vmorchestrator.guest.phase_start`
8. `vmorchestrator.guest.phase_end`
9. `sandbox-rental.execution.finalize`

## Primary Sources

- GitHub App registration: https://docs.github.com/en/apps/creating-github-apps/registering-a-github-app/registering-a-github-app
- GitHub App permissions: https://docs.github.com/en/apps/creating-github-apps/registering-a-github-app/choosing-permissions-for-a-github-app
- GitHub App private keys: https://docs.github.com/en/apps/creating-github-apps/authenticating-with-a-github-app/managing-private-keys-for-github-apps
- GitHub App installation auth: https://docs.github.com/apps/creating-github-apps/authenticating-with-a-github-app/authenticating-as-a-github-app-installation
- GitHub self-hosted runner REST API: https://docs.github.com/en/rest/actions/self-hosted-runners
- GitHub workflow job webhook event: https://docs.github.com/en/webhooks/webhook-events-and-payloads#workflow_job
- GitHub self-hosted runner labels in workflows: https://docs.github.com/en/actions/how-tos/manage-runners/self-hosted-runners/use-in-a-workflow
