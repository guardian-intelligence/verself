# Convergence Inventory

This inventory records the current disaster-recovery cutover shape. It is not a
historical incident log.

| Component | Current convergence owner | Current status | Next proof |
| --- | --- | --- | --- |
| Guardian preflight | Guardian CLI + Ansible preflight playbook | Uploads the repo/workspace graph and prepares root services; verified on gamma from wiped OpenBao/Nomad state | Keep fast path and wiped-host path green |
| OpenBao | Preflight root service via `openbao-recover` and systemd | Installs, starts, initializes/restores, unseals, bootstraps `SecretPath` KV mounts/import handoff and Nomad JWT auth, then revokes fresh init root token | Sealed restart recovery |
| Nomad | Preflight root service via systemd | Starts the agent, exposes workload-identity JWKS before OpenBao JWT auth setup, and validates the Podman driver | `guardian fly gamma` converges past Cloudflare root-material import |
| Podman | Preflight root prerequisite | Nomad loads repo-local image archives through the Podman driver | More services run from repo-local image archives |
| Cloudflare Control Plane | Component-owned Nomad recovery job | Runs from a repo-local static OCI image archive and reaches the explicit root-material-required gate when neither account-admin nor recovery R2 authority exists | Operator import makes account-admin or recovery R2 authority available |
| Profile Service | Component-owned Nomad job | First service cutover to OCI/Podman; static auth audience is in the CRD | Nomad alloc runs migration and service from `docker-archive:` |
| Distribution Service | Component-owned Nomad job | Cut over to OCI/Podman; static auth audience and release policy inputs are in the CRD | Nomad alloc runs migration and service from `docker-archive:` |

## Current Rules

- OpenBao is not a Nomad job.
- OpenBao recovery only bootstraps generic trust-store substrate: KV v2 mounts
  implied by `SecretPath` and encrypted operator-import handoff for
  `source: operatorImport`.
- OpenBao recovery does not import provider credentials, create generated
  runtime secret values, or reconcile service auth/readers.
- Services run as OCI images through the Nomad Podman driver.
- OCI-bound Go binaries are built static so scratch-style image layers do not
  depend on host distro libraries.
- Image archive digests must be part of the submitted Nomad job metadata/vars,
  so changed bytes produce a new Nomad evaluation.
- Batch recovery jobs skip when the same image digest has already completed,
  but a failed or lost batch allocation is purged and resubmitted so operator
  imports can be retried without changing job bytes.
- The repo upload is the golden image; Bazel-built artifacts are uploaded
  explicitly when they live under `bazel-bin`.
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
