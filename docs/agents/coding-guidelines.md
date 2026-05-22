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
* Never construct OCSF events outside a single typed builder. Hand-rolled map[string]any events drift and break SIEM rules silently.
* Treat errors as data. Use tagged and structured errors to aid control flow.
* Avoid fallbacks and defaults. Runtime behavior should fail fast with useful logging.
* Avoid verbosity. When solving a specific problem, the patch should solve the general case. E.g. if solving a TOCTOU vuln, don't write a function named `fix_toctou_bug`, make the simple patch to use the toctou-safe call and optionally leave a comment (no more than a few words).
* Don't resolve failures through silent no-ops and imperative checks. Failures should be loud; signals should be followed to address root causes. Failures are useful data!
* When you run into a footgun, leave a comment around the code (no more than a sentence) explaining the footgun and how the code works around it.
* Browser coverage belongs in ongoing live canaries with ClickHouse evidence. Do not add frontend Playwright suites; the old frontend e2e harness has been retired.

* ClickHouse inserts must use `batch.AppendStruct` with `ch:"column_name"` struct tags. `batch.Append` silently corrupts data when columns are added or reordered.
* ClickHouse schema design: ORDER BY columns are sorted on disk and control compression — order keys by ascending cardinality (low-cardinality columns first). Avoid `Nullable` (it adds a hidden `UInt8` column per row); use empty-value defaults instead. Use `LowCardinality(String)` for columns with fewer than ~10k distinct values. Use the smallest sufficient integer type (`UInt8` over `Int32` when the range fits).
* Browser canaries should use short operation deadlines and diagnose behavior from traces, logs, and ClickHouse evidence instead of extending waits. Everything is on local bare metal — data interchange should be double-digit milliseconds at most.
* Our customers will use our services via API and browser. Fix issues at the service level; don't paper over them in any one domain. E2E test the browser primarily since it exercises the same API that API consumers call directly.
* No global, hand-managed /usr/local/bin. Let Bazel call out to package-specific toolchains for dev tools and deployment requirements.
* For local development, packages should offer to install onto the caller's $HOME/.local/bin, requiring an explicit --bin-dir. These shims should point back to Bazel-resolved outputs or package-manager-resolved binaries and not duplicate version state.

* Avoid drift between what runs in CI and what you run for local development. CI is basically a warm dev box. Local development should give high confidence on correctness.
