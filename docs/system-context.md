# System Context

How the platform is currently wired together. Direction and target state are in `docs/product-direction.md`; when the two disagree, this doc describes what exists today.

## Service Architecture

High-level topology lives in `docs/architecture/service-architecture.md`. Port assignments are declared in `src/platform/ansible/group_vars/all/services.yml`; run `make services-doctor` to cross-check the declared map against live listeners on the box (supports `FORMAT=json|nftables`).

Bootstrap and operator-recovery secrets are SOPS-encrypted in `group_vars/all/secrets.sops.yml` and loaded at service start via systemd `LoadCredential=` into `$CREDENTIALS_DIRECTORY`. Repo-owned service-to-service authentication is SPIFFE/SPIRE; runtime third-party provider credentials are fetched from OpenBao by SPIFFE-authenticated services. See [`docs/architecture/workload-identity.md`](architecture/workload-identity.md).

Go services are written with the Huma v2 framework (<https://pkg.go.dev/github.com/danielgtaylor/huma/v2>). Do not write custom clients for Go services; generate them from an OpenAPI specification. Each service commits both an OpenAPI 3.0 spec (Go client generation via `oapi-codegen`) and a 3.1 spec (TypeScript client + Valibot validator generation via `@hey-api/openapi-ts`). Shared cross-service DTO wire language lives in `src/apiwire`; use it for Huma boundary DTOs and generated-client contracts instead of service-local 64-bit JSON encodings.

## Topology and Replication

Single-node is the default deployment — everything runs on one box with no replication. Adding two more nodes (three total) enables TigerBeetle consensus replication, ClickHouse ReplicatedMergeTree, Postgres streaming replication, and cross-node health monitoring with external paging. The single-node path is the current working target; the 3-node topology uses Netbird as the overlay.

## Safety Rings

- **Internet-Exposed:** frontend TanStack apps (`src/viteplus-monorepo/apps/*`), Go services (`src/sandbox-rental-service`, `src/mailbox-service`, `src/billing-service`'s webhook handler), Forgejo, Grafana. Hardened via nftables.
- **Private Subnet / Linux Userspace:** internal Go services (billing-service), databases (PostgreSQL, ClickHouse, TigerBeetle), self-hosted platform components (Zitadel, Stalwart).
- **Linux Root:** ZFS, `src/vm-orchestrator`.

## Self-Hosting and Third-Party Providers

Hard product requirement: everything self-hosted. Exceptions:

- **Backups.** Target providers: Backblaze B2, Cloudflare R2, AWS S3 (planned, via `zfs send`). Not yet implemented.
- **Domain Registrar:** Cloudflare.
- **Compute Provider:** Latitude.sh.
- **Email Delivery:** Resend (outbound). Inbound self-hosted via Stalwart.
- **Payments, Tax, Payment Methods:** Stripe.

## Auth and IAM

Zitadel is the sole IdP for humans, organizations, and customer/API credentials. All public Go service APIs import `src/auth-middleware/`, which validates JWTs against Zitadel's JWKS endpoint (cached, local crypto after first fetch). Identity (subject, org ID, roles, email) is extracted from token claims and attached to request context. Repo-owned workload identity is SPIFFE/SPIRE (see [workload-identity.md](architecture/workload-identity.md)); Zitadel machine users are not used for repo-owned service-to-service calls.

Auth at the web application level is treated only as a UX concern. Authentication and authorization happen in services validating JWTs and calling out to Zitadel, and sometimes at the DB level. Any violation of this principle is a critical security concern.

Full model — organization boundary, three-role IAM (`owner`/`admin`/`member`), capability catalog, API credentials, SCIM, TanStack Start server-owned OAuth sessions, browser CSP bearer isolation, and the single-node JWKS loopback path — lives in `src/platform/docs/identity-and-iam.md`.

We use OpenBao Transit for KMS and OpenBao KV for Secrets Management. OpenBao is a relying party for workload identity and the resource plane for secrets/KMS material: it accepts SPIRE-issued JWT-SVIDs and maps SPIFFE subjects to OpenBao policies. OpenBao is not the source of truth for repo-owned workload identity.

## Dual-Write Pattern

Services that produce data for both real-time UX and long-term analytics use **application-level dual write**: the service writes to PostgreSQL (for live sync via ElectricSQL → TanStack DB in the browser) and to ClickHouse (for dashboards, metering, historical queries) in the same request path. Consistency is verified by periodic reconciliation, same pattern as billing's 6-check `Reconcile()`.

ClickHouse's `MaterializedPostgreSQL` engine was evaluated as a CDC alternative and rejected — experimental, with replication-slot coupling risks on a single node. The planned replacement for application-level dual write is WAL-based CDC into ClickHouse; [`docs/architecture/change-data-capture.md`](architecture/change-data-capture.md) records the current PeerDB-based direction.

## Billing

Credit-based subscription billing with entitlements — a prepaid + metered hybrid. Monthly subscriptions grant entitlements like credits, access to digital goods, software licenses, and priority lanes; credits are consumed via metering events (token inference, vCPU/RAM/disk/network usage, build minutes). Full model: `src/billing-service/docs/billing-architecture.md`.

## Inbound Mail

Self-hosted inbound via Stalwart. Boundary, auth, storage, and the mailbox-service model: `src/mailbox-service/docs/inbound-mail.md`.

## Supply Chain

- Git repos (including this one) are hosted on the deployed Forgejo instance at `git.<domain>`.
- NPM mirror self-hosted via Verdaccio.

## Founder Focus Areas

- **Secure by default.** Above and beyond most SaaS options. Security is regularly audited and verified (work in progress).
- **Cheap.** The founder pays only for compute and object storage at commodity prices, not for DataDog operating margin.
- **Solve hard problems faced by new businesses** (aspirational, not yet fully implemented). Lowering a price for a metered product should propagate seamlessly: customer billing pages, marketing pricing sections, customer emails, end-of-month invoices reflecting usage at both old and new prices at a specified `effective_at`, metering updates, and customer support agents (not yet implemented) answering questions from safe tables. Achieved via a robust system of record + deterministic workflows.
- **Observable — o11y 2.0.** Logs, traces, metrics are one thing: the Wide Event. ClickHouse handles millions of writes per second; instrument aggressively. Easier to reduce noisy instrumentation than to backfill gaps.

## Arch at a High Level

- Only Ubuntu 24.04 on the bare-metal box.
- `vm-orchestrator` is the privileged Go host daemon managing Firecracker lifecycle (ZFS, TAP, jailer) and aggregating guest telemetry. `vm-guest-telemetry` is the Zig guest agent streaming 60Hz health frames over vsock port 10790.
- Current working bare-metal box: `ssh ubuntu@64.34.84.75`.
- Auth: Zitadel (Stalwart JMAP has a separate auth path).
- Payments: Stripe + TigerBeetle + PostgreSQL.
- `otelcol-config.yaml.j2` contains the custom otel collection config.

## Platform Contracts

- Avoid CLIs. Things talk to each other over HTTP.
- Start telemetry investigation with `make observe` — discoverability-first. See `docs/architecture/founder-workflows.md` for the full flow.
- `make clickhouse-schemas` reads all ClickHouse tables (ground truth). Prefer `make observe` first, fall back to raw `make clickhouse-query` when observe has no named query.
