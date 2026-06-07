# Convergence Inventory

This inventory records the current disaster-recovery cutover shape. It is not a
historical incident log.

## Data Model

The source of truth is the Guardian CUE site graph plus component CRDs. Static
deployment facts live with the owning component, not in a centralized deployment
declaration layer. Bazel emits deployable artifacts, artifact metadata,
component-owned `nomad.hcl`, and component-owned generated `nomad.vars.hcl`.

Artifact promotion is content-addressed:

- Bazel reports the digest for each fly artifact.
- Guardian uploads build outputs under `<repoRoot>/artifacts/sha256/<digest>`.
- Guardian no longer synchronizes a wholesale `bazel-bin` tree to the target.
- Nomad receives concrete HCL vars: `repo_root`, `artifact_root`, `site`, and
  the component-owned var file.
- Components use those vars inside `nomad.hcl`.
- Unchanged digests mean no byte upload and no meaningful Nomad job change.

Secrets are runtime resources:

- Provider authority originates outside the repo and is imported or rotated into
  OpenBao.
- Generated site-local credentials are produced by the component responsible for
  the integration.
- Consumers declare required `SecretPath` references or health dependencies.
- Missing or unhealthy credentials cause allocation failure or recovery failure
  with structured evidence.

## State Machines

Before:

- Static `src/**/deploy/*.yml` files declared routes, environment, secrets,
  databases, and canary gates.
- Guardian deployment code consumed centralized declarations.
- Component-specific Nomad submission and monitoring logic leaked into Guardian.
- Artifact upload was broad `bazel-bin` synchronization.
- Canary intent was represented by static gate files.

After:

- `guardian fly`: load site graph -> build `fly_artifacts` -> upload changed
  digests -> preflight root services -> plan and submit Nomad jobs with var
  files -> poll evaluations/allocations -> emit evidence.
- Component recovery: Nomad allocation starts -> prestart/templates resolve
  secrets -> reconcile task applies runtime state -> service task reaches
  healthy -> `/recoveryz` and ClickHouse evidence confirm.
- OpenBao: absent/uninitialized -> init or restore -> unseal -> configure
  mounts, policies, and Nomad JWT roles -> generated/imported secret authority
  available. Existing initialized gamma state additionally needs an
  operator-authorized reconcile for missing roles; preflight does not retain a
  root token.
- Credentials: missing -> producer obtains or imports authority -> producer
  writes OpenBao secret with metadata -> consumer allocation validates ->
  healthy, expired, revoked, or unhealthy evidence.
- Canary/promotion: component emits recovery and health evidence -> canary
  runner records success, failure, or skipped -> promotion decision consumes
  evidence.

## Current Gamma Inventory

| Component | Current convergence owner | Current status | Next proof |
| --- | --- | --- | --- |
| Guardian preflight | Guardian CLI + Ansible preflight playbook | Uploads the repo/workspace graph plus content-addressed Bazel outputs and prepares root services; verified on gamma from wiped OpenBao/Nomad state | Keep fast path and wiped-host path green |
| OpenBao | Preflight root service via `openbao-recover` and systemd | Installs, starts, initializes/restores, unseals, bootstraps `SecretPath` KV mounts, generated secret values, import handoff, and Nomad JWT auth, then revokes fresh init root token | Fresh gamma wipe with Cloudflare import |
| Nomad | Preflight root service via systemd | Starts the agent, exposes workload-identity JWKS before OpenBao JWT auth setup, and validates the Podman driver | Add the next component job to the fly loop |
| Podman | Preflight root prerequisite | Nomad loads repo-local image archives through the Podman driver | More services run from repo-local image archives |
| Cloudflare Control Plane | Component-owned Nomad recovery job | Converged on gamma after operator import; account-admin and R2 recovery authority are now written through OpenBao | Re-run from wiped gamma and confirm same path |
| PostgreSQL | Component-owned Nomad recovery job | Submitted by generic `guardian fly`; blocked before setup because the already-initialized gamma OpenBao instance lacks the `postgresql-runtime` Nomad JWT role | Re-run from wiped OpenBao or provide operator authority to reconcile existing OpenBao auth |
| Static deployment facts | Guardian CRDs and site graph | PostgreSQL databases, public routes, runtime credential references, and promotion signals are expressed in the graph instead of a separate declaration layer | Add graph validators for cross-resource invariants |
| Runtime secrets | Producer-owned component recovery jobs plus OpenBao | Generated values are created by OpenBao bootstrap or by the provider integration that owns the external API; consumers reference `SecretPath` resources and fail their allocations when required credentials are absent or unhealthy | Add component-owned health conditions for produced credentials and consumer prestart checks |
| Profile Service | Component-owned Nomad job | First service cutover to OCI/Podman; static auth audience is in the CRD | Nomad alloc runs migration and service from `docker-archive:` |
| Distribution Service | Component-owned Nomad job | Cut over to OCI/Podman; static auth audience and release policy inputs are in the CRD | Nomad alloc runs migration and service from `docker-archive:` |

## Observed Errors

| Component | Error | Root cause | Resolution |
| --- | --- | --- | --- |
| Deployment artifact identity | deploy/fly must submit stable Nomad jobs from Bazel-owned bytes | OCI-native services use Bazel-produced image archives and job vars owned by the component package | unchanged jobs submit the same bytes and metadata, so Nomad receives a stable jobspec |
| OpenBao operator import | account-admin import returned OpenBao 403 after decrypting init material | the import token was a child of the transient initial root token and was revoked when the initial root token was revoked | create the short-lived operator-import token as an orphan token before revoking the initial root token |
| Cloudflare recovery OCI task | Cloudflare API verification failed with `x509: certificate signed by unknown authority` | scratch-style OCI image had no CA trust roots | mount host `/etc/ssl/certs` read-only into the recovery task |
| Cloudflare certificate issuance | certificate writer failed with `mkdir /etc/haproxy: read-only file system` | recovery task root filesystem is read-only, while TLS output is intentionally `/etc/haproxy/certs` for HAProxy consumption | component-owned root prestart creates `/etc/haproxy/certs`; recovery task mounts `/etc/haproxy` read-write |
| PostgreSQL Nomad Vault token | allocation failed before task start with `role "postgresql-runtime" could not be found` | gamma OpenBao was initialized before PostgreSQL's workload role existed, and no operator authority was available to mutate auth roles in-place | fresh OpenBao bootstrap now creates `postgresql-runtime`; existing initialized stores need an explicit operator-authenticated reconcile |
| Duplicate deployment facts | component facts were declared outside the component CRD/site graph contract | duplicate declaration layers split validation and ownership | express deployment facts once in component CRDs/site graph and component-owned Nomad vars |

## Current Rules

- OpenBao is not a Nomad job.
- OpenBao recovery only bootstraps generic trust-store substrate: KV v2 mounts
  implied by `SecretPath`, generated `source: generated` values, encrypted
  operator-import handoff for `source: operatorImport`, and Nomad workload JWT
  roles derived from root-service substrate needs.
- OpenBao recovery does not import provider credentials. Existing initialized
  stores require operator authority before new OpenBao auth roles can be added.
- Services run as OCI images through the Nomad Podman driver.
- OCI-bound Go binaries are built static so scratch-style image layers do not
  depend on host distro libraries.
- Image archive and runtime artifact digests must be part of component-owned
  generated Nomad vars, so changed bytes produce a new Nomad evaluation.
- Batch recovery jobs skip when the same image digest has already completed,
  but a failed or lost batch allocation is purged and resubmitted so operator
  imports can be retried without changing job bytes.
- The repo upload is source and generated graph state. `guardian run bazel --
  build //src/guardian-specification/examples/<site>:fly_artifacts` records
  Bazel-reported outputs in `.guardian/build/manifest.json`, and the preflight
  playbook uploads those outputs once by SHA-256 digest under
  `<repoRoot>/artifacts/sha256`.
- `guardian fly` plans each declared Nomad job, submits only changed jobs with
  Nomad's plan check index, and treats unchanged plans as successful no-ops.
- Component packages own their Nomad jobs, OCI image/archive outputs, and
  component CRD schemas.
- PostgreSQL databases, public routes, runtime credential references, and
  promotion signals are expressed in component CRDs and the site graph.
- No fixed host ports for services. Nomad allocates dynamic ports.
- No numeric UID/GID contracts. Host accounts are reconciled by name only when a
  current host boundary requires a host identity.
- Static nonsecret configuration belongs in component CRDs.
- Secret values originate from OpenBao or provider control planes after the
  trust store is available. The component that owns the provider integration
  produces the provider credential; consumers only declare typed references and
  fail allocation startup when required credentials are missing, expired, or
  unhealthy.

## Gamma Secret Authority

For the current gamma fly loop, Cloudflare has the provider authority needed to
reconcile the Cloudflare control-plane job: the account-admin import path
completed and the R2 recovery authority is available through OpenBao.

PostgreSQL is not blocked on a provider secret. It is blocked on OpenBao auth
state: the initialized gamma OpenBao store does not currently expose the
`postgresql-runtime` Nomad JWT role needed by the PostgreSQL allocation. There
are two valid ways forward:

- wipe/re-bootstrap OpenBao so preflight recreates the auth role set from the
  current resource graph;
- run an explicit operator-authenticated OpenBao reconcile against the existing
  store.

The first path proves disaster recovery from current source. The second path is
the day-two mutation path and needs an explicit operator authority flow, because
preflight intentionally does not retain root authority after bootstrap.

## Dynamic Credential Pattern

Provider credentials are owned by the component that integrates with the
provider. Examples:

- Cloudflare Control Plane owns Cloudflare account/R2 child credentials.
- Source Code Hosting owns Forgejo automation/webhook credentials.
- Email integration owns Resend child API keys.

The producer job authenticates to OpenBao with Nomad workload identity, creates
or rotates the provider credential, writes the value and metadata to OpenBao,
and emits provider-health evidence. Consumer jobs read only the referenced
OpenBao path and include a prestart or startup check that fails loudly when the
credential is absent, expired too soon, scoped incorrectly, or marked unhealthy.

Promotion gates are not static files. A promotion controller can consume
component health, provider canary evidence, `/recoveryz`, Nomad allocation
state, and ClickHouse rows. Spinnaker or another canary engine may make
promotion decisions from that evidence, but basic `guardian fly` convergence
does not depend on a separate gate declaration layer.

## First Cutover Target

`profile-service` is the first service cutover because it had both legacy
patterns directly:

- a static Zitadel audience rendered through OpenBao/Nomad templates;
- fixed numeric UID/GID repair.

The new shape is:

- `ProfileService.spec.auth.audience` is static CRD configuration;
- the Nomad job projects the resource graph and OCI archive into a
  service-readable runtime root;
- `migrate` and `serve` run through the Podman driver from the projected image;
- service checks and HAProxy discovery continue to use Nomad service names and
  dynamic ports.
