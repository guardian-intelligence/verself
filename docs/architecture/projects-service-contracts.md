# Projects Service Contracts

Projects uses public and internal projections from one canonical Smithy model.

```text
src/contracts/models/verself/projects.smithy
  -> public projection
     -> SDK-layer generated transports
     -> generated OpenAPI compatibility artifacts
     -> SDK conformance fixtures
  -> internal projection
     -> src/services/projects-service/client
     -> generated OpenAPI compatibility artifacts
```

The public projection is the customer contract. It uses bearer authentication,
omits service-origin headers, and feeds public docs plus SDK-layer code.

The internal projection is the repo-owned service-to-service contract. It uses
SPIFFE mTLS, includes public-shaped operations for service callers, and includes
service-only operations such as project resolution and domain-event listing.
Generated service clients are transport packages; callers own auth by providing
an mTLS `http.Client`.

`src/services/projects-service/client` is generated from the internal
projection and is visible only to service packages. SDKs generate their own code
from the public projection under `src/sdks/` or frontend SDK packages.

During cutover, the existing Huma route catalog and committed OpenAPI files may
mirror this model. They are compatibility artifacts and implementation inputs,
not the settled contract authority.

## Naming

- `public` means customer, CLI, browser server route, documentation, and SDK
  contract.
- `internal` means repo-owned service-to-service contract over SPIFFE mTLS.
- `client` under a service directory means generated service transport client.
- SDK generated code lives under the SDK package that consumes it.

## Invariants

```shell
rg -n "github.com/verself/projects-service/client" src/sdks src/verself-cli src/websites/packages/sdk src/websites/apps/verself-web
rg -n "projects.smithy" src/contracts src/services/projects-service src/sdks
rg -n "projects-internal-openapi" src/services/projects-service src/host .aspect
rg -n "unknown module path version unknown version" src/services/projects-service src/sdks/go/verself
```

The first command must return no matches. The second command should return the
canonical contract model and generated projection paths once the cutover is
wired. The remaining commands cover transitional OpenAPI emitters and generated
SDK paths that still define the current projection boundary. The Go `internal/`
package path remains normal Go visibility and is unrelated to the contract
projection model.
