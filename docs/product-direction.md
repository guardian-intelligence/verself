# Product Direction

Where the platform is headed. When this doc disagrees with `docs/system-context.md`, the latter describes what exists today and wins for ground truth; this doc describes the target state.

## Direction

- Each project under `src/` should be treated as its own public open-source repo.
- `vm-orchestrator` (Go daemon) is the single privileged host process that manages Firecracker VMs: ZFS clones/checkpoints, TAP networking, jailer lifecycle, vm-bridge control, and guest telemetry aggregation. It exposes a gRPC API over a Unix socket for service callers. `vm-guest-telemetry` (Zig) is the minimal guest agent streaming 60Hz health samples over vsock. `sandbox-rental-service` is the product control plane layered on that substrate.
- Runtime product services must never receive privileged host access. All ZFS, Firecracker, TAP, jailer, `/dev/kvm`, and `/dev/zvol` operations go through `vm-orchestrator`; services carry policy-checked refs over the orchestrator API, not host paths, dataset names, device paths, or privileged CLIs.
- Every service should:
  1. Be designed for use by customers in a multi-tenant, organization-based fashion and integrated into the policy and billing abstractions.
  2. Be designed such that we are the principal customers (dogfooding). We go through the same policy and billing abstractions, except our usage is unlimited and our bill at invoice time nets to zero after applying an adjustment. Not currently upheld for Mail; worth dogfooding there too. This philosophy is direction, not current state; uphold as the codebase is upgraded.
- Product IAM direction: Zitadel owns identity, organizations, users, OAuth/OIDC, project roles, and role assignments; Forge Metal owns the product policy model; each Go service owns and enforces its operation catalog. The platform ships working default role bundles and policy documents, then exposes customer editing through a constrained Forge Metal organization console rather than requiring founders to hand-author IAM documents. See `src/platform/docs/identity-and-iam.md`.
- Dogfood our own Forgejo and CI: establish `main`, `beta`, `gamma`, and per-branch preview environments of the entire system with automatic promotion. Dev branches merge to `gamma`; `gamma` bakes and runs more expensive automation tests, then promotes to `beta`; `beta` sees private invite-only users and has manual or time-gated promotion to `main`. Dev branches are accessible only by the founder and their agent.
- Define e2e canaries of our own infrastructure as repeatable, scheduled workloads.

## Sandbox Runtime Products

`sandbox-rental-service` sells isolated compute products built from the `vm-orchestrator` + `vm-bridge` + `vm-guest-telemetry` substrate. Firecracker provides the isolation boundary; ZFS zvols/checkpoints provide fast clone, restore, and persistent filesystem semantics; billing, IAM, logs, traces, metrics, and checkpoint policy remain in service-owned control-plane state.

The three customer-facing products:

1. A Blacksmith-like clean-room Actions runner: customers install a Forge Metal GitHub Action or Forgejo Actions equivalent and run repository workflows on Forge Metal Firecracker VMs for a 2–10x CI speedup.
2. Arbitrary workload execution: customers define Lambda-like workloads with a persistent filesystem, first invoked manually and later schedulable as minimum-60-second loops.
3. Long-running VMs: customers run persistent VM sessions on the same isolation, telemetry, billing, and checkpoint substrate.

Dogfood all three through the same org, IAM, billing, telemetry, and checkpoint paths customers use. Internal usage is unlimited by entitlement and nets to zero at invoice time via adjustment, not by bypassing product control planes.
