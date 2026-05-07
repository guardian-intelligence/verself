# Projects Service Contracts

Projects uses two OpenAPI projections from one Huma operation catalog.

```text
src/services/projects-service/internal/api/operations.go
  -> public projection
     -> openapi/openapi-3.0.yaml
     -> openapi/openapi-3.1.yaml
     -> SDK-layer generated code
  -> internal projection
     -> openapi/internal-openapi-3.0.yaml
     -> openapi/internal-openapi-3.1.yaml
     -> src/services/projects-service/client
```

The public projection is the customer contract. It uses bearer authentication,
omits service-origin headers, and feeds public docs plus SDK-layer code.

The internal projection is the repo-owned service-to-service contract. It uses
SPIFFE mTLS, includes public-shaped operations for service callers, and includes
service-only operations such as project resolution and domain-event listing.
Generated service clients are transport packages; callers own auth by providing
an mTLS `http.Client`.

`src/services/projects-service/client` is generated from the internal projection
and is visible only to service packages. SDKs generate their own code from the
public projection under `src/sdks/` or frontend SDK packages.

## Naming

- `public` means customer, CLI, browser server route, documentation, and SDK
  contract.
- `internal` means repo-owned service-to-service contract over SPIFFE mTLS.
- `client` under a service directory means generated service transport client.
- SDK generated code lives under the SDK package that consumes it.

## Invariants

```shell
rg -n "github.com/verself/projects-service/client" src/sdks src/cli src/frontends/viteplus-monorepo/packages/sdk src/frontends/viteplus-monorepo/apps/verself-web
rg -n "spec = \"//src/services/projects-service/openapi:internal-openapi-3.0.yaml\"" src/services/projects-service/client/BUILD.bazel
rg -n "projects-internal-openapi" src/services/projects-service src/host .aspect
rg -n "unknown module path version unknown version" src/services/projects-service src/sdks/go/verself
```

The first command must return no matches. The remaining commands must return
the service transport client, OpenAPI emitter, and generated SDK paths that
define the projection boundary. The Go `internal/` package path remains normal
Go visibility and is unrelated to the contract projection model.
