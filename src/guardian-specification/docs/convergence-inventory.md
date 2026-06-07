# Convergence Inventory

This inventory records the current disaster-recovery cutover shape. It is not a
historical incident log.

| Component | Current convergence owner | Current status | Next proof |
| --- | --- | --- | --- |
| Guardian preflight | Guardian CLI + Ansible preflight playbook | Uploads the repo/workspace graph plus `.guardian/build/bazel-bin` materialized outputs and prepares root services; verified on gamma from wiped OpenBao/Nomad state | Keep fast path and wiped-host path green |
| OpenBao | Preflight root service via `openbao-recover` and systemd | Installs, starts, initializes/restores, unseals, bootstraps `SecretPath` KV mounts, generated secret values, import handoff, and Nomad JWT auth, then revokes fresh init root token | Fresh gamma wipe with Cloudflare import |
| Nomad | Preflight root service via systemd | Starts the agent, exposes workload-identity JWKS before OpenBao JWT auth setup, and validates the Podman driver | Add the next component job to the fly loop |
| Podman | Preflight root prerequisite | Nomad loads repo-local image archives through the Podman driver | More services run from repo-local image archives |
| Cloudflare Control Plane | Component-owned Nomad recovery job | Converged on gamma after operator import; account-admin and R2 recovery authority are now written through OpenBao | Re-run from wiped gamma and confirm same path |
| PostgreSQL | Component-owned Nomad recovery job | Submitted by generic `guardian fly`; blocked before setup because the already-initialized gamma OpenBao instance lacks the `postgresql-runtime` Nomad JWT role | Re-run from wiped OpenBao or provide operator authority to reconcile existing OpenBao auth |
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
- Image archive digests must be part of the submitted Nomad job metadata/vars,
  so changed bytes produce a new Nomad evaluation.
- Batch recovery jobs skip when the same image digest has already completed,
  but a failed or lost batch allocation is purged and resubmitted so operator
  imports can be retried without changing job bytes.
- The repo upload is the golden image; `guardian run bazel -- build ...`
  materializes Bazel-reported outputs into `.guardian/build/bazel-bin`, and the
  preflight playbook uploads that tree without following Bazel convenience
  symlinks.
- Component packages own their Nomad jobs, OCI image/archive outputs, and
  component CRD schemas.
- No fixed host ports for services. Nomad allocates dynamic ports.
- No numeric UID/GID contracts. Host accounts are reconciled by name only when a
  current host boundary requires a host identity.
- Static nonsecret configuration belongs in component CRDs.
- Secret values originate from OpenBao or provider control planes after the
  trust store is available.

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
