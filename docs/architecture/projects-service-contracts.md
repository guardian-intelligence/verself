# Projects Service Contracts

Projects uses public and internal projections from one canonical Smithy model.

```text
src/smithy/models/verself/projects.smithy
  -> official public OpenAPI 3.1 projection
     -> SDK-layer TypeScript transport
     -> public docs and ecosystem tooling
  -> hand-written Huma routes and service-owned Go clients
     -> conformance against the Smithy/OpenAPI HTTP surface
```

The public Smithy service is the customer contract. It uses bearer
authentication, omits service-origin headers, and feeds public docs plus
SDK-layer code through the official OpenAPI projection.

The internal Smithy service is the repo-owned service-to-service contract. It
uses SPIFFE mTLS, includes public-shaped operations for service callers, and
includes service-only operations such as project resolution and domain-event
listing. Service-owned Go clients are transport packages; callers own auth by
providing an mTLS `http.Client`.

`src/services/projects-service/client` is visible only to service packages.
SDKs generate their own transport code from the public OpenAPI projection under
`src/sdks/` or frontend SDK packages.

During cutover, the existing Huma route catalog and committed OpenAPI files may
mirror this model. They are compatibility artifacts and implementation inputs,
not the settled contract authority.

## Naming

- `public` means customer, CLI, browser server route, documentation, and SDK
  contract.
- `internal` means repo-owned service-to-service contract over SPIFFE mTLS.
- `client` under a service directory means service-owned transport client.
- SDK generated code lives under the SDK package that consumes it.

## Invariants

```shell
rg -n "github.com/verself/projects-service/client" src/sdks src/verself-cli src/websites/packages/sdk src/websites/apps/verself-web
rg -n "projects.smithy" src/smithy src/services/projects-service src/sdks
rg -n "projects-internal-openapi" src/services/projects-service src/host .aspect
rg -n "unknown module path version unknown version" src/services/projects-service src/sdks/go/verself
```

The first command must return no matches. The second command should return the
canonical contract model and projection paths. The remaining commands cover
OpenAPI emitters and SDK paths that define the current projection boundary. The
Go `internal/` package path remains normal Go visibility and is unrelated to the
contract projection model.
