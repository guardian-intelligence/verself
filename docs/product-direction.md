# Product Direction

- Dogfood our own Forgejo and CI: establish `main`, `beta`, `gamma`, and per-branch preview environments of the entire system with automatic promotion. Dev branches merge to `gamma`; `gamma` bakes and runs more expensive automation tests, then promotes to `beta`; `beta` sees private invite-only users and has manual or time-gated promotion to `main`. Dev branches are accessible only by the founder and their agent.cy-checked refs over the orchestrator API, not host paths, dataset names, device paths, or privileged CLIs.
- Every service should:
  1. Be designed for use by customers in a multi-tenant, organization-based fashion and integrated into the policy and billing abstractions.
  2. Be designed such that we are the principal customers (dogfooding). We go through the same policy and billing abstractions, except our usage is unlimited and our bill at invoice time nets to zero
- Define e2e canaries of our own infrastructure as repeatable, scheduled workloads.

## Sandbox Runtime Products

`sandbox-rental-service` sells isolated compute products built from the `vm-orchestrator` + `vm-bridge` + `vm-guest-telemetry` substrate. Firecracker provides the isolation boundary; ZFS zvols/checkpoints provide fast clone, restore, and persistent filesystem semantics; billing, IAM, logs, traces, metrics, and checkpoint policy remain in service-owned control-plane state.

The three customer-facing products:

1. A Blacksmith-like clean-room Actions runner: customers install a Verself GitHub Action or Forgejo Actions equivalent and run repository workflows on Verself Firecracker VMs for a 2–10x CI speedup.
2. Arbitrary workload execution: customers define Lambda-like workloads with a persistent filesystem, first invoked manually and later schedulable as minimum-60-second loops.
3. Long-running VMs: customers run persistent VM sessions on the same isolation, telemetry, billing, and checkpoint substrate.

Dogfood all three through the same org, IAM, billing, telemetry, and checkpoint paths customers use. Internal usage is unlimited by entitlement and nets to zero at invoice time via adjustment, not by bypassing product control planes.
