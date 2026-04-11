
```
                              Internet (port 25)
                                      │
                              ┌───────▼───────┐
                              │  Stalwart     │
                              │  (SMTP+JMAP)  │───── OTLP ──┐
                              └───────────────┘              │
                                                             │
                                    ┌─────────────────────────────────────────────────────────┐
                                    │                     Caddy (TLS + WAF)                    │
                                    │   allowlist routing, Coraza WAF, Stripe IP allowlist     │
                                    └──┬──────────┬──────────┬──────────┬──────────┬──────────┘
                                       │          │          │          │          │
                              ┌────────▼──┐ ┌─────▼────┐ ┌──▼───┐ ┌───▼────┐ ┌───▼──────────┐
                              │rent-a-    │ │billing-  │ │Zitadel│ │Forgejo │ │  HyperDX     │
                              │sandbox    │ │service   │ │(OIDC) │ │(git+CI)│ │  (obs UI)    │
                              │(webapp)   │ │(Go/Huma) │ │       │ │        │ │              │
                              └─────┬─────┘ └──┬───┬───┘ └──┬───┘ └───┬────┘ └──────────────┘
                                    │          │   │        │         │
                              ┌─────▼──────────▼┐  │   OIDC JWKS      │
                              │sandbox-rental-  │  │   (cached)       │
                              │service (Go/Huma)│  │        │         │
                              └──┬────┬────┬────┘  │        │         │
                                 │    │    │       │        │         │
                    ┌────────────▼┐   │  ┌─▼───────▼───┐    │    ┌────▼─────────────┐
                    │vm-          │   │  │auth-        │    │    │forge-metal CLI   │
                    │orchestrator │   │  │middleware   │    │    │(CI warm/exec)    │
                    │(Go daemon)  │   │  │(Go library) │    │    │imports           │
                    └──┬──────────┘   │  └─────────────┘    │    │vm-orchestrator   │
                       │              │                     │    └──────────────────┘
              ┌────────▼──────┐       │
              │  Firecracker  │       │
              │  VMs (jailer) │       │                    Data Stores
              │  ┌──────────┐ │       │    ┌─────────────────────────────────────────┐
              │  │vm-guest- │ │       │    │                                         │
              │  │telemetry │ │       │    │  PostgreSQL ◄── billing schemas         │
              │  │(Zig agent│ │       │    │               ◄── sandbox job_logs      │
              │  │ 60Hz)    │ │       │    │               ◄── Zitadel event store   │
              │  └──────────┘ │       │    │               ◄── Forgejo metadata      │
              └───────────────┘       │    │               ◄── Stalwart mail store   │
                                      │    │                                         │
                                      │    │  TigerBeetle ◄── billing ledger         │
                                      │    │                   (Reserve/Settle/Void) │
                                      │    │                                         │
                                      │    │  ClickHouse  ◄── OTel logs/traces       │
                                      ├───►│               ◄── billing metering      │
                                      │    │               ◄── CI wide events        │
                                      │    │               ◄── sandbox job logs      │
                                      │    │               ◄── deploy events         │
                                      │    │                                         │
                                      │    │  MongoDB     ◄── HyperDX app state      │
                                      │    └─────────────────────────────────────────┘
                                      │
                              ┌───────▼───────┐
                              │  Stripe       │
                              │  (webhooks)   │
                              └───────────────┘
```

## Wire Contracts

See [wire-contracts.md](wire-contracts.md). `src/apiwire` owns shared Huma DTOs, decimal 64-bit JSON/OpenAPI types, and cross-service field language. Service domain packages can keep native Go types, but Huma boundary structs use `apiwire` DTOs when a frontend, generated client, or another service consumes the shape.

## Identity And IAM

See [identity-and-iam.md](identity-and-iam.md). Zitadel owns identity and role assignments, Forge Metal owns product policy documents and organization management UX, and each Go service owns the operation catalog it enforces.
