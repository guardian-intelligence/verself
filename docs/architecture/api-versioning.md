# API Versioning Library Specification

A Go library that ports [Cadwyn](https://github.com/zmievsa/cadwyn)'s Stripe-style API versioning pattern onto Huma v2 + `dto`. Handlers always see the latest ("HEAD") request/response shape; a chain of dated, declarative `VersionChange`s ferries old wire payloads forward and new wire payloads backward at the edge.

This document specifies the library's surface area and internals. Cadwyn is the reference implementation; we deviate where Go semantics or our existing toolchain demand it.

## 1. Naming and Home

- Module path: `github.com/verself/apiversion` (suggested; alternatives: `apirev`, `wireversion`)
- Repo location: `src/apiversion/`
- Public package: `apiversion`
- Internal sub-packages: `apiversion/internal/jsonschema` (validation), `apiversion/internal/specgen` (OpenAPI mutation), `apiversion/internal/changelog` (rendering)
- CLI entry points under `src/apiversion/cmd/`: `apiversion-render` (emit per-version specs), `apiversion-check` (lint changes), `apiversion-changelog` (emit markdown)

## 2. Mental Model

Service handlers, DB queries, business logic — all of these only ever know the HEAD shape of every request and response. A request from a client pinned to an older version is **migrated forward** through an ordered chain of small, atomic `VersionChange`s before the handler sees it. The handler's response is **migrated backward** through the same chain before the wire write. Each `VersionChange` declares two things: (a) *what changed in the schema* (drives OpenAPI generation per version) and (b) *how to translate a payload across that change* (drives runtime JSON mutation).

This is Cadwyn's mental model verbatim. Go semantics force one major divergence: **we do not generate per-version Go structs.** Cadwyn produces a fresh Pydantic model per version via runtime metaclass; Go has no equivalent and codegen would explode the package graph. Instead:

- Per-version *OpenAPI specs* are generated mechanically from the HEAD spec + change chain.
- Per-version *typed clients* (TS, Python, Swift, Go-external) are generated from those specs via `oapi-codegen`/`@hey-api/openapi-ts` per version.
- Per-version *server-side validation* is JSON Schema against the requested version's spec, performed on raw bytes before unmarshalling. Once migrated, the handler's HEAD struct receives a payload Huma already knows how to validate.

The Go server therefore deals in three representations: raw bytes (wire), `map[string]any` (during migration), and HEAD typed structs (handler). It never holds an old-version typed struct.

## 3. Public Surface

The user-facing API is small, orthogonal, and free of class-body magic.

```go
package apiversion

// A Bundle is the ordered list of versions a service exposes.
type Bundle struct { /* ... */ }

func NewBundle(versions ...Version) *Bundle

// A Version is a dated slot containing zero or more Changes.
type Version struct { /* ... */ }

func NewVersion(date string, changes ...*Change) Version

// A Change is the unit of breaking-change description. Constructed via builder.
type Change struct { /* ... */ }

func NewChange(opts ...ChangeOption) *Change

// Apply wires the bundle into a Huma API: middleware + per-version OpenAPI handlers.
func Apply(api huma.API, bundle *Bundle, opts ...ApplyOption) error

// IsApplied reports whether the change is "live" for the request's pinned version.
// Use sparingly — leaks versioning into business logic.
func IsApplied(ctx context.Context, change *Change) bool

// MigrateOutbound walks an outbound payload backward to a subscriber's pinned
// version. Used by webhook senders, async workers, audit emitters.
func MigrateOutbound(bundle *Bundle, schemaName string, headBody any, targetVersion string) ([]byte, error)

// PickedVersion returns the version the current request was matched to.
func PickedVersion(ctx context.Context) (string, bool)

// RenderOpenAPI returns the spec for one version. Used by CLI and per-version routes.
func RenderOpenAPI(api huma.API, bundle *Bundle, version string) ([]byte, error)

// RenderChangelog returns the auto-generated changelog as structured data.
func RenderChangelog(bundle *Bundle) Changelog
```

That is the entire top-level surface. Everything else is `ChangeOption` builders and `ApplyOption` configuration.

## 4. The `Change` Builder

Cadwyn uses class bodies + `__init_subclass__` validation. Go's equivalent is a functional-options builder.

```go
var ChangeAddressToList = apiversion.NewChange(
    apiversion.ID("user-address-to-list"),
    apiversion.Description("Allow multiple addresses per user."),

    apiversion.AlterSchema("UserCreateRequest",
        apiversion.Field("addresses").HadName("address"),
        apiversion.Field("addresses").HadType(apiversion.TypeString),
    ),
    apiversion.AlterSchema("UserResource",
        apiversion.Field("addresses").HadName("address"),
        apiversion.Field("addresses").HadType(apiversion.TypeString),
    ),

    apiversion.ConvertRequest("UserCreateRequest", func(r *apiversion.RequestInfo) {
        r.Body["addresses"] = []any{r.Body["address"]}
        delete(r.Body, "address")
    }),
    apiversion.ConvertResponse("UserResource", func(r *apiversion.ResponseInfo) {
        addrs := r.Body["addresses"].([]any)
        r.Body["address"] = addrs[0]
        delete(r.Body, "addresses")
    }),
)
```

### 4.1 Identification & Metadata

| Option | Purpose |
|---|---|
| `ID(string)` | Stable identifier (used in changelog anchors, telemetry). Required. |
| `Description(string)` | Human-readable changelog text. Required unless `Hidden()`. |
| `Hidden()` | Suppress from changelog (purely-internal renames). |
| `SideEffect()` | Marks a behavior gate change (no schema/payload mutation expected; enables `IsApplied`). |

### 4.2 Schema Instructions

`AlterSchema(modelName, ...SchemaInstruction)` — applies one or more instructions to the named OpenAPI schema. The model name is matched against `huma.OpenAPI().Components.Schemas`. Instructions are pure description; the library handles application.

| Instruction (builder) | Equivalent Cadwyn primitive | Effect on older spec |
|---|---|---|
| `Field(name).HadType(t)` | `field(n).had(type=t)` | Replaces the field's `type`/`$ref`. |
| `Field(name).HadName(old)` | `field(n).had(name=old)` | Renames the field in the older spec; converters move data. |
| `Field(name).HadDescription(s)` | `field(n).had(description=s)` | Older spec carries `s`. |
| `Field(name).HadDefault(v)` | `field(n).had(default=v)` | Older spec carries the default. |
| `Field(name).HadConstraint(...)` | `field(n).had(min_length=...)` etc | Constraint deltas. |
| `Field(name).DidntExist()` | `field(n).didnt_exist` | Field removed in older spec. |
| `Field(name).ExistedAs(type, ...)` | `field(n).existed_as(...)` | Field added in older spec (deleted at HEAD). |
| `Field(name).DidntHave(attr)` | `field(n).didnt_have(...)` | Strip a named attribute (e.g. `description`) in the older spec. |
| `Renamed(old)` | `schema(M).had(name=...)` | Whole-schema rename in older spec. |

Enums are first-class:

```go
apiversion.AlterEnum("UserStatus",
    apiversion.EnumHadMembers("invited", "deactivated"),  // old spec had these
    apiversion.EnumDidntHaveMembers("archived"),           // old spec lacked this
)
```

### 4.3 Endpoint Instructions

```go
apiversion.AlterEndpoint("/v1/users", apiversion.MethodGET,
    apiversion.EndpointHadDeprecated(true),
    apiversion.EndpointHadDescription("Returns up to 100 users."),
)
apiversion.AlterEndpoint("/v1/users/{id}/avatar", apiversion.MethodDELETE,
    apiversion.EndpointDidntExist(),  // HEAD has it; older versions don't
)
apiversion.AlterEndpoint("/v1/legacy-stat", apiversion.MethodGET,
    apiversion.EndpointExisted(/* full route description */),  // older versions have it; HEAD doesn't
)
```

Constraints (matching Cadwyn): the handler function, `responseModel`, and path-parameter names cannot change between versions. Only "decorative" attrs (description, deprecated, tags, status code, response examples). If the underlying behavior must change, that's a request/response converter problem, not an endpoint problem.

For routes that exist in old versions but not at HEAD, services must implement them as HEAD handlers and tag them with `apiversion.OnlyInOlderVersions(api, "...")` — this hides them from the HEAD spec but keeps them routable for older requests.

### 4.4 Converters

```go
apiversion.ConvertRequest(schemaName,  func(*RequestInfo))
apiversion.ConvertRequestPath(path, methods, func(*RequestInfo))   // path-keyed variant
apiversion.ConvertResponse(schemaName, func(*ResponseInfo))
apiversion.ConvertResponsePath(path, methods, func(*ResponseInfo), opts ...ConverterOpt)
```

`RequestInfo` and `ResponseInfo` mirror Cadwyn's:

```go
type RequestInfo struct {
    Body        map[string]any   // mutable; nil for empty bodies
    Headers     http.Header      // mutable
    Cookies     map[string]string
    QueryParams url.Values
    Form        *multipart.Form  // nil unless form/multipart
}

type ResponseInfo struct {
    Body       any              // map[string]any | []any | scalar
    StatusCode int
    Headers    http.Header
}
```

Opt-in `MigrateHTTPErrors()` lets a response converter touch `>= 400` payloads (for problem-detail envelope evolution).

## 5. Request Lifecycle

A request hits the Huma server. The middleware installed by `Apply` runs before Huma's router selects a handler:

1. **Version detection.** A `VersionPicker` reads from the configured location (header by default, e.g. `Verself-Api-Version: 2026-04-25`; alternatives: path segment, custom callable). If absent → fall back to `WithDefaultVersion`. If unparseable → 400 with `urn:verself:problem:apiversion:invalid`.

2. **Version resolution.** Date-format versions waterfall via binary search: a request for `2026-03-12` matches the latest version `<= 2026-03-12`. The matched version is stamped onto `context.Context` (retrievable via `PickedVersion`) and onto an outbound `Verself-Api-Version` response header.

3. **Inbound JSON Schema validation.** If the route accepts a body and the requested version's JSON Schema is known, the raw bytes are validated against that older schema before any mutation. Failures return Huma's standard `application/problem+json` 400. (Library: `santhosh-tekuri/jsonschema/v6`.)

4. **Forward migration.** Body is unmarshalled to `map[string]any`. The middleware walks `versions[picked+1:]` toward HEAD; for each version, every matching `ConvertRequest`/`ConvertRequestPath` mutates `RequestInfo`. Order within a version is the order converters were registered on the `Change`; order across versions is bundle order.

5. **HEAD validation.** Migrated body is re-marshalled to JSON bytes, swapped onto the `*http.Request`, and Huma's normal pipeline takes over (validation against the HEAD schema, deserialization into the typed input struct, dependency injection, handler invocation). Migration bugs surface here as `400` against the HEAD schema with a header `X-Apiversion-Migration-Failed: true` so they're visibly distinct from genuine client errors in ClickHouse.

6. **Handler runs.** Returns a HEAD-typed response struct.

7. **Backward migration.** Huma serializes the response to bytes; the middleware intercepts, deserializes to `any`, walks `versions[:picked+1]` from HEAD back toward the client, applies matching `ConvertResponse`/`ConvertResponsePath`, re-serializes, writes.

8. **Telemetry.** OpenTelemetry span attrs `apiversion.requested`, `apiversion.matched`, `apiversion.changes_applied=N` are set; these land in ClickHouse traces by default through our existing OTel pipeline.

## 6. Per-Version OpenAPI Generation

The HEAD spec is what Huma produces today via `api.OpenAPI().YAML()`. To produce an older version's spec, we walk the change chain backward and apply each instruction as a structured mutation on a deep copy of the HEAD spec.

```go
// Pseudocode for the spec mutator.
func applyInstruction(spec *huma.OpenAPI, ins SchemaInstruction) error {
    switch ins := ins.(type) {
    case *fieldDidntExist:
        delete(spec.Components.Schemas[ins.Schema].Properties, ins.Field)
    case *fieldExistedAs:
        spec.Components.Schemas[ins.Schema].Properties[ins.Field] = ins.toSchema()
    case *fieldHadName:
        renameField(spec, ins.Schema, ins.Field /*head name*/, ins.OldName)
    // ...
    }
}
```

Mutators operate on Huma's `*huma.OpenAPI` value (which round-trips JSON faithfully), then `.YAML()` / `.DowngradeYAML()` emit the per-version artifact. Each service's `cmd/<svc>-openapi/main.go` gains a `--version=<date>` flag; with no flag it emits HEAD as today.

Per-version specs are also served live at `/openapi.json?version=<date>` and `/openapi-3.0.yaml?version=<date>` on each Huma API. A `/openapi-versions` index lists available versions.

## 7. Validation Strategy

Cadwyn double-validates: once against the old Pydantic model on entry, once against HEAD post-migration. We do the same but with cheaper primitives:

- **Inbound (old version):** JSON Schema validation of raw bytes against the requested-version spec's request schema. Fast, dependency-light, no struct generation. Skipped only if the route has no body schema.
- **Post-migration (HEAD):** Huma's existing typed deserialization + struct-tag validation. Free; we already pay for it.
- **Outbound (HEAD):** Huma's existing response validation in dev/test; production trusts the handler.
- **Outbound (old version):** Optional. Off by default. Enabling it via `WithStrictResponseValidation()` validates the post-backward-migration body against the old version's response schema and logs a structured error if invalid (does not break the request — old clients are the victims either way, and we'd rather observe than 500).

## 8. Version Detection

```go
apiversion.Apply(api, bundle,
    apiversion.WithHeader("Verself-Api-Version"),
    apiversion.WithDefaultVersion(func(ctx context.Context, r *http.Request) string {
        // Per-account pinning. Read auth, look up Org, return its pinned version.
        if org, ok := orgFromAuth(ctx); ok {
            return org.PinnedAPIVersion
        }
        return bundle.Latest()  // anonymous → latest
    }),
)
```

Alternative pickers ship in the box:

```go
apiversion.WithPathPicker("/v/{version}/")       // /v/2026-04-25/users/...
apiversion.WithQueryPicker("api_version")
apiversion.WithCustomPicker(func(*http.Request) (string, error) { ... })
```

Per-account pinning happens via `WithDefaultVersion` returning a value computed from the request's auth identity — Stripe's model. The header always wins if present, allowing CLI/SDK overrides.

## 9. Outbound / Webhook Migration

Webhooks, audit emitters, and async workers cannot be intercepted by HTTP middleware. They use the standalone helper:

```go
payloadBytes, err := apiversion.MigrateOutbound(
    bundle,
    "InvoiceFinalized",                  // schema name in HEAD
    invoiceEvent,                         // any HEAD-typed value (struct, map, etc.)
    subscriber.PinnedAPIVersion,          // target version
)
// payloadBytes is JSON shaped like the subscriber expects.
```

Internally: marshal HEAD body → walk backward chain applying `ConvertResponse` keyed on schema name → re-marshal. Webhook subscribers have a `pinned_api_version` column on their config row (mailbox-service, governance-service, etc. follow this convention); the sender passes that value in.

## 10. Behavioral Gates (`SideEffect` Changes)

Cadwyn's `VersionChangeWithSideEffects` lets business logic gate behavior on version. We expose the same, deliberately friction-bearing:

```go
var ExternalAddressCheck = apiversion.NewChange(
    apiversion.ID("external-address-check"),
    apiversion.Description("Validate addresses against external service."),
    apiversion.SideEffect(),
)

// in handler:
if apiversion.IsApplied(ctx, ExternalAddressCheck) {
    if err := externalAddressService.Validate(addr); err != nil { ... }
}
```

`IsApplied` reads `PickedVersion(ctx)` and returns true iff the picked version is `>=` the version that owns the change. Linter (`apiversion-check`) flags every `IsApplied` call site so they stay grep-able.

## 11. Changelog Generation

Every `Change` carries `ID`, `Description`, and a typed instruction list. `RenderChangelog(bundle)` produces:

```go
type Changelog struct {
    Versions []ChangelogVersion
}
type ChangelogVersion struct {
    Date    string
    Changes []ChangelogChange
}
type ChangelogChange struct {
    ID           string
    Description  string
    Instructions []ChangelogInstruction  // typed: schema added, field renamed, endpoint deleted, etc.
    Hidden       bool
}
```

`apiversion-changelog` CLI emits markdown for `src/frontends/viteplus-monorepo/apps/verself-web/src/routes/_workshop/policy/changelog.tsx`. `Hidden()` changes are excluded from rendered output but retained in the structured form.

## 12. Tooling

| Binary | Purpose | Inputs |
|---|---|---|
| `apiversion-render` | Emit per-version OpenAPI to disk | service binary's bundle, version date, output path |
| `apiversion-check` | Lint changes for correctness | bundle |
| `apiversion-changelog` | Emit markdown changelog | bundle |

`apiversion-check` enforces:

- Every `AlterSchema` instruction targets a schema that exists at HEAD (or was added by `ExistedAs`).
- Every schema with a `Field(...).HadName(...)` rename has both a request and response converter (or proves it's request-only / response-only via explicit annotation).
- Every `EndpointDidntExist` is matched by a HEAD handler tagged `OnlyInOlderVersions`.
- No two `Change`s in the same version mutate the same `(schema, field)` tuple.
- Path parameters are stable across versions for any route in the change chain.
- IDs are unique across the bundle.

Failures are tagged structured errors so CI parses cleanly.

## 13. Integration With Existing Stack

A service adopts the library with a four-line diff:

```go
// src/services/billing-service/internal/billingapi/api.go
func NewAPI(mux *http.ServeMux, cfg Config) huma.API {
    config := huma.DefaultConfig("Billing Service", cfg.Version)
    api := humago.New(mux, config)
    if err := apiversion.Apply(api, billing.Bundle); err != nil { /* fatal */ }
    RegisterRoutes(api, cfg)
    dto.ApplyOpenAPIWireDefaults(api)
    return api
}
```

The service owns its own `Bundle` (e.g. `src/services/billing-service/internal/billing/versions.go`). Cross-service shared changes — when a DTO in `domain-transfer-objects` evolves and multiple services consume it — live in `src/domain-transfer-objects/go/versions/<dto-area>.go` and are imported into each service's bundle. The generated contract gate gains awareness of the per-version specs to keep the wire-contract checks honest.

## 14. Generated Client Implications

`oapi-codegen` and `@hey-api/openapi-ts` already run against committed OpenAPI specs. Per-version specs check in alongside:

```
src/services/billing-service/openapi/
  openapi-3.0.yaml                          # HEAD (existing)
  openapi-3.1.yaml                          # HEAD (existing)
  versions/2026-04-25/openapi-3.0.yaml      # NEW
  versions/2026-01-01/openapi-3.0.yaml      # NEW
```

Generated client packages namespace by version: `client/v20260425/billing`, `client/v20260101/billing`. SDK consumers pin a version explicitly. The "latest" client is an alias to whichever version ships in the latest stable SDK release.

CI emits all version specs via `apiversion-render`; each service package declares the Bazel target that regenerates them from its binary. The matching diff test fails CI when the rendered spec differs from the committed one.

## 15. Telemetry

Each request emits to ClickHouse (via existing OTel → ClickHouse pipeline) a span with attrs:

| Attr | Value |
|---|---|
| `apiversion.requested` | Raw value the client sent |
| `apiversion.matched` | Version after waterfalling |
| `apiversion.changes_applied` | Count of forward + backward conversions |
| `apiversion.fallback_used` | Whether `WithDefaultVersion` fired |
| `apiversion.client_kind` | From `User-Agent` (cli, sdk-ts, sdk-go, ios, web) |

A standing query `apiversion_usage_by_org_and_version` powers a console panel showing each org's lagging clients. The deprecation runbook reads this query to drive sunset emails.

## 16. Open Design Questions

1. **Library naming.** `apiversion` vs `apirev` vs `wireversion`. `apiversion` is most discoverable; `apirev` is shorter and matches Stripe's "revisions" usage.
2. **Path-versioning support at v0.** Cadwyn ships it; we likely don't need it (header is cleaner) and could leave it out until a customer asks.
3. **Streaming responses.** Cadwyn explicitly doesn't migrate them (open issues). We have `governance-service` audit streams and `mailbox-service` JMAP push channels — punt to v0.next, document the gap in the spec, route streaming endpoints through `OnlyInOlderVersions` if they ever change shape.
4. **Form/multipart bodies.** Cadwyn added in v6.1.0. We have at most one use case (avatar uploads). Defer to v0.next.
5. **`DowngradeYAML` interaction.** Huma already downgrades 3.1 → 3.0; we apply mutations on 3.1 then downgrade for the public artifact. Verify this composes cleanly.
6. **Per-version Go server-side struct generation.** This spec says no. Reconsider only if a concrete payload emerges that can't be expressed as a `map[string]any` mutation.

## 17. Out of Scope (v0)

- Streaming response migration
- Multipart/form body migration
- WebSocket payload migration
- gRPC / protobuf evolution (use protobuf's native field-number rules)
- Database row migration (Cadwyn's docs warn against this; we agree)
- Cross-tenant version pinning sharding

## 18. Acceptance Criteria

A v0 ship is achieved when:

1. The library compiles, passes `apiversion-check`, and exposes the surface in §3.
2. `billing-service` adopts it with one historical `Change` (e.g. a renamed field) and a live rehearsal proves a v1 client gets the old shape and a HEAD client gets the new shape.
3. Per-version OpenAPI specs are checked into `src/services/billing-service/openapi/versions/` and a regenerated TS client compiles against the latest version.
4. ClickHouse trace spans surface `apiversion.matched` for every billing request.
5. The console renders the changelog from the bundle.
6. The billing-service Bazel package regenerates the version specs from the binary deterministically.
