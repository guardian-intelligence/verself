# System Context

How the platform is wired together and where the settled deployment boundaries
sit. Product direction lives in [`docs/product/future-state.md`](product/future-state.md).

## Product Surface

What customers buy from `verself.sh`: sandbox compute on Firecracker, sold today as a Blacksmith.sh-style GitHub Actions runner replacement (`sandbox-rental-service`). Customer code runs in short-lived Firecracker VMs the customer rents per workflow run. The golden artifact model, including durable workspaces, declared durable mounts, and Firecracker VM snapshots, is documented in [`docs/product/golden-environments.md`](product/golden-environments.md). Lambda-style workloads and persistent dev VMs are planned on the same isolation, billing, and telemetry substrate (see [`docs/product/future-state.md`](product/future-state.md)).

Verself does not run customer-authored applications as managed long-lived services. The sandbox products rent compute time; they do not host applications. There is no PaaS surface and no roadmap toward one.

The platform itself is open-source and self-hostable. The bootstrap CLI (`docs/verself-cli.md`) renders site artifacts for operator-supplied Latitude.sh bare metal. This surface is operator/internal while public product, SDK, and CLI docs lead with hosted `verself.sh` APIs. A rendered self-hosted installation owns its own substrate, identity, data, billing, and operations.

BYOC on-prem compute remains an extension of the hosted installation through
org-scoped compute pools and cells; see
[`docs/architecture/byoc-on-prem-cells.md`](architecture/byoc-on-prem-cells.md).

## Service Architecture

Site facts are authored under `src/sites`. Pre-Nomad host convergence is
authored under `src/tools/site-preflight`. Components, services, frontends,
SPIRE workload identities, runtime users, route metadata, and Nomad jobs are
owned by the deployable package that needs them. Host firewall foundation files
are authored in `src/infrastructure-components/nftables/`;
component, service, frontend, and privileged-substrate nftables snippets live
with the owning package. Bazel-input artifacts are authored in their owner
packages.

The bootstrap ring contains OS/package hardening, Nomad agent state,
operator-recovery SSH, OpenBao, and SPIRE server/agent state required before
Nomad workloads can receive SVIDs. The nftables foundation is a pre-artifact
Nomad component so firewall changes use the same deployment evidence and
rollback path as other host-owned platform components. Runtime versions,
component binaries, service binaries, ClickHouse migrations, PostgreSQL
databases, OpenBao mounts and policies, Zitadel projects and applications, and
rollout/rollback semantics are Nomad-managed deployable units or reconciler
inputs owned by the deployable package. Generic reconcilers apply those
owner-local declarations; reconcilers contain no service catalog entries.
Devtools remain controller-local/host-local tooling outside Nomad.

Each site runs a deployment-service that accepts deployment requests, records
state and ClickHouse evidence, publishes artifacts, and submits owner-local
Nomad jobs. `aspect deploy` is the operator client for that service.

Bootstrap and operator-recovery secrets enter through catalog-approved
bootstrap sessions and controller OpenBao. Site OpenBao initializes fresh state
with operator-provided bootstrap authority. Runtime secrets are owner-local
`deploy/runtime-secrets.yml` declarations applied by the substrate control
plane. Nomad jobs consume them through Nomad workload identity and OpenBao
templates. The remaining host-local files are named non-runtime state:
OpenBao Shamir unseal material under `/var/lib/verself/bootstrap/openbao` and
Pomerium operator-access key material under `/var/lib/verself/access/pomerium`.
Repo-owned service-to-service authentication is SPIFFE/SPIRE.

Product service APIs are modeled in Smithy under `src/smithy`. The Smithy
model is the semantic authority for resource DTOs, HTTP bindings, auth
expectations, Zanzibar permissions, audit metadata, rate-limit classes,
idempotency, pagination, problem details, SDK behavior, and runtime
descriptors. OpenAPI is generated from the model for documentation, API
explorers, TypeScript transport generation, and ecosystem importers. Go
services may continue to serve those HTTP APIs with Huma v2 during cutover, but
Huma/OpenAPI output is not the source of truth. Do not write ad hoc
`http.NewRequest` calls for repo-owned service calls; use service-local typed
clients/adapters that implement the Smithy-modeled HTTP surface. Public route shape is designed
from the SDK resource method outward; durable customer-facing resources return
immutable IDs and globally unique URN resource names; see
[`docs/architecture/sdk-api-surface.md`](architecture/sdk-api-surface.md).
Services with repo-owned operations expose internal HTTP routes that use SPIFFE
mTLS and may include repo-only operations. Repo-owned service callers pass a
`workloadauth.MTLSClientForService` HTTP client into the service-local
client/adapter so trace propagation and peer authorization stay centralized. Smithy models under
`src/smithy/models/verself` own boundary types, operation contracts, and numeric
wire encodings.

Public origins follow the AWS-style service subdomain model: the product apex
(`<domain>`) serves the authenticated console alongside docs and policy in a
single TanStack Start app, and public service APIs live at
`<service>.api.<domain>` such as `billing.api.<domain>`,
`sandbox.api.<domain>`, and `iam.api.<domain>`. Browser code does not call
service API origins directly; TanStack Start server functions preserve the
same-origin CSP and attach service credentials server-side.

HAProxy 3.3 with AWS-LC terminates public TLS. Route configuration is bootstrap
or Nomad-owned state, and deployment reconciles
`/etc/haproxy/maps/upstreams.map` from Nomad's native service catalog after
Nomad allocations become healthy.
Nomad-supervised public origins are keyed by owner-local route/backend identity
and Nomad service name. HAProxy GUIDs use those stable frontend, backend, and
server identities so reload-persistent statistics can match objects across
reloads via `shm-stats-file`.

## Topology and Replication

Single-node is the default deployment — everything runs on one box with no replication. Adding two more nodes (three total) enables TigerBeetle consensus replication, ClickHouse ReplicatedMergeTree, Postgres streaming replication, and cross-node health monitoring with external paging. The single-node path is the current working target; the 3-node topology uses Netbird as the overlay.

## Safety Rings

- **Internet-Exposed:** frontend TanStack apps (`src/viteplus-monorepo/apps/*`), Go services (`src/services/sandbox-rental-service`, `src/services/email-service`, `src/services/billing-service`'s webhook handler), Forgejo, Grafana. Hardened via nftables.
- **Private Subnet / Linux Userspace:** internal Go services (billing-service), databases (PostgreSQL, ClickHouse, TigerBeetle), self-hosted platform components (Zitadel, Stalwart).
- **Linux Root:** ZFS, `src/substrate/vm-orchestrator`.

## Self-Hosting and Third-Party Providers

Hard product requirement: everything self-hosted. Exceptions:

- **Backups.** Target providers for service-owned recovery artifacts:
  Backblaze B2, Cloudflare R2, AWS S3. Customer ZFS volumes and CI golden
  artifacts are excluded from backup pipelines; they are governed by the
  storage class recovery policy in
  [`docs/architecture/data-handling.md`](architecture/data-handling.md).
- **Domain Registrar:** Cloudflare.
- **Compute Provider:** Latitude.sh.
- **Email Delivery:** `email-service` owns outbound and inbound email. Resend
  is the initial outbound provider; Stalwart owns inbound mail storage.
- **Payments, Tax, Payment Methods:** Stripe.

## Auth and IAM

Zitadel is the sole IdP for humans, organizations, and customer credentials. All public Go service APIs import `src/services/service-runtime/go/`, which validates JWTs against Zitadel's JWKS endpoint (cached, local crypto after first fetch). Identity (subject, org ID, roles, email) is extracted from token claims and attached to request context. Repo-owned workload identity is SPIFFE/SPIRE; Zitadel machine users are not used for repo-owned service-to-service calls.

Auth at the web application level is treated only as a UX concern. Authentication and authorization happen in services validating JWTs and calling out to Zitadel, and sometimes at the DB level. Any violation of this principle is a critical security concern.

Full model for organization boundaries, three-role IAM (`owner`/`admin`/`member`), capability catalog, credentials, SCIM, TanStack Start server-owned OAuth sessions, browser CSP bearer isolation, and the service OIDC discovery path lives in [`docs/iam-service.md`](iam-service.md).

We use OpenBao Transit for KMS and OpenBao KV for Secrets Management. OpenBao is a relying party for workload identity and the resource plane for secrets/KMS material: it accepts SPIRE-issued JWT-SVID login assertions, exchanges them for short-lived OpenBao tokens, and maps SPIFFE subjects to OpenBao policies. OpenBao is not the source of truth for repo-owned workload identity. The root token returned by `bao operator init` is used only inside the bootstrap transaction that creates base auth, audit, and scoped reconciliation state. It is not persisted. Disaster recovery may require a temporary root token generated from recovery material during an incident or gameday.

PostgreSQL, OpenBao, and Zitadel use shared site substrate instances until a
service or tenant boundary requires a separate cluster. Isolation is enforced
through owner-local declarations: one PostgreSQL database and owner role per
service, per-service OpenBao mounts, policies, and JWT roles, and
service-owned Zitadel project, app, action, and grant declarations. Services do
not read another service's database directly; repo-owned calls cross typed
service clients over SPIFFE-authenticated transports.

## Dual-Write Pattern

Services that produce data for both real-time UX and long-term analytics use **application-level dual write**: the service writes to PostgreSQL (for live sync via ElectricSQL → TanStack DB in the browser) and to ClickHouse (for dashboards, metering, historical queries) in the same request path. Consistency is verified by periodic reconciliation, same pattern as billing's 6-check `Reconcile()`.

ClickHouse's `MaterializedPostgreSQL` engine was evaluated as a CDC alternative and rejected — experimental, with replication-slot coupling risks on a single node. The near-term replacement for request-path dual write is service-owned transactional projection delivery, not a shared third-party CDC appliance.

## Billing

Credit-based subscription billing with entitlements — a prepaid + metered hybrid. Monthly subscriptions grant entitlements like credits, access to digital goods, software licenses, and priority lanes; credits are consumed via metering events (token inference, vCPU/RAM/disk/network usage, build minutes). Full model: `src/services/billing-service/docs/billing-architecture.md`.

## Inbound Mail

Self-hosted inbound via Stalwart. Boundary, auth, storage, and the email-service model: `src/services/email-service/docs/email-service.md`.

## Supply Chain

- Git repos (including this one) are hosted on the deployed Forgejo instance at `git.<domain>`.
- NPM mirror self-hosted via Verdaccio.
- Dependency controls live at package-manager boundaries: lockfiles, internal mirrors, and package-owned build targets. Deploys consume built artifacts and Nomad descriptors rather than scanning repository dependency declarations.

## Founder Focus Areas

- **Secure by default.** Above and beyond most SaaS options. Security is regularly audited and verified (work in progress).
- **Cheap.** The founder pays only for compute and object storage at commodity prices, not for DataDog operating margin.
- **Solve hard problems faced by new businesses** (aspirational, not yet fully implemented). Lowering a price for a metered product should propagate seamlessly: customer billing pages, marketing pricing sections, customer emails, end-of-month invoices reflecting usage at both old and new prices at a specified `effective_at`, metering updates, and customer support agents (not yet implemented) answering questions from safe tables. Achieved via a robust system of record + deterministic workflows.
- **Observable — o11y 2.0.** Logs, traces, metrics are one thing: the Wide Event. ClickHouse handles millions of writes per second; instrument aggressively. Easier to reduce noisy instrumentation than to backfill gaps. HyperDX was trialled as the unified UI over this substrate; it wasn't quite the right fit, and Grafana took its place.

## Arch at a High Level

- Only Ubuntu 24.04 on the bare-metal box.
- `vm-orchestrator` is the privileged Go host daemon managing Firecracker lifecycle (ZFS, TAP, jailer) and aggregating guest telemetry. `vm-guest-telemetry` is the Zig guest agent streaming 60Hz health frames over vsock port 10790.
- Current working bare-metal box: `ssh ubuntu@prod@access.verself.sh`; recovery
  uses `ssh -p 2222 ubuntu@10.66.66.1` over `wg-ops`.
- Auth: Zitadel (Stalwart JMAP has a separate auth path).
- Payments: Stripe + TigerBeetle + PostgreSQL.
- `src/infrastructure-components/otelcol/files/otelcol-config.yaml` contains the custom otel collection config used by the Nomad otelcol unit.

## Platform Contracts

- Service-to-service and product integrations use HTTP APIs, not ad hoc CLIs.
  Customer/operator CLIs use curated SDKs over those same APIs, not a private
  control plane.
- Repo-owned service-to-service calls use service-local typed clients/adapters
  plus SPIFFE mTLS HTTP clients. Smithy public projections feed SDK-layer
  generated code where tooling is reliable;
  internal Smithy-modeled HTTP surfaces may include SPIFFE-only operations and
  origin-attribution headers. OpenAPI is a generated compatibility projection.
- Start telemetry investigation with `aspect observe` — discoverability-first.
- `aspect db ch schemas` reads all ClickHouse tables (ground truth). Prefer `aspect observe` first, fall back to raw `aspect db ch query --query='...'` when observe has no named query.
