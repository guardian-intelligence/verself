The list below is not exhaustive but should be referenced for engineering decisions and code review.

* Always lean on open standards where possible. Avoid re-inventing the wheel.
* Between competing options in the same problem space, seek the high-taste modern option. NATS JetStream over Kafka, TanStack over Next.js. Viteplus over assorted frontend tooling. Zig over C.
* Building on-ramps to our platform from existing platforms is extremely highly valued. That's why a GitHub App is our primary product service.
* Expect to build with the level of rigor that would make FedRAMP HIGH certification seem realistic.
* Keep OpenTofu provisioning lean -- It does a narrow job. Let Ansible keep the boxes in order, and Bazel for build graph and Nomad for deployment orchestration. Every layer does what it's good at.
* Use nftables for perimeter, host, and guest-boundary policy. Do not encode service-to-service reachability or dependency ports in nftables.
* Always think of the governance, IAM, quotas, and metering. Customers must know who did what, what they're allowed to do, and how much they used.
* The optimal frontend is a real-time server-rendered optimistically-updating with periodic resync thin client. TanStack + Electric solves most of this. We're moving to add a write-path sync engine.
* Think in terms of providing users a "Digital Habitat" -- their sessions should be synced across devices as much as possible.
* Never use useEffect. Very rarely, if ever, use `useState` -- prefer TanStack Query primitives for all state. Sync snowflake client-side state with the URL.
* SPIFFE mTLS solves some problems, unsure whether it's pulling its weight yet.
* We should consider a QEMU warm pool.
* No shell scripts. The only exceptions are the platform bootstrap entrypoints under `src/tools/dev/bootstrap/`. Choose the appropriate language and check the result into a Bazel target. Treat scripts as core load-bearing architecture + sharp knives. They are extremely dangerous and should be carefully reviewed.
