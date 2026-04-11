## Project Structure

```
forge-metal/                            # Monorepo root
├── go.work                             # Go workspace (all src/*/ Go modules)
├── Makefile                            # Dev commands (wraps paths into src/)
├── docs/                               # Cross-cutting architecture docs
│
│   ── Shared libraries ──────────────────────────────────────────────────
│
├── src/auth-middleware/                # OIDC JWT validation (Go library)
│   └── go.mod                          # github.com/forge-metal/auth-middleware
├── src/apiwire/                        # Cross-service DTO language, JSON/OpenAPI wire types
│   └── go.mod                          # github.com/forge-metal/apiwire
├── src/otel/                           # Shared OTel bootstrap (Go library)
│   └── otel.go                         # TracerProvider + MeterProvider init
├── src/vm-orchestrator/                # Firecracker + ZFS VM orchestrator (Go, gRPC service + library)
│   ├── go.mod                          # github.com/forge-metal/vm-orchestrator
│   ├── server.go                       # gRPC server (Unix socket)
│   ├── api.go                          # Service API surface
│   ├── client.go, client_types.go      # gRPC client
│   ├── orchestrator.go                 # VM lifecycle: create, start, stop, destroy
│   ├── zvol.go                         # ZFS zvol helpers
│   ├── network.go                      # TAP + CIDR lease allocator
│   ├── telemetry_stream.go             # Guest telemetry aggregation
│   ├── proto/                          # gRPC protobuf definitions
│   ├── vmproto/                        # Host-guest vsock wire protocol
│   └── cmd/vm-bridge/                  # Guest PID 1/control bridge and in-guest snapshot CLI
│
│   ── Services ──────────────────────────────────────────────────────────
│
├── src/billing-service/                # Billing HTTP API (Go/Huma)
│   ├── go.mod                          # imports: auth-middleware, otel
│   ├── client/                         # Generated OpenAPI client (client.gen.go)
│   ├── openapi/                        # OpenAPI v3.1 spec
│   ├── cmd/
│   │   ├── billing-service/            # Main binary, systemd LoadCredential= for secrets
│   │   ├── billing-openapi/            # OpenAPI spec generator
│   │   ├── billing-seed/               # Seed catalog, plans, credits
│   │   └── tb-inspect/                 # TigerBeetle account inspector
│   ├── internal/
│   │   ├── billing/                    # Billing domain: Reserve/Settle/Void, grants, metering
│   │   ├── billingapi/                 # Huma HTTP handlers
│   │   └── runtime/                    # App lifecycle, PostgreSQL task worker
│   ├── postgresql-migrations/          # Billing PostgreSQL schema
│   └── testharness/                    # Test utilities
├── src/sandbox-rental-service/         # Sandbox product backend (Go/Huma)
│   ├── go.mod                          # imports: vm-orchestrator, billing-service/client, auth-middleware
│   ├── cmd/sandbox-rental-service/     # Main service binary
│   ├── internal/
│   │   ├── api/                        # Huma HTTP handlers
│   │   ├── checkpoints/                # Target checkpoint ref/version policy
│   │   ├── ci/                         # Target Forgejo/GitHub Actions workflow compiler
│   │   └── jobs/                       # Execution, segment, billing, checkpoint orchestration
│   ├── e2e/                            # End-to-end tests
│   ├── migrations/                     # executions, segments, checkpoints, logs PostgreSQL schemas
│   └── testharness/                    # Test utilities
├── src/mailbox-service/                # Inbound mail processing (Go/Huma)
│   ├── go.mod                          # imports: auth-middleware, otel
│   ├── cmd/
│   │   ├── mailbox-openapi/            # OpenAPI spec generator
│   │   ├── mailbox-service/            # Main HTTP + sync + forwarder binary
│   │   └── mailbox-tool/               # Typed operator CLI over the generated client
│   └── internal/
│       ├── api/                        # Huma HTTP handlers
│       ├── app/                        # App lifecycle
│       ├── forwarder/                  # Forwarding logic
│       ├── jmap/                       # JMAP client/session helpers
│       ├── mailstore/                  # PostgreSQL projection + mailbox bindings
│       ├── sessionproxy/               # JMAP session proxy logic
│       └── sync/                       # Stalwart discovery + mailbox reconciliation
│
│   ── Frontends ─────────────────────────────────────────────────────────
│
├── src/viteplus-monorepo/              # Vite+ (released March 2026 https://viteplus.dev/guide/dev) workspace for frontend applications
│   ├── apps/rent-a-sandbox/            # Customer-facing sandbox product frontend
│   └── packages/ui/                    # Shared frontend UI package
│
│   ── Standalone tools ──────────────────────────────────────────────────
│
├── src/vm-guest-telemetry/             # Firecracker VM guest telemetry agent (Zig)
│   ├── build.zig
│   ├── protocol/                       # Wire protocol spec
│   └── src/                            # 60Hz /proc sampler, vsock 10790 streamer
│
│   ── Platform ──────────────────────────────────────────────────────────
│
└── src/platform/                       # Infrastructure + deployment
    ├── ansible/
    │   ├── playbooks/                  # All orchestration (deploy, provision, vm-guest-telemetry-dev)
    │   └── roles/                      # Flat directory — deployment is a platform concern
    │       ├── deploy_profile/         # Build + download + install all server binaries
    │       ├── base/                   # OS hardening, users, credstore, SSH
    │       ├── nftables/               # Host firewall (forge-metal-firewall.target)
    │       ├── zfs/                    # Pool creation and ZFS dataset layout
    │       ├── caddy/                  # Edge proxy, TLS, WAF, route allowlist
    │       ├── postgresql/             # Shared PostgreSQL (one DB per service)
    │       ├── clickhouse/             # ClickHouse config + schema bootstrap
    │       ├── tigerbeetle/            # Financial ledger service
    │       ├── otelcol/                # OTel Collector → ClickHouse
    │       ├── electric/               # ElectricSQL sync service -- we run it via Podman but that's because it's annoying to get the raw binary. We accept it for now since we will use Podman after k3s migration.
    │       ├── billing_service/        # Billing service deploy + Zitadel auth project
    │       ├── sandbox_rental_service/ # Sandbox product deploy
    │       ├── mailbox_service/        # Mailbox service deploy
    │       ├── rent_a_sandbox/         # TanStack Start frontend deploy
    │       ├── stalwart/               # Receive-only mail: SMTP + JMAP + cert sync
    │       ├── resend/                 # Outbound email delivery config
    │       ├── forgejo/                # Git server
    │       ├── zitadel/                # Identity provider (OIDC)
    │       ├── hyperdx/                # Observability UI + MongoDB
    │       ├── verdaccio/              # Sealed npm registry mirror
    │       ├── firecracker/            # KVM, jailer, golden zvol, vm-orchestrator
    │       ├── containerd/             # Container runtime
    │       ├── cloudflare_dns/         # DNS record management
    │       ├── dev_tools/              # Dev tool installation
    │       └── ...                     # guest_rootfs, ci_fixtures, wireguard, etc.
    ├── terraform/                      # Latitude.sh provisioning
    ├── scripts/                        # clickhouse.sh, build-guest-rootfs.sh, traces.sh, mail-send.sh
    ├── migrations/                     # ClickHouse schemas (platform-level)
    ├── server-tools.json               # Pinned server binary versions + SHA256
    └── dev-tools.json                  # Pinned dev tool versions + SHA256
```
