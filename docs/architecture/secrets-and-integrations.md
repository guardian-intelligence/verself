# Configuration, Secrets, And Integration Runbook

Verself configuration and third-party integration state is managed through an
environment-scoped catalog. The catalog records every external account,
resource, credential, public variable, destination, consumer, verification step,
and isolation exception. Secret values live in OpenBao, provider-native vaults,
or product KV stores according to lifecycle. The catalog is the inventory that
ties those stores together.

Stripe Projects is the provider provisioning and credential-handoff model for
new integrations when the provider exists in the Projects catalog. A Stripe
project represents a single app or codebase, groups provider accounts,
services, resources, credentials, and environment variables, and supports
catalog search, provisioning, credential rotation, and `env --pull`. Stripe
Projects does not support named environments inside one project, so Verself uses
one provider project per deployment environment.

```text
Verself site/environment
  -> provider project
  -> integration catalog entry
  -> OpenBao site-config or runtime-secret target
  -> materialization target
  -> runtime consumer
  -> verification evidence
```

## Environment Boundary

Each environment has independent provider resources and credentials unless the
catalog marks a credential as a bootstrap exception.

| Verself environment | Provider project policy | Runtime isolation |
| --- | --- | --- |
| `prod` | Dedicated provider projects and live provider accounts. | Customer-facing. No gamma or dev credentials. |
| `gamma` | Dedicated provider projects and sandbox/test provider accounts where possible. | Pomerium-gated production rehearsal. No prod runtime credentials. |
| `dev` | Per-operator provider projects or disposable provider resources. | Operator-local. No prod credentials. |

Provider projects are a provisioning surface. Runtime services do not read
`.env`, `.projects/vault`, or provider CLI caches. Import tooling reads only
catalog-approved names and writes them into OpenBao.

Stripe Projects state lives in a per-site worktree:

```text
src/integrations/stripe-projects/sites/<site>/
  .projects/state.json
  .projects/state.local.json
  .projects/vault/        # ignored
  .projects/cache/        # ignored
  .env                    # ignored
```

Use the repo wrapper so the command runs from the correct site worktree:

```text
aspect integrations stripe-projects --site=gamma --action=status
aspect integrations stripe-projects --site=gamma --action=init --confirm
aspect integrations stripe-projects --site=gamma --action=search --query=resend
aspect integrations stripe-projects --site=gamma --action=env-pull --confirm
```

The wrapper checks that the pinned Projects plugin is installed and fails before
Stripe can prompt for implicit installation.

## Storage Classes

| Class | Stored in | Consumed by | Rule |
| --- | --- | --- | --- |
| `provider_project` | Stripe Projects vault or provider-native vault | Operator import tooling | Local handoff only. Never consumed by Nomad jobs. |
| `site_config` | OpenBao `site-config/config/<name>` | Host bootstrap, provider bootstrap, and runtime seed import | Environment-scoped site configuration. |
| `host_credstore` | `/etc/credstore/...` | Host daemons and local jobs | Host file cache owned by Ansible. |
| `runtime_secret` | OpenBao KV v2 | Workloads through secrets-service or direct runtime injection | Runtime application secret material. |
| `product_kv` | secrets-service over OpenBao | Customers and product services | Customer/org-owned secrets and variables after deploy. |

## Replacement Boundary

Stripe Projects replaces provider signup, provisioning, credential generation,
credential sync, rotation, provider dashboard linking, and provider billing
handoff where the provider exists in the Projects catalog. It does not replace
the Verself runtime secret system, service-owned provider clients, provider
webhook verification, ClickHouse canaries, or host convergence.

| Surface | Stripe Projects role | Verself-owned role |
| --- | --- | --- |
| Resend or alternate email provider | Candidate provider provisioning and credential handoff. | Sender-domain DNS, OpenBao import, email-service canary. |
| PostHog, Sentry, OpenRouter, queue/cache/search providers | Preferred provisioning path when selected. | Catalog validation, OpenBao import, service config, canary evidence. |
| Cloudflare DNS/TLS | Possible provider account linking for supported services. | Parent-zone DNS token remains a bootstrap exception when using `verself.sh`. |
| Stripe Billing | No replacement for the product billing integration. | Billing service Stripe client, webhook endpoint, signing secret, catalog seed. |
| GitHub App and hosted runners | Provider-specific setup. | GitHub App, webhooks, runner prefix/group, runtime secrets, canaries. |
| Latitude bare-metal provisioning | Provider-specific setup. | OpenTofu allocation, inventory, host convergence. |

## Bootstrap Exceptions

Some provider credentials cannot be fully isolated because the provider scopes
authorization above the environment boundary. These credentials must be marked
`bootstrap_shared`, never imported as runtime secrets, and used only by
operator-controlled tasks.

| Provider surface | Reason | Allowed use |
| --- | --- | --- |
| Cloudflare parent-zone DNS token for `verself.sh` | Cloudflare API tokens scope DNS at zone level, not individual subdomains. | Delegate or reconcile gamma DNS records. |
| Cloudflare Email Routing token for the company domain | Company mail routing is account or zone scoped. | Operator mailbox routing. Avoid gamma use unless explicitly needed. |
| Shared provider billing account for Projects paid tiers | Payment method belongs to the Stripe account used by Projects. | Provider spend authorization with explicit limits. |

A bootstrap exception must include an owner, scope, provider permissions,
permitted commands, rotation path, and blast-radius statement. Runtime
declarations that reference a `bootstrap_shared` credential fail validation.

## Catalog Entry

The catalog is checked in as structured data and validated by an Aspect task
before deployment. Owner-local integration declarations can remain near the
service that consumes them, but the global catalog must answer where a value
lives, who owns it, and how it is verified without revealing plaintext.

```yaml
version: verself.integrations.v1
site: gamma
integrations:
  - key: billing.stripe
    provider: stripe
    owner: src/services/billing-service
    purpose: billing
    isolation:
      class: site_dedicated
      provider_environment: sandbox
    resources:
      - key: webhook_endpoint
        provider_id_ref: stripe_test_webhook_endpoint_id
        expected_url: https://billing.api.gamma.verself.sh/webhooks/stripe
    credentials:
      - key: billing-service.stripe.secret_key
        kind: api_key
        sensitivity: secret
        source: manual_provider_dashboard
        target: site_config
        openbao_key: stripe_secret_key
        consumer: src/services/billing-service/deploy/runtime-secrets.yml
        rotation: provider_dashboard_roll_key
      - key: billing-service.stripe.webhook_secret
        kind: webhook_secret
        sensitivity: secret
        source: provider_webhook_endpoint
        target: site_config
        openbao_key: stripe_webhook_secret
        consumer: src/services/billing-service/deploy/runtime-secrets.yml
    variables:
      - key: billing-service.stripe.publishable_key
        sensitivity: public
        source: provider_dashboard
        target: site_config
        openbao_key: stripe_publishable_key
    verification:
      - command: aspect deploy --site=gamma --sha=HEAD --post-deploy-checks=medium
      - evidence: billing Stripe webhook route accepts signed sandbox event
```

For a Stripe Projects-backed provider, `source` names the Projects environment
variable exported by `stripe projects env --pull`:

```yaml
  - key: analytics.posthog
    provider: posthog
    owner: src/services/analytics-service
    purpose: analytics
    provider_project:
      engine: stripe_projects
      project_name: verself-gamma
      service: posthog/analytics
    credentials:
      - key: analytics-service.posthog.project_api_key
        source: stripe_projects_env
        external_name: POSTHOG_PROJECT_API_KEY
        target: site_config
        openbao_key: posthog_project_api_key
```

## Runbook: Add Configuration

1. Classify the value.

   Use `public`, `confidential`, `secret`, or `key_material`. Configuration
   that changes provider resource identity, routing, IAM, billing behavior, or
   tenant isolation is cataloged even when it is public.

2. Choose the storage class.

   Use `site_config` for host bootstrap inputs, provider bootstrap inputs, and
   third-party runtime seed material; `runtime_secret` for workload
   consumption; and `product_kv` for customer/org-managed values after deploy.

3. Add or update the catalog entry.

   Include owner path, provider, environment policy, sensitivity, source,
   storage target, consumer, rotation path, revocation path, and verification
   evidence. Declare `bootstrap_shared` only when the provider cannot scope the
   credential to the environment.

4. Add the consumer declaration.

   Runtime service credentials use owner-local `deploy/runtime-secrets.yml`.
   Host file credentials use owner-local `deploy/credstore.yml`. Public
   provider variables belong in site vars, OpenBao site config, or generated
   service config, never in ad hoc Nomad literals when they vary by
   environment.

5. Populate the environment value.

   Prefer provider-project import. If the provider is not supported by Stripe
   Projects, write the value through `aspect site secret-put --site=<site>
   --name=<openbao_key>`.

6. Validate before deploy.

   The validator should reject undeclared OpenBao keys, undeclared
   `site_secret` references, runtime references to bootstrap-shared material,
   prod credentials in gamma or dev, and hardcoded environment-specific Nomad
   literals.

7. Deploy and capture evidence.

   Deploy with `aspect deploy --site=<site> --sha=<sha>` and use ClickHouse
   evidence plus provider-specific canaries to prove the value was consumed by
   the intended component.

## Runbook: Get Credentials For An Environment

Credential access starts with metadata. Operators list credential names,
owners, target stores, rotation state, and verification status without revealing
values.

```text
aspect integrations inventory --site=gamma
aspect integrations inventory --site=prod --provider=cloudflare
```

The command surface to build:

```text
aspect integrations credentials pull --site=gamma
aspect integrations credentials reveal --site=gamma --key=<catalog-key> --reason=<ticket-or-incident>
aspect integrations credentials rotate --site=gamma --key=<catalog-key>
```

`credentials pull` imports provider-project values into catalog-approved OpenBao
targets without printing plaintext. It must reject unrecognized environment
variable names from `.env` or provider-project output.

`credentials reveal` is break-glass. It requires a reason, writes an audit row,
prints at most one requested value, and never supports broad reveal. Production
reveal should require an explicit production flag and a Pomerium-authenticated
operator session.

Runtime services get credentials through OpenBao and secrets-service. They do
not read provider project files, local `.env`, shell history, GitHub Actions
secrets, or operator terminals.

## Runbook: Add An Integration

1. Search the provider catalog.

   Use Stripe Projects first when the provider and service are available:

   ```text
   stripe projects search <provider-or-capability> --json
   stripe projects catalog <provider> --json
   ```

2. Create or select the environment provider project.

   Use separate Projects for `prod`, `gamma`, and each durable `dev`
   environment. Run `stripe projects link <provider>` before provisioning when
   browser or account association is required.

3. Provision or attach the provider resource.

   Use `stripe projects add <provider>/<service>` for new resources. Use
   provider-native import or catalog metadata for resources that already exist.
   The provider resource ID must be recorded in the Verself catalog.

4. Pull credentials into a local handoff workspace.

   `stripe projects env --pull` refreshes the local vault and `.env` file for
   local handoff. The import tool reads only catalog-approved variable names and
   writes OpenBao site-config material.

5. Add service ownership.

   Add owner-local runtime, credstore, public route, and post-deploy canary
   declarations. If the integration changes customer-visible API behavior,
   include it in the Service Change Packet.

6. Add provider verification.

   Verification must exercise the provider boundary: webhook signature, API
   call, DNS resolution, email domain status, OAuth callback, queue publish, or
   equivalent. The canary emits ClickHouse evidence with site, provider,
   integration key, provider resource ID, and deploy run key.

7. Add rotation and revocation.

   Every credential has a documented rotation command, expected propagation
   path, propagation behavior, and revocation action. Rotation must update the
   provider project and Verself target store in one operator flow.

## Validator Rules

The catalog validator should run in CI and before deploy.

- Every OpenBao `site-config` key has a catalog entry.
- Every `site_secret` reference in `deploy/runtime-secrets.yml` and
  `deploy/credstore.yml` resolves to exactly one catalog entry.
- Every environment-specific Nomad literal is represented as site vars or a
  cataloged variable.
- `bootstrap_shared` credentials are denied as runtime secret sources.
- `prod`, `gamma`, and `dev` provider resource IDs do not match unless the
  catalog marks the value as intentionally shared.
- Credential values are never logged, printed in JSON, or committed in `.env`,
  `.projects/vault`, `.projects/cache`, or generated artifacts.
- Provider project imports reject unknown environment variable names.
- Rotation metadata exists for all `secret`, `key_material`, and
  `webhook_secret` entries.

## Evidence

Credential and integration operations emit operational evidence:

| Event | Evidence |
| --- | --- |
| Catalog validation | Site, integration key, owner, storage target, missing or extra keys. |
| Provider import | Site, provider project ID, variable names, target keys, value fingerprints, no plaintext. |
| Host materialization | Credstore paths, groups, modes, consumer components. |
| OpenBao seed | Mount, namespace, secret names, version fingerprints. |
| Runtime read | Workload SPIFFE ID, secret name, OpenBao role, result class. |
| Provider canary | Provider, resource ID, endpoint, route, deploy run key, pass/fail. |

Completion evidence for integration work is a successful deploy plus
provider-specific canary evidence in ClickHouse.

## Initial Build Order

1. Add the integration catalog schema and validator.
2. Populate prod inventory from `runtime-secrets.yml`, `credstore.yml`,
   provider tasks, site vars, and hardcoded provider config.
3. Add gamma catalog entries and mark bootstrap-shared Cloudflare exceptions.
4. Add OpenBao site-config import for a new site from the catalog.
5. Add provider-project import for Stripe Projects-backed providers.
6. Add credential reveal, rotation, and provider canary evidence.
7. Gate deploy automation on catalog validation for non-bootstrap deploys.

## References

- Stripe Projects: https://docs.stripe.com/projects
- Stripe testing environments and sandboxes:
  https://docs.stripe.com/testing-use-cases
- Stripe API key management:
  https://docs.stripe.com/keys-best-practices
