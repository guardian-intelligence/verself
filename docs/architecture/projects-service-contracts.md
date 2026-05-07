# Projects Service Contracts

Projects uses two OpenAPI projections from one Huma operation catalog.

```text
src/services/projects-service/internal/api/operations.go
  -> public projection
     -> openapi/openapi-3.0.yaml
     -> openapi/openapi-3.1.yaml
     -> SDK-layer generated code
  -> service projection
     -> openapi/service-openapi-3.0.yaml
     -> openapi/service-openapi-3.1.yaml
     -> src/services/projects-service/client
```

The public projection is the customer contract. It uses bearer authentication,
omits service-origin headers, and feeds public docs plus SDK-layer code.

The service projection is the repo-owned service-to-service contract. It uses
SPIFFE mTLS, includes public-shaped operations for service callers, and includes
service-only operations such as project resolution and domain-event listing.
Generated service clients are transport packages; callers own auth by providing
an mTLS `http.Client`.

`src/services/projects-service/client` is generated from the service projection
and is visible only to service packages. SDKs generate their own code from the
public projection under `src/sdks/` or frontend SDK packages.

## Naming

- `public` means customer, CLI, browser server route, documentation, and SDK
  contract.
- `service` means repo-owned service-to-service contract over SPIFFE mTLS.
- `client` under a service directory means generated service transport client.
- SDK generated code lives under the SDK package that consumes it.

## Invariants

```shell
rg -n "projects-service/internalclient|projectsinternalclient" src/services src/sdks src/cli src/frontends/viteplus-monorepo
rg -n "github.com/verself/projects-service/(client|internalclient)" src/sdks src/cli src/frontends/viteplus-monorepo/packages/sdk src/frontends/viteplus-monorepo/apps/verself-web
rg -n "internal-openapi|InternalOpenAPI|NewInternalAPI|projects-internal-openapi" src/services/projects-service
rg -n "apiSurfaceInternal|internalProjectOperation|publicInternalPeers" src/services/projects-service
rg -n "unknown module path version unknown version" src/services/projects-service src/sdks/go/verself
```

Each command must return no matches. The Go `internal/` package path remains
normal Go visibility and is unrelated to the contract projection model.
