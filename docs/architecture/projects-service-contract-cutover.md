# Projects Service Contract Cutover

Projects is the first service to use the converged contract pipeline:

```text
projects-service operation catalog
  -> internal/canonical OpenAPI projection
     -> service-owned Go transport client
        -> repo-owned service-to-service callers
  -> public OpenAPI projection
     -> SDK-layer Go and TypeScript generated code
        -> curated SDK facade
           -> verself CLI, TanStack server routes, customer automation
```

The service-owned generated Go client is a transport package. Its consumers are
other services. SDK packages generate their own SDK-layer code from the public
OpenAPI projection and do not import service generated clients.

`internalclient` is removed by moving its contract coverage into the existing
service transport package:

```text
before:
  src/services/projects-service/client
    generated from public OpenAPI
    used by public/SDK code

  src/services/projects-service/internalclient
    generated from internal OpenAPI
    used by service-to-service code

after:
  src/services/projects-service/client
    generated from internal/canonical OpenAPI
    used by service-to-service code

  src/sdks/go/verself/internal/generated/projects
    generated from public OpenAPI
    used only by the Go SDK facade

  src/frontends/viteplus-monorepo/packages/sdk/src/__generated/projects-api
    generated from public OpenAPI
    used only by the TypeScript SDK facade
```

The package name `client` becomes the low-level service transport client. It
contains public-shaped operations and service-only operations. Auth remains
outside the generated package: service callers pass an mTLS `http.Client` and
request editors for service-projection headers; SDK callers use SDK-owned public
generated code with bearer-token request editors.

## Acceptance Criteria

- `src/services/projects-service/internalclient/` is deleted.
- `src/services/projects-service/client` is generated from the internal/canonical
  Projects OpenAPI spec and is visible only to `src/services/...` Bazel targets.
- `source-code-hosting-service` resolves Projects through
  `github.com/verself/projects-service/client`.
- `src/sdks/go/verself` has no dependency on `github.com/verself/projects-service`,
  `github.com/verself/service-runtime`, `github.com/spiffe/go-spiffe`, or
  `projects-service/client`.
- The TypeScript SDK remains generated and wrapped inside
  `src/frontends/viteplus-monorepo/packages/sdk`.
- SDK code has bearer/customer auth only. Service-to-service SPIFFE/mTLS stays in
  service callers through caller-owned transports.
- `rg -n "projects-service/internalclient|projectsinternalclient" src/services src/sdks src/cli src/frontends/viteplus-monorepo` returns no matches.
- `rg -n "github.com/verself/projects-service/(client|internalclient)" src/sdks src/cli src/frontends/viteplus-monorepo/packages/sdk src/frontends/viteplus-monorepo/apps/verself-web` returns no matches.

## File-by-File Plan

### Contract Generation

`src/services/projects-service/internal/api/operations.go`

- Keep the operation catalog as the source for public and service projections.
- Rename the current surface concepts in code comments and type names so the
  distinction is `publicProjection` and `serviceProjection`.
- Preserve public operations in the service projection so service callers can
  call public-shaped endpoints over SPIFFE/mTLS.
- Preserve internal-only operations such as `ResolveProject` and
  `ResolveProjectEnvironment` in the service projection only.

`src/services/projects-service/internal/api/openapi.go`

- Treat `NewAPI`, `OpenAPIYAML`, and `OpenAPIDowngradeYAML` as the public
  projection.
- Ensure public operations emit only `bearerAuth`.
- Ensure public operations omit `X-Verself-Origin-*` parameters and
  `x-verself-origin`.
- Keep this projection as the input to SDK-layer generation and public docs.

`src/services/projects-service/internal/api/internal_openapi.go`

- Treat `NewInternalAPI`, `InternalOpenAPIYAML`, and
  `InternalOpenAPIDowngradeYAML` as the internal/canonical service projection.
- Ensure public-shaped operations emitted here use `mutualTLS` and include
  `X-Verself-Origin-Org-ID`, `X-Verself-Origin-Subject`, and optional
  `X-Verself-Origin-Email`.
- Ensure internal-only operations use `mutualTLS` and their own peer allowlists.
- Keep the output file names for now because other repo OpenAPI tooling already
  understands `internal-openapi-*`.

`src/services/projects-service/internal/api/policy.go`

- Keep enforcement split by runtime projection:
  - public projection reads the Zitadel identity from request context;
  - service projection reads SPIFFE peer identity and, for org-scoped
    public-shaped operations, reads origin headers.
- Keep idempotency enforcement in the shared policy path.
- Keep origin headers as service-projection contract details; public projection
  should never expose them.

`src/services/projects-service/internal/api/contract_test.go`

- Update tests to assert the projection relationship:
  - every public operation appears in the service projection;
  - service-only operations do not appear in the public projection;
  - public projection has `bearerAuth` only;
  - service projection has `mutualTLS` only;
  - service-projected public operations carry origin headers and
    `x-verself-origin`;
  - public projection has no origin headers.
- Add a regression assertion that the operation catalog has unique operation IDs.

`src/services/projects-service/openapi/BUILD.bazel`

- Keep `verself_openapi_specs` with the current public and internal tools.
- Add comments naming `internal-openapi-*` as the service/canonical projection for
  Projects until the shared macro grows first-class canonical naming.

`src/services/projects-service/cmd/projects-openapi/main.go`

- Keep as the public projection emitter.
- Keep `--check` behavior pointed at `openapi/openapi-<format>.yaml` once
  committed spec files are introduced for Projects.

`src/services/projects-service/cmd/projects-internal-openapi/main.go`

- Keep as the service/canonical projection emitter.
- Keep `--check` behavior pointed at `openapi/internal-openapi-<format>.yaml`
  once committed spec files are introduced for Projects.

### Service Transport Client

`src/services/projects-service/client/BUILD.bazel`

- Change `spec` from `//src/services/projects-service/openapi:openapi-3.0.yaml`
  to `//src/services/projects-service/openapi:internal-openapi-3.0.yaml`.
- Set `response_type_suffix = "HTTPResponse"` if the internal/canonical spec
  needs it for collision-free `oapi-codegen` output.
- Set `visibility = ["//src/services:__subpackages__"]` so SDK, CLI, and
  frontend Bazel targets cannot import the service transport client.

`src/services/projects-service/client/client.gen.go`

- Regenerate from the internal/canonical spec.
- Confirm it includes public-shaped operations such as `ListProjects`,
  `CreateProject`, and `GetProject`.
- Confirm it includes service-only operations such as `ResolveProject` and
  `ResolveProjectEnvironment`.
- Do not hand-edit this file.

`src/services/projects-service/internalclient/BUILD.bazel`

- Delete.

`src/services/projects-service/internalclient/client.gen.go`

- Delete.

### Service Callers

`src/services/source-code-hosting-service/internal/source/projects_client.go`

- Replace the import of
  `github.com/verself/projects-service/internalclient` with
  `github.com/verself/projects-service/client`.
- Keep the caller-owned SPIFFE/mTLS `http.Client` injection through
  `NewProjectsClient`.
- Keep request construction against generated transport types.
- Adjust response type names if `response_type_suffix` changes generated names.

`src/services/source-code-hosting-service/internal/source/BUILD.bazel`

- Replace `//src/services/projects-service/internalclient` with
  `//src/services/projects-service/client`.

`src/services/source-code-hosting-service/go.mod`

- No semantic dependency change should be needed because the module dependency is
  already `github.com/verself/projects-service`.
- Run `aspect bazel tidy` after the import cutover.

### Go SDK Layer

`src/sdks/AGENTS.md`

- Keep the boundary text: SDKs do not implement SPIFFE/mTLS; service callers use
  service generated clients directly.

`src/sdks/go/verself/BUILD.bazel`

- Remove dependencies on `//src/services/projects-service/client` and
  `//src/services/projects-service/internalclient`.
- Add the Projects SDK generated-core target once the generator exists.
- Keep only SDK-owned generated files and ordinary SDK dependencies visible here.

`src/sdks/go/verself/go.mod`

- Remove `github.com/verself/projects-service`.
- Remove `github.com/verself/service-runtime` and
  `github.com/spiffe/go-spiffe/v2`.
- Keep dependencies required by SDK-owned generated code after `aspect bazel
  tidy`.

`src/sdks/go/verself/go.sum`

- Regenerate through `aspect bazel tidy`.

`src/sdks/go/verself/client.go`

- Remove `WorkloadIdentityOptions`, `Origin`, `ProjectsInternalURL`, and
  SPIFFE/mTLS construction.
- Keep bearer-token auth and caller-supplied `HTTPClient`.
- Construct Projects from SDK-owned generated code, not from
  `projects-service/client`.
- Keep tracing header injection for public SDK requests.

`src/sdks/go/verself/projects.go`

- Remove imports of `github.com/verself/projects-service/client` and
  `github.com/verself/projects-service/internalclient`.
- Remove the `internal` client field and all internal branch methods.
- Keep the curated shape:
  - `Projects.List`;
  - `Projects.Create`;
  - `Projects.Get`;
  - public DTOs and typed options.
- Delegate repetitive request/response binding to generated SDK core code.

`src/sdks/go/verself/projects_test.go`

- Delete the SPIFFE/mTLS internal-client test.
- Add tests that use `httptest.Server` to verify:
  - bearer `Authorization` is sent;
  - `Idempotency-Key` is sent for `Create`;
  - query parameters match `ListProjectsOptions`;
  - Problem Details responses normalize into `APIError`;
  - no service generated client package is imported.

`src/sdks/go/verself/internal/generated/projects/`

- Add SDK-owned generated Projects code from the public OpenAPI projection.
- Keep this package private to the Go SDK module.
- Generate DTOs and operation bindings needed by the curated SDK.
- Do not import `github.com/verself/projects-service/client`.

`src/sdks/go/verself/cmd/verself-go-sdkgen/`

- Add a small generator binary if `oapi-codegen` output alone cannot produce the
  desired SDK resource core.
- Read the public Projects OpenAPI spec.
- Emit deterministic Go code under
  `src/sdks/go/verself/internal/generated/projects/`.
- Use OpenAPI operation IDs and `x-verself-*` extensions for idempotency,
  pagination, and error-normalization metadata.

### TypeScript SDK Layer

`src/frontends/viteplus-monorepo/packages/sdk/BUILD.bazel`

- Keep `viteplus_openapi_clients` pointed at
  `//src/services/projects-service/openapi:openapi-3.1.yaml.bin`.
- Add a generated SDK-core target if the wrapper logic moves out of
  `src/projects.ts`.
- Ensure generated TS code stays inside this SDK package, not in application
  directories.

`src/frontends/viteplus-monorepo/packages/sdk/src/projects.ts`

- Keep the curated `Projects` facade and public types.
- Move repetitive operation binding into generated SDK-core code:
  - input validation;
  - query/path/body mapping;
  - idempotency header policy;
  - generated result parsing;
  - Problem Details normalization.
- Keep bearer/customer auth as the only SDK auth mode.

`src/frontends/viteplus-monorepo/packages/sdk/src/service-api.ts`

- Keep shared bearer headers, trace headers, idempotency helpers, and service
  error normalization.
- Do not add SPIFFE/mTLS or service-to-service auth here.

`src/frontends/viteplus-monorepo/packages/sdk/src/__generated/projects-api/`

- Regenerate from the public Projects OpenAPI projection.
- Keep committed/generated TS output aligned with the existing
  `write_source_files` workflow.
- Do not hand-edit generated output.

`src/frontends/viteplus-monorepo/packages/sdk/src/index.ts`

- Keep exporting the curated SDK facade.
- Avoid exporting raw generated clients as the primary public API.

### Facades

`src/cli/verself/internal/app/projects.go`

- Keep using `github.com/verself/verself-go`.
- No direct service generated client imports are allowed.
- Keep token-file and environment token handling at the CLI edge.

`src/cli/verself/internal/app/BUILD.bazel`

- Keep the dependency on `//src/sdks/go/verself`.
- Do not add service generated client dependencies.

`src/cli/verself/go.mod`

- Run `aspect bazel tidy` after the SDK dependency cleanup.
- Confirm `github.com/verself/projects-service`,
  `github.com/verself/service-runtime`, and `github.com/spiffe/go-spiffe/v2`
  disappear unless another CLI dependency legitimately needs them.

`src/cli/verself/go.sum`

- Regenerate through `aspect bazel tidy`.

`src/frontends/viteplus-monorepo/apps/verself-web/src/server-fns/api.ts`

- Keep using `@verself/sdk`.
- Do not import app-local Projects API wrappers or generated service clients.
- Keep token extraction and session concerns at the server-function edge.

`src/frontends/viteplus-monorepo/apps/verself-web/BUILD.bazel`

- Keep depending on `//src/frontends/viteplus-monorepo/packages/sdk`.
- Do not add Projects service generated-client dependencies.

### Deleted or Replaced App-Local Projects Code

`src/frontends/viteplus-monorepo/apps/verself-web/src/lib/projects-api.ts`

- Keep deleted.
- The Projects facade for web server functions is `@verself/sdk`.

### Build and Workspace Files

`bazel.go.work`

- Keep the Go SDK module if the CLI depends on it.
- No Projects-specific change expected beyond tidy output.

`BUILD.bazel`

- Keep any module registration needed for the Go SDK.
- No Projects-specific change expected beyond generator target wiring if a
  repo-root aggregate target requires it.

`MODULE.bazel`

- Keep existing OpenAPI generator dependencies.
- Add only generator dependencies required by the SDK-core generator.

`src/frontends/viteplus-monorepo/viteplus_rules.bzl`

- Keep `viteplus_openapi_clients` for generated TS transport output.
- Extend or add a sibling macro only if generated SDK-core output needs a
  separate deterministic post-process step.

`bazel/openapi_codegen.bzl`

- Keep `verself_oapi_go_client` for service transport clients.
- Add a separate macro for SDK-layer Go generation if needed. Name it so service
  transport generation and SDK generation cannot be confused.

## Cutover Order

1. Add failing guardrails:
   - confirm `rg -n "projects-service/internalclient|projectsinternalclient"` is
     non-empty before the cutover;
   - add or update contract tests for public/service OpenAPI projection
     differences;
   - set service client Bazel visibility to services-only and observe SDK build
     failures before the SDK is cleaned up.
2. Switch `src/services/projects-service/client` to the internal/canonical spec
   and regenerate.
3. Update `source-code-hosting-service` to use the service client package.
4. Delete `src/services/projects-service/internalclient`.
5. Remove all service-client imports from the Go SDK and replace them with
   SDK-owned generated Projects core code from the public spec.
6. Regenerate the TypeScript SDK from the public spec and shrink handwritten
   Projects code to facade behavior.
7. Run tidy, focused builds, and focused tests.
8. Deploy the committed SHA and collect ClickHouse deploy evidence.

## Verification

Pre-cutover failure checks:

```shell
rg -n "projects-service/internalclient|projectsinternalclient" src/services src/sdks src/cli src/frontends/viteplus-monorepo
rg -n "github.com/verself/projects-service/(client|internalclient)" src/sdks src/cli src/frontends/viteplus-monorepo/packages/sdk src/frontends/viteplus-monorepo/apps/verself-web
```

Expected post-cutover grep results:

```shell
rg -n "projects-service/internalclient|projectsinternalclient" src/services src/sdks src/cli src/frontends/viteplus-monorepo
# no matches

rg -n "github.com/verself/projects-service/(client|internalclient)" src/sdks src/cli src/frontends/viteplus-monorepo/packages/sdk src/frontends/viteplus-monorepo/apps/verself-web
# no matches
```

Focused validation:

```shell
aspect bazel tidy
bazelisk test //src/services/projects-service/internal/api:api_test
bazelisk build //src/services/source-code-hosting-service/internal/source:source
bazelisk test //src/sdks/go/verself:verself_test
bazelisk test //src/cli/verself/internal/app:app_test
(cd src/frontends/viteplus-monorepo && vp exec tsc -p packages/sdk/tsconfig.json)
(cd src/frontends/viteplus-monorepo && vp exec tsc -p apps/verself-web/tsconfig.json)
bazelisk build //src/services/projects-service/client:client
bazelisk build //src/services/source-code-hosting-service/cmd/source-code-hosting-service:source-code-hosting-service
bazelisk build //src/cli/verself/cmd/verself:verself
bazelisk build //src/frontends/viteplus-monorepo/apps/verself-web:node_app_nomad_artifact
```

Live evidence:

```shell
aspect deploy --site=prod --sha=HEAD
aspect observe --what=deploy --site=prod --format=markdown --limit=10
aspect observe --what=trace --trace-id=<trace-id> --format=markdown --limit=20
```
