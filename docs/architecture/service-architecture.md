
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

`src/apiwire` owns cross-service DTO language. It contains the shared Huma DTO field types, parsers, and OpenAPI schema providers that make service boundaries explicit. Service domain packages can keep native Go types such as `uint64`, `int64`, and service-local ID aliases; DTO structs at Huma boundaries own JSON encoding and generated OpenAPI.

Frontend-facing 64-bit identifiers are decimal strings, not JSON numbers. Response and body DTO fields use `apiwire.DecimalUint64` / `apiwire.DecimalInt64`; path params use `string` with `pattern:"^[0-9]+$"` and parse through `apiwire.ParseUint64` because Huma path decoding validates the path value before JSON unmarshaling.

Frontend-facing quantities may remain JSON numbers only when the generated OpenAPI proves `maximum <= 9007199254740991`. Unbounded 64-bit quantities must be decimal strings or carry explicit generated-client metadata such as `x-js-wire: bigint`.

Services do not return domain structs directly when those structs contain unsafe numeric fields. They return DTOs that express the wire contract and convert to/from domain types at the boundary. Generated TypeScript clients consume the OpenAPI 3.1 contract, then app-facing wrappers parse with Valibot and deliberately convert IDs to branded strings and quantities to safe numbers or bigint. App code should not consume raw `response.json()` from service APIs.
