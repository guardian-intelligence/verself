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

See [identity-and-iam.md](../../src/platform/docs/identity-and-iam.md). Zitadel owns identity and role assignments. Forge Metal pins three role keys per project (`owner`, `admin`, `member`), exposes a fixed switchboard of code-owned member-capability bundles in the org console, and each Go service owns the operation catalog it enforces. Members can never receive a permission whose operation is not tagged `member_eligible: true`; the boundary is enforced at catalog `init()` and at credential issuance.

## Authorization Direction (Future)

Authorization today is enforced per service in Go: `auth-middleware` validates the Zitadel JWT, each service's `policy.go` maps roles and permissions to operations, and `x-forge-metal-iam` OpenAPI extensions surface the catalog to identity-service. The role-to-capability matrix is duplicated across services; retiring that duplication is the direction, not committed work.

The target is a centralized Policy Decision Point (PDP) — Open Policy Agent (OPA) is the planned choice — deployed as a sidecar beside each service. The shape:

- `x-forge-metal-iam` extensions remain the declared input contract.
- Per-service `policy.go` shrinks to: shape the OPA input, make a localhost decision call, honor the result. Rate limiting, idempotency, body limits, and governance audit emission stay in Go.
- Rego policies live in the monorepo, reviewed in PRs, tested with `opa test` in CI, distributed as signed bundles.
- Decision logs ship to ClickHouse alongside governance audit; the two are different grains — governance records business events, OPA records policy decisions.

Two layers, cleanly separated:

- **Operation-level authz** — "is this principal allowed to invoke this operation at all?" — becomes OPA's job.
- **Resource-level authz** — "once allowed, can this specific path or record be read/written?" — stays in the resource plane that owns the data (for example, OpenBao policies in the secrets plane once OpenBao is stood up).

This mirrors AWS's split: Zitadel is the IdP (equivalent to IAM Identity Center), OPA is the identity-based policy engine (equivalent to IAM), per-resource authz is resource-based policy, and `governance-service` is the CloudTrail-equivalent outside the authz path.

Until the PDP migration lands, current `policy.go` files are the reference implementation that future OPA policies must match. Keep role matrices declarative and avoid control-flow creep so the translation is mechanical.

## Secrets Plane

See [secrets-service.md](../../src/platform/docs/secrets-service.md). `secrets-service` is the customer-facing control plane for secrets, variables, dynamic credentials, and crypto operations across org, source, environment, and branch scopes; SPIFFE/SPIRE attests sandbox workloads; the service runs an OIDC provider so Forge Metal compute can federate to customer clouds without bootstrap API keys. OpenBao is the backend implementation, not the product contract.

## Deploy Trace Correlation

Ansible deploys emit OTLP traces via the upstream
`community.general.opentelemetry` callback; every span lands in
`default.otel_traces` with `ServiceName='ansible'`. There is no separate
`deploy_events` rollup. The canonical operator surface is
`make observe WHAT=deploy RUN_KEY=<deploy_run_key>`; raw history queries still
run over `otel_traces` directly when observe has no named query for the task.

Deterministic identity is exported by `src/platform/scripts/deploy_identity.sh`
before `ansible-playbook` runs:

- `deploy_run_key = YYYY-MM-DD.<counter>@<controller-host>`
- `deploy_id      = UUIDv5("forge-metal:" + deploy_run_key)`
- `TRACEPARENT    = 00-<hex(deploy_id)>-<stable_span>-01`
- `OTEL_RESOURCE_ATTRIBUTES = forge_metal.deploy_id=…,forge_metal.deploy_run_key=…,forge_metal.commit_sha=…,forge_metal.dirty=…,forge_metal.branch=…,forge_metal.commit_message=…,forge_metal.author=…,forge_metal.deploy_kind=…`

The callback inherits the TRACEPARENT-anchored trace-id, so its
playbook/task spans share it with every `fm_uri` probe (which emits the
same `traceparent` and a `baggage` header carrying the same
`forge_metal.*` members).

Two collector-side normalizations keep the query surface flat:

1. `transform/ansible_spans` in `otelcol-config.yaml.j2` rewrites the
   upstream span names (`<playbook>.yml`, `<task.name>`) to
   `ansible.playbook` / `ansible.task` and mirrors `forge_metal.*` from
   `ResourceAttributes` onto `SpanAttributes`.
2. `fmotel.baggageSpanProcessor` (`src/otel/otel.go`) copies every
   incoming baggage member with the `forge_metal.` prefix onto every
   span a service creates.

One ClickHouse query joins deploy and service spans:

```sql
SELECT SpanName, ServiceName, StatusCode
FROM default.otel_traces
WHERE SpanAttributes['forge_metal.deploy_id'] = '…'
ORDER BY Timestamp;
```
