# Service Architecture

```mermaid
flowchart TB
  browser["Browsers<br/>customer APIs"]
  github["GitHub / Forgejo<br/>repository workflows"]
  smtp["Inbound SMTP"]
  stripe["Stripe<br/>webhooks + checkout"]

  caddy["Caddy<br/>TLS, WAF, routing"]
  stalwart["Stalwart<br/>SMTP + JMAP"]
  forgejo["Forgejo<br/>git hosting + Actions source"]
  zitadel["Zitadel<br/>OIDC, orgs, role assignments"]
  grafana["Grafana<br/>observability UI"]

  rent["rent-a-sandbox<br/>TanStack Start BFF"]
  identity["identity-service<br/>org + product IAM control plane"]
  sandbox["sandbox-rental-service<br/>compute product control plane"]
  billing["billing-service<br/>Reserve / Settle / Void"]
  authmw["auth-middleware<br/>local JWT validation"]

  actions["Actions runner product<br/>Blacksmith-like clean-room"]
  workloads["Arbitrary workload execution<br/>manual now, schedulable later"]
  longvms["Long-running VMs<br/>persistent sessions"]

  vmorch["vm-orchestrator<br/>privileged Go host daemon"]
  zfs["ZFS pool<br/>zvols, clones, checkpoint versions"]
  fc["Firecracker VMs<br/>jailer + TAP networking"]
  bridge["vm-bridge<br/>guest PID 1 + checkpoint control"]
  telemetry["vm-guest-telemetry<br/>Zig 60Hz health stream"]

  pg["PostgreSQL<br/>service schemas, execution state,<br/>checkpoint refs, frontend auth"]
  electric["ElectricSQL<br/>browser sync shapes"]
  clickhouse["ClickHouse<br/>OTel logs, traces, metrics,<br/>wide events, metering"]
  tigerbeetle["TigerBeetle<br/>billing ledger"]

  browser --> caddy
  smtp --> stalwart
  stripe --> caddy
  github --> actions

  caddy --> rent
  caddy --> sandbox
  caddy --> billing
  caddy --> identity
  caddy --> zitadel
  caddy --> forgejo
  caddy --> grafana
  caddy --> stalwart

  rent --> sandbox
  rent --> identity
  rent --> billing
  pg --> electric --> rent

  forgejo --> actions
  actions --> sandbox
  workloads --> sandbox
  longvms --> sandbox

  sandbox --> billing
  sandbox --> pg
  sandbox --> clickhouse
  sandbox --> vmorch
  billing --> pg
  billing --> tigerbeetle
  billing --> clickhouse
  identity --> pg

  authmw --> zitadel
  sandbox -. validates bearer JWTs .-> authmw
  billing -. validates bearer JWTs .-> authmw
  identity -. validates bearer JWTs .-> authmw

  vmorch --> zfs
  vmorch --> fc
  fc --> bridge
  fc --> telemetry
  bridge -- "host-authorized checkpoint requests" --> vmorch
  telemetry -- "vsock health frames" --> vmorch
  vmorch --> clickhouse

  clickhouse --> grafana
```

`sandbox-rental-service` is the product control plane for three related compute
products: a Blacksmith-like clean-room Actions runner, arbitrary workload
execution, and long-running VMs. These products must reuse the same runtime
substrate rather than developing separate runners: `vm-orchestrator` manages the
privileged host operations, `vm-bridge` exposes a narrow guest control surface,
`vm-guest-telemetry` streams health data, Firecracker provides the isolation
boundary, and ZFS zvols/checkpoints provide fast restore and persistent
filesystem semantics.

`sandbox-rental-service` owns customer semantics: organization policy, workflow
planning, execution records, checkpoint refs, billing windows, logs, public DTOs,
and the future scheduling model. `vm-orchestrator` owns privileged VM lifecycle
and ZFS operations. Guest checkpoint requests are untrusted input; the guest may
name only service-authorized checkpoint refs, and it must never provide org IDs,
ZFS paths, dataset names, or checkpoint version paths.

The next architecture gaps are customer secret management and block-layer
composition. Secret handling needs a first-class product service rather than
ad hoc execution env vars. zvol restore/composition belongs behind the
`sandbox-rental-service` checkpoint policy model and the `vm-orchestrator`
privileged restore API, not in customer-visible ZFS paths.

## Wire Contracts

See [wire-contracts.md](../../src/apiwire/docs/wire-contracts.md). `src/apiwire` owns shared Huma DTOs, decimal 64-bit JSON/OpenAPI types, and cross-service field language. Service domain packages can keep native Go types, but Huma boundary structs use `apiwire` DTOs when a frontend, generated client, or another service consumes the shape.

## Identity And IAM

See [identity-and-iam.md](../../src/platform/docs/identity-and-iam.md). Zitadel owns identity and role assignments, Forge Metal owns product policy documents and organization management UX, and each Go service owns the operation catalog it enforces.

## Secrets Plane

See [secrets-plane-openbao.md](../../src/platform/docs/secrets-plane-openbao.md). `secrets-service` is the customer-facing control plane for org/repo/environment secrets and variables, with OpenBao as the preferred backend implementation.

## Deploy Trace Correlation

Ansible deploys emit OTLP traces (`ServiceName='ansible'`) to `default.otel_traces`
through `deploy_traces.py`, while `deploy_events.py` writes one rollup row to
`forge_metal.deploy_events`. Both are keyed by the same deterministic identity:

- `deploy_run_key = YYYY-MM-DD.<counter>@<controller-host>`
- `deploy_id = UUIDv5("forge-metal:" + deploy_run_key)`
- `trace_id = hex(deploy_id)`

`fm_uri` carries this identity into service HTTP probes via `traceparent`,
`baggage`, and `X-Forge-Metal-*` headers. Go services attach those headers as
span attributes (`forge_metal.deploy_id`, `forge_metal.task_instance_id`,
`forge_metal.probe_id`, etc.), so a single ClickHouse query over `TraceId` can
show both deploy tasks and downstream service spans for proof-level debugging.
