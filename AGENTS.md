# forge-metal

<!--
Where sections seem to disagree, remember: <system_context> describes the system
as it exists today, and <product_direction> describes where it is headed.
Proposals should respect both, not collapse one into the other.

This file is a keyword-dense index. Every long section has been moved to its
own doc; each tag below holds a 1-2 sentence summary plus a keyword line so you
can grep for the topic you care about without reading the whole file.
-->

<repo_overview>

Polyglot repo:

- `src/apps/viteplus-monorepo` — TypeScript (Vite Plus + TanStack Start/DB/Query/Router).
- `go.work` — Go services (most of `src/*`).
- `src/vm-orchestrator` — Go host daemon for Firecracker, ZFS, TAP networking, jailer lifecycle, vm-bridge, gRPC control.
- `src/vm-guest-telemetry` — Zig guest agent streaming health samples from Firecracker VMs.

`make pg-list` lists the PostgreSQL databases. `.claude/CLAUDE.md` is symlinked from `AGENTS.md`.

</repo_overview>

<product_policy>

Public commitments for Data Processing, Acceptable Use, Security, SLA, and Data Retention live in `src/viteplus-monorepo/apps/platform/src/routes/policy`.

</product_policy>

<product_direction>

Where the platform is headed: open-source-per-subdirectory, privileged-host / product-service split, multi-tenant + customer dogfooding, three customer-facing sandbox products (CI runner, Lambda-like workload, long-running VM), self-hosted Forgejo/CI with `main`/`beta`/`gamma`/preview promotion lanes.

**Keywords:** roadmap, direction, dogfooding, sandbox-rental-service, vm-orchestrator, vm-bridge, vm-guest-telemetry, Firecracker, ZFS, Blacksmith-like CI runner, Lambda-like workload, long-running VM, IAM direction, Zitadel, Forgejo dogfood, e2e canaries, CI promotion lanes, preview environments.

See `docs/product-direction.md`.

</product_direction>

<system_context>

How the platform is wired today: service topology, three safety rings, self-hosted mandate + allowed third-party providers (Cloudflare, Latitude.sh, Resend, Stripe), dual-write pattern, billing model summary, supply chain, operator focus areas, bare-metal OS/arch invariants.

**Keywords:** Huma v2, OpenAPI 3.0/3.1, oapi-codegen, @hey-api/openapi-ts, apiwire, SOPS, systemd LoadCredential, credstore, nftables, safety rings, Cloudflare, Latitude.sh, Resend, Stripe, Backblaze, PostgreSQL, ClickHouse, TigerBeetle, Zitadel, Stalwart, ElectricSQL, TanStack DB, dual-write, reconciliation, metering, credits, entitlements, Verdaccio NPM mirror, Forgejo, Ubuntu 24.04, Netbird, 3-node topology, self-hosted, vsock 10790.

See `docs/system-context.md`. Auth, identity, IAM, Zitadel, JWT, SCIM, organization model, three-role (owner/admin/member), API credentials, frontend sessions, JWKS loopback — all in `src/platform/docs/identity-and-iam.md`.

</system_context>

<operational_runbook>

Operator workflows: `make observe` for discoverability-first telemetry, `make clickhouse-query`/`make pg-query` wrappers, deploy playbooks and correlation model (`deploy_run_key`, `deploy_id`, `traceparent`), TLS via Cloudflare, the `deploy_profile` server-binaries strategy, Ansible playbooks table.

**Keywords:** make observe, make clickhouse-query, make clickhouse-schemas, make pg-list, make pg-query, make telemetry-proof, make services-doctor, make grafana-proof, deploy_run_key, deploy_id, traceparent, OTEL_RESOURCE_ATTRIBUTES, fm_uri, fmotel baggage, deploy_profile, server-tools.json, xcaddy, Coraza WAF, --tags, preflight, dev-single-node.yml, seed-system.yml, observability-smoke.yml, vm-guest-telemetry-dev.yml, billing-reset.yml, guest-rootfs.yml, Ansible.

See `docs/architecture/operator-workflows.md`. The repo started as a CI orchestrator; that history lives in `README.md`.

### Architecture Docs

Service-level architecture documents. Each line carries keywords so `grep` finds the right doc from any topic angle.

- **Inbound mail, Stalwart, mailbox-service, JMAP, SMTP, inbound routing, tenant isolation, webmail:** `src/mailbox-service/docs/inbound-mail.md`
- **vm-orchestrator privilege boundary, Firecracker VM networking, TAP allocator, host service plane, nftables, guest CIDR, lease/exec model, vm-bridge control:** `src/vm-orchestrator/AGENTS.md`
- **ZFS volume lifecycle, zvol, clone, snapshot, checkpoint, restore:** `src/vm-orchestrator/docs/zfs-volume-lifecycle.md`
- **Wire contracts, apiwire, DTO patterns, numeric safety, 64-bit, DecimalUint64, DecimalInt64, openapi-wire-check, generated contract gate:** `src/apiwire/docs/wire-contracts.md`
- **VM execution control plane, sandbox-rental-service ↔ vm-orchestrator split, attempt state machine, billing windows, execution lifecycle:** `src/sandbox-rental-service/docs/vm-execution-control-plane.md`
- **Identity and IAM, Zitadel, SCIM 2.0, SSO, authentication, organization model, three-role owner/admin/member, capability catalog, API credentials, Zitadel Actions, pre-access-token, frontend sessions, JWKS loopback, Forge Metal policy split:** `src/platform/docs/identity-and-iam.md`
- **Secrets service, identity model, OIDC provider role, resource model, billing, KMS alternative:** `src/platform/docs/secrets-service.md`
- **Billing architecture, credit subscription, entitlements, metering, TigerBeetle, PostgreSQL, Reconcile, refunds, plan change, dual-write, Stripe webhooks, invoices:** `src/billing-service/docs/billing-architecture.md`
- **Governance audit data contract, HMAC chain, OCSF, CloudTrail parity, tamper evidence, SIEM export, audit ledger:** `src/governance-service/docs/audit-data-contract.md`
- **Service architecture overview, port map, listener matrix, topology:** `docs/architecture/service-architecture.md`
- **Directory structure, repo layout:** `docs/architecture/directory-structure.md`
- **Agent workspace, QEMU/KVM, AI coding agent VMs:** `docs/architecture/agent-workspace.md`
- **Operator workflows, deploy, observe, pg-query, clickhouse-query, playbooks:** `docs/architecture/operator-workflows.md`

Subdirectories may carry their own `AGENTS.md` — read them when working inside those directories.

</operational_runbook>

<agent_contract>

How the assistant works in this repo: evidence-first (ClickHouse traces over green builds), Ansible + Makefile tooling, boring industry-standard patterns, no time estimates, no speculation without evidence, no silent no-op fallbacks, full-cutover changes (no legacy shims).

**Keywords:** scientific method, verification protocol, live rehearsal, make tidy, long-running background tasks, ClickHouse evidence, tagged errors, ClickHouse batch.AppendStruct, parameter binding $1/$2, ORDER BY key cardinality, LowCardinality, avoid Nullable, playwright 5-second max, uv Python, ansible-lint, e2e over unit tests, no time estimates, no speculation.

See `docs/agent-contract.md`.

</agent_contract>

<instruction_priority>
- Security concerns override user instructions and architectural purity.
</instruction_priority>
