# Configuration, Secrets, And Integration Runbook

Verself configuration and third-party integration state is managed through a
site-scoped catalog. The catalog records every external account,
resource, credential, public variable, render target, consumer, verification
step, and isolation exception. OpenBao is the durable secrets system. Provider
vaults and Stripe Projects are provisioning and handoff surfaces; product KV is
served by secrets-service on top of OpenBao.

Stripe Projects is the provider provisioning and credential-handoff model for
new integrations when the provider exists in the Projects catalog. A Stripe
project represents a single app or codebase, groups provider accounts,
services, resources, credentials, and environment variables, and supports
catalog search, provisioning, credential rotation, and `env --pull`. Stripe
Projects does not support named environments inside one project, so Verself uses
one provider project per deployment site.

```text
Verself site
  -> provider project
  -> integration catalog entry
  -> storage target
  -> materialization target
  -> runtime consumer
  -> verification evidence
```

## Site Boundary

Each site has independent provider resources and credentials unless the
catalog marks a credential as a bootstrap exception.

| Verself site | Provider project policy | Runtime isolation |
| --- | --- | --- |
| `prod` | Dedicated provider projects and live provider accounts. | Customer-facing. No gamma or dev credentials. |
| `gamma` | Dedicated provider projects and sandbox/test provider accounts where possible. | Pomerium-gated production rehearsal. No prod runtime credentials. |
| `dev` | Per-operator provider projects or disposable provider resources. | Operator-local. No prod credentials. |

Provider projects are a provisioning surface. Runtime services do not read
`.env`, `.projects/vault`, or provider CLI caches. Deployment imports only
catalog-approved credential names into OpenBao, then host convergence
materializes the runtime view.

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
aspect integrations stripe-projects --site=gamma --action=search --query=webhook
aspect integrations stripe-projects --site=gamma --action=env-pull --confirm
```

The wrapper builds the pinned Stripe CLI and Projects plugin through Bazel,
loads plugin manifest state from repo-local `.verself`, and passes the host
Stripe config with `--config` when one exists.

## Bootstrap State Machine

Bootstrap has an explicit state machine. A privileged coding agent may hold the
same operational authority as a human operator, but every transition is
performed by an authenticated principal and leaves audit evidence.

```text
S0 repo_metadata_only
  -> S1 stripe_authenticated
  -> S2 provider_bootstrap_credentials_available
  -> S3 controller_openbao_imported
  -> S4 bare_metal_allocated
  -> S5 host_openbao_installed
  -> S6 site_openbao_initialized
  -> S7 runtime_secret_policy_reconciled
  -> S8 nomad_ready
  -> S9 deployed
  -> S10 steady_state_rotation
```

| State | Meaning | Secret location | Exit transition |
| --- | --- | --- | --- |
| `S0 repo_metadata_only` | Catalog, site vars, provider resource declarations, and tfvars exist. | No plaintext secrets in repo. | User or privileged agent starts a bootstrap session. |
| `S1 stripe_authenticated` | The operator or agent has authenticated Stripe CLI/Projects locally. | Stripe config on the principal's machine only. | Initialize or select the site provider project. |
| `S2 provider_root_credentials_available` | Latitude, Cloudflare, and other pre-host API keys are acquired. | Operator session memory until imported. | Import catalog-approved root credentials. |
| `S3 controller_openbao_imported` | Root credentials and provider-project handoff values are stored in a controller OpenBao namespace for the target site. | Controller OpenBao. | Provisioning reads short-lived credentials from OpenBao. |
| `S4 bare_metal_allocated` | Latitude host exists and inventory can be written. | Provider credentials remain in controller OpenBao. | Host bootstrap connects over SSH. |
| `S5 host_openbao_installed` | Host convergence copies the OpenBao binary, configuration, TLS files, and service definition to the host. | No runtime secret values copied yet. | Start OpenBao and initialize the site store. |
| `S6 site_openbao_initialized` | The host OpenBao Raft store, wrapped recovery material, auth mounts, audit sinks, KV mounts, transit mount, and base policies exist for this site. The initial root token is used only inside the bootstrap transaction. | Site OpenBao and operator-held recovery material. | Reconcile runtime secret declarations and workload auth. |
| `S7 runtime_secret_policy_reconciled` | Generated runtime secrets are created through OpenBao transit/random, produced-secret placeholders, external secret declarations, OpenBao policies, and Nomad JWT roles are present. External provider values may still be missing until their integration lifecycle runs. | OpenBao is the source of truth for secret values and metadata. | Hand off to Nomad. |
| `S8 nomad_ready` | Base OS, SPIRE, OpenBao workload auth, and Nomad are ready. | Runtime reads use OpenBao/secrets-service. | Nomad deploys service jobs. |
| `S9 deployed` | Services run from Nomad and consume only OpenBao-backed runtime secrets. | Site OpenBao and product KV. | Post-deploy canaries pass. |
| `S10 steady_state_rotation` | Future credential changes use catalog-driven import, rotate, and revoke commands. | OpenBao only. | Repeat from provider handoff or rotation transition. |

The phrase "copy OpenBao" means install/copy OpenBao runtime assets to the new
host and then initialize that site's OpenBao store. It does not mean copying
prod's OpenBao data, root token, unseal keys, Raft store, or runtime secret
values into gamma.

The first controller is the only special case. If there is no controller OpenBao
yet, the authenticated operator session may hold the small set of secret-zero
values in process memory long enough to bring up the first OpenBao. Once any
controller OpenBao exists, new sites use `S3 controller_openbao_imported`
instead of plaintext files.

### Transition Rules

- No transition writes plaintext credentials to git, `.env`, generated Bazel
  outputs, shell history, or logs.
- Controller-owned provider authority may be used by provisioning, DNS, and TLS
  tasks. It cannot be a runtime secret source.
- Provider-project imports reject variables that are not declared in the
  catalog.
- Host OpenBao initializes its own site-local Raft store and recovery material.
  Sites never share OpenBao root tokens, unseal keys, or Raft state.
- The `bao operator init` response is the only normal path that exposes an
  OpenBao root token. Bootstrap uses that token in process memory to create
  base auth, audit, and scoped reconciliation state, then revokes or discards
  it. The token is not written to host files, controller files, git, logs, or
  generated artifacts.
- Disaster recovery may require a temporary root token generated from recovery
  material during a gameday or incident. No steady-state automation handles
  that path.
- Runtime services authenticate with SPIFFE JWT-SVIDs or Nomad workload
  identity mapped to scoped OpenBao policies.
- Runtime declarations with `generated` source request entropy from OpenBao
  transit/random and then store the resulting value in OpenBao KV. Workload
  reconcilers do not use process-local RNG for runtime secret material.
- Owner-local deployment declarations define every runtime secret, provider
  credential, host credential projection, and consumer.
- Missing product-provider external OpenBao secrets do not block substrate reconciliation.
  The consuming workload or promotion gate reports the absent runtime secret.
- The successful transition to `S9 deployed` requires provider-specific canary
  evidence, not just a green Nomad allocation.

## Cloudflare Integration Service

`cloudflare-integration-service` owns Cloudflare provider lifecycle for Verself.
It is the only service that calls Cloudflare APIs. DNS reconciliation, ACME
DNS-01 records, Email Routing reconciliation, Cloudflare account authority, and
Cloudflare R2 provider capability lifecycle are modeled as Cloudflare
integration capabilities. Object-byte workflows are modeled by
`object-storage-service`; it uses `cloudflare-integration-service` as the R2
provider driver.

`secrets-service` is the storage and access-control boundary for Cloudflare
secret material. It enforces SPIFFE/JIT access policy, records audit evidence,
and stores encrypted values in OpenBao. OpenBao supplies KV and Transit
primitives. Cloudflare policy, rotation, verification, and provider state
machines remain in `cloudflare-integration-service`.

Cloudflare account authority is global and anchored to prod. Site names select
target DNS records, R2 prefixes, runtime capability credentials, and evidence
labels. Target hosts do not receive Cloudflare account authority during host
bootstrap.

For first-site recovery, the operator provides one local Cloudflare
account-admin token through the recovery configuration consumed by the
Cloudflare integration recovery job. That job derives and verifies
bucket-scoped R2 child credentials, then imports only the runtime child
credentials into site OpenBao. After the site is alive, steady-state
Cloudflare authority is service-owned and stored through `secrets-service`.

The account-admin credential has the minimal Cloudflare account permissions
required to verify, create, update, and delete account-owned R2 capability
credentials, reconcile DNS, issue DNS-01 challenge records, and reconcile Email
Routing. Overlapping provider credential generations are a steady-state
`cloudflare-integration-service` concern after bootstrap, with new generations
persisted and verified before old generations are revoked.

The site root key initializes and unseals the site OpenBao instance. Site
OpenBao then creates runtime DEKs and stores imported provider credentials under
site-local policy. Cloudflare account tokens, R2 credentials, DNS authority,
Stripe keys, Resend full-access authority, and GitHub App private material
originate from provider control planes and enter OpenBao through service-owned
import or rotation paths.

DNS authority is verified by listing hosted zones referenced by target site
metadata and recording zone ID fingerprints. DNS reconciliation accepts explicit
desired records, builds a provider diff, applies the changes, verifies provider
state, and emits evidence. It produces records and evidence, not runtime
credential material.

TLS/certificate issuance is an explicit public-edge transition. ACME DNS-01
issuance requests short-lived TXT records from `cloudflare-integration-service`
and deletes them after authorization. The resulting HAProxy PEM is host runtime
material and is not part of host bootstrap.

`object-storage-service` owns S3-compatible object sessions for deployment
artifacts, product object storage, and recovery bytes. Deployment-service submits
an artifact manifest to `object-storage-service`, uploads bytes to the returned
S3-compatible write handles, completes the object write session, and receives
read handles for Nomad. Deployment-service treats the provider as an
object-storage implementation detail.

R2 buckets are global capability resources:

| Capability | Bucket | Runtime capability |
| --- | --- | --- |
| Deployment artifacts | `verself-deployment-artifacts` | object-storage deployment-artifact write sessions and read handles |
| Product object storage | service-declared bucket set | object-storage S3-compatible transfer handles and product credentials |
| Recovery and backup bytes | `verself-recovery` | object-storage recovery writer and reader handles |

Site identity is encoded in object prefixes and capability policy metadata:

```text
verself-deployment-artifacts/<site>/sha256/<artifact-sha256>/...
verself-deployment-artifacts/<site>/candidate/<deploy-run-key>/...
verself-recovery/<site>/...
```

R2 capability credentials are bucket-scoped Cloudflare child credentials.
`cloudflare-integration-service` creates a new generation, verifies real R2
access, persists the generation through `secrets-service`, and only then drains
the previous generation. A run that cannot verify the new generation leaves the
old generation active, emits a structured failure event, and emails the
operator. Silent retries, implicit fallback credentials, and deletion before
verification are invalid state transitions.

Object write sessions follow this state machine in `object-storage-service`:

```text
requested
  -> object_session_created
  -> caller_uploading
  -> provider_object_verified
  -> committed
  -> read_handle_issued
  -> retained
  -> expired_or_gc_eligible
```

## Storage Classes

| Class | Stored in | Consumed by | Rule |
| --- | --- | --- | --- |
| `provider_project` | Stripe Projects vault or provider-native vault | Import tooling | Local handoff only. Never consumed by Nomad jobs. |
| `controller_openbao` | Controller OpenBao root-trust namespace | Provisioning tools | Pre-host source of truth for provider handoff and first-host recovery inputs. |
| `site_openbao` | Per-site OpenBao KV v2 and transit | Host convergence, secrets-service, workloads | Durable source of truth after site OpenBao exists. |
| `host_runtime_file` | Nomad allocation `secrets/` directory or component-owned runtime config | Host daemons and local jobs | Derived projection only. Files are never secret sources and deploy tooling does not read them. |
| `runtime_secret` | OpenBao KV v2 | Workloads through secrets-service or direct runtime injection | Runtime application secret material. |
| `product_kv` | secrets-service over OpenBao | Customers and product services | Customer/org-owned secrets and variables after deploy. |

## Provider Project Boundary

Stripe Projects replaces provider signup, provisioning, credential generation,
credential sync, rotation, provider dashboard linking, and provider billing
handoff where the provider exists in the Projects catalog. It does not replace
the Verself runtime secret system, service-owned provider clients, provider
webhook verification, ClickHouse canaries, or host convergence.

| Provider surface | Stripe Projects role | Verself-owned role |
| --- | --- | --- |
| Resend or alternate email provider | Candidate provider provisioning and full-access authority handoff. | Child sending-key creation, sender-domain policy, runtime secret ownership, email-service canary. |
| PostHog, Sentry, OpenRouter, queue/cache/search providers | Preferred provisioning path when selected. | Catalog validation, OpenBao import, service config, canary evidence. |
| Cloudflare DNS/TLS/R2/Email Routing | Possible provider account linking for supported services. | `cloudflare-integration-service` reconciles Cloudflare provider state; `object-storage-service` owns object-byte sessions over the selected storage provider. |
| Stripe Billing | No replacement for the product billing integration. | Billing service Stripe client, webhook endpoint, signing secret, catalog entry. |
| GitHub App and hosted runners | Provider-specific setup. | GitHub App, webhooks, runner prefix/group, runtime secrets, canaries. |
| Latitude bare-metal provisioning | Provider-specific setup. | OpenTofu/Ansible provisioning, inventory, host convergence. |

## Bootstrap Exceptions

Some provider credentials cannot be fully isolated because the provider scopes
authorization above the environment boundary. These credentials remain
controller-owned, are never imported as runtime secrets, and are used only by
operator-controlled or privileged-agent-controlled tasks.

| Provider surface | Reason | Allowed use |
| --- | --- | --- |
| Cloudflare account-admin credential | Cloudflare DNS/TLS, Email Routing, and account-owned R2 are global account resources. | Used only by `cloudflare-integration-service` for DNS, ACME DNS-01, Email Routing, R2 capability credentials, and R2 bucket reconciliation. Object-byte sessions are requested through `object-storage-service`. |
| Shared provider billing account for Projects paid tiers | Payment method belongs to the Stripe account used by Projects. | Provider spend authorization with explicit limits. |

A bootstrap exception must include an owner, scope, provider permissions,
permitted commands, rotation path, and blast-radius statement. Runtime
declarations that reference a controller-owned provider authority credential
fail validation.

## Catalog Entry

The catalog should be checked in as structured data and validated by an Aspect
task before deployment. Owner-local integration declarations can remain near the
service that consumes them, but the global catalog must be able to answer
"where is this value, who owns it, and how is it verified" without decrypting
anything.

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
        target: runtime_secret
        target_store: site_openbao
        openbao_name: billing-service.stripe.secret_key
        consumer: src/services/billing-service/deploy/runtime-secrets.yml
        rotation: provider_dashboard_roll_key
      - key: billing-service.stripe.webhook_secret
        kind: webhook_secret
        sensitivity: secret
        source: provider_webhook_endpoint
        target: runtime_secret
        target_store: site_openbao
        openbao_name: billing-service.stripe.webhook_secret
        consumer: src/services/billing-service/deploy/runtime-secrets.yml
    variables:
      - key: billing-service.stripe.publishable_key
        sensitivity: public
        source: provider_dashboard
        target: site_vars
        site_var: stripe_publishable_key
    verification:
      - command: aspect deploy --site=gamma --sha=HEAD
      - evidence: billing Stripe webhook provider canary passes for the deploy run key
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
        target: runtime_secret
        target_store: site_openbao
        openbao_name: analytics-service.posthog.project_api_key
```

## Runbook: Add Configuration

1. Classify the value.

   Use `public`, `confidential`, `secret`, or `key_material`. Configuration
   that changes provider resource identity, routing, IAM, billing behavior, or
   tenant isolation is cataloged even when it is public.

2. Choose the storage class.

   Use `controller_openbao` for pre-host root-trust inputs, `site_openbao` for
   durable site material, `runtime_secret` for OpenBao runtime values, and
   `product_kv` for customer or org-managed values after deploy. Secret-zero
   values for the first controller exist only in the authenticated operator
   process and are not catalog storage targets.

3. Add or update the catalog entry.

   Include owner path, provider, environment policy, sensitivity, source,
   storage target, consumer, rotation path, revocation path, and verification
   evidence. Use `site_scoped` for provider credentials constrained to one
   Verself site.

4. Add the consumer declaration.

   Runtime service credentials use owner-local `deploy/runtime-secrets.yml`.
   Public provider variables belong in site vars or generated service config,
   never in ad hoc Nomad literals when they vary by environment. Bootstrap and
   operator-access host files are named bootstrap/access-plane state, not
   runtime credential declarations.

5. Populate the environment value.

   Prefer provider-project import. If the provider is not supported by Stripe
   Projects, import the value through the owning integration service or the
   catalog-aware credential command and record the manual source in the catalog.

6. Validate before deploy.

   The target validator should reject undeclared OpenBao names, undeclared
   runtime secret consumers, runtime references to bootstrap-shared material,
   prod credentials in gamma or dev, and hardcoded site-specific Nomad
   literals.

7. Deploy and capture evidence.

   Deploy with `aspect deploy --site=<site> --sha=<sha>` and use ClickHouse
   evidence plus provider-specific canaries to prove the value was consumed by
   the intended component.

## Runbook: Inspect Site Credential Metadata

Credential access starts with metadata. Operators list credential names,
owners, target stores, rotation state, and verification status.

```text
aspect integrations inventory --site=gamma
aspect integrations inventory --site=prod --provider=cloudflare
```

The command surface to build:

```text
aspect integrations credentials pull --site=gamma
aspect integrations credentials rotate --site=gamma --key=<catalog-key>
```

`credentials pull` imports provider-project values into the catalog-approved
OpenBao targets without printing plaintext. It must reject unrecognized
variable names from provider-project output.

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

   `stripe projects env --pull` refreshes the provider-local handoff material.
   The import tool reads only catalog-approved names and writes OpenBao entries.

5. Add service ownership.

   Add owner-local runtime secret, public route, and post-deploy canary
   declarations. If the integration changes customer-visible API behavior,
   include it in the Service Change Packet.

6. Add provider verification.

   Verification must exercise the provider boundary: webhook signature, API
   call, DNS resolution, email domain status, OAuth callback, queue publish, or
   equivalent. The canary emits ClickHouse evidence with site, provider,
   integration key, provider resource ID, and deploy run key.

7. Add rotation and revocation.

   Every credential has a documented rotation command, expected propagation
   path, rollback behavior, and revocation action. Rotation must update the
   provider project and Verself target store in one operator flow.

## Validator Rules

The catalog validator should run in CI and before `aspect deploy`.

- Every OpenBao target name has a catalog entry.
- Every OpenBao target name has exactly one owner-local consumer declaration.
- Every credential projection has a declared path, group, mode, consumer, and
  rotation path.
- Every site-specific Nomad literal is represented as site vars or a
  cataloged variable.
- `prod`, `gamma`, and `dev` provider resource IDs do not match unless the
  catalog marks the value as intentionally shared.
- Credential values are never logged, printed in JSON, or committed in `.env`,
  `.projects/vault`, `.projects/cache`, or generated artifacts.
- Runtime services never read local provider workspaces, shell environments,
  GitHub Actions secrets, or operator terminals.
- Provider project imports reject unknown variable names.
- Rotation metadata exists for all `secret`, `key_material`, and
  `webhook_secret` entries.

## Evidence

Credential and integration operations emit operational evidence:

| Event | Evidence |
| --- | --- |
| Catalog validation | Site, integration key, owner, storage target, missing or extra keys. |
| Provider import | Site, provider project ID, variable names, target keys, value fingerprints, no plaintext. |
| Host materialization | Bootstrap output paths and OpenBao runtime declaration names. |
| OpenBao reconciliation | Mount, namespace, secret names, version fingerprints, policies, and roles. |
| Runtime read | Workload SPIFFE ID, secret name, OpenBao role, result class. |
| Provider canary | Provider, resource ID, endpoint, route, deploy run key, pass/fail. |

Completion evidence for integration work is a successful deploy plus
provider-specific canary evidence in ClickHouse.

## Build Order

1. Add the integration catalog schema and validator.
2. Add owner-local declaration validators for runtime secrets, host credential
   projections, provider variables, and bootstrap-shared exceptions.
3. Add site catalog entries and mark Cloudflare account authority as an integration-service-owned bootstrap exception.
4. Add bootstrap session, controller OpenBao ingress, and site OpenBao
   reconciliation commands.
5. Add provider-project import for Stripe Projects-backed providers.
6. Add rotation and provider canary evidence.
7. Gate `aspect deploy` on catalog validation for non-bootstrap deploys.

## References

- Stripe Projects: https://docs.stripe.com/projects
- Stripe testing environments and sandboxes:
  https://docs.stripe.com/testing-use-cases
- Stripe API key management:
  https://docs.stripe.com/keys-best-practices
- OpenBao JWT auth method:
  https://openbao.org/api-docs/auth/jwt/
- Nomad Vault/OpenBao workload identity integration:
  https://developer.hashicorp.com/nomad/docs/secure/vault
- Nomad job `identity` block:
  https://developer.hashicorp.com/nomad/docs/job-specification/identity
- SPIFFE JWT-SVID:
  https://github.com/spiffe/spiffe/blob/main/standards/JWT-SVID.md
