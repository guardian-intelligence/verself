# Wire Contracts

`src/apiwire` owns the DTO language shared by Go services, OpenAPI specs, generated TypeScript clients, and app-facing TypeScript wrappers. Service domain packages can keep native Go types and service-local aliases. Huma boundary structs own JSON encoding and generated schema.

## Numeric Safety

Frontend-facing 64-bit identifiers are decimal strings, never JSON numbers. Go DTO fields use `apiwire.DecimalUint64`, `apiwire.DecimalInt64`, or a semantic alias such as `apiwire.OrgID`. Path and query parameters remain strings at the transport edge and parse through `apiwire.ParseUint64` or `apiwire.ParseInt64`.

Frontend-facing quantities may be JSON numbers only when the committed OpenAPI 3.1 schema proves `maximum <= 9007199254740991`. Unbounded 64-bit quantities use decimal string DTOs or explicit `x-js-wire: bigint` metadata when the TypeScript wrapper deliberately exposes `bigint`.

Internal Go domain structs can keep `uint64` and `int64`. Do not return those structs directly from Huma handlers when a frontend or another service consumes the shape. Convert domain values into `apiwire` DTOs at the API boundary.

## Service Boundary Pattern

Shared DTO structs live in `src/apiwire` once their field language is shared across services or generated clients. Service packages keep small converter functions next to the Huma handlers because those functions understand the local domain model and authorization context.

Use this shape:

```go
type entitlementsOutput struct {
	Body apiwire.BillingEntitlementsView
}

func toEntitlementsDTO(orgID billing.OrgID, view billing.EntitlementsView) apiwire.BillingEntitlementsView {
	return apiwire.BillingEntitlementsView{
		OrgID:     apiwire.Uint64(uint64(orgID)),
		Universal: entitlementPoolList(view.Universal),
		Products:  entitlementProductSections(view.Products),
	}
}
```

Do not use hand-written `strconv.FormatUint` string fields in service-local DTOs for cross-service fields. The decimal type owns JSON encoding and the Huma schema provider.

## Generated Contract Gate

Each service's OpenAPI package declares the frontend wire-contract gate next to the spec it produces. The gate scans frontend-consumed OpenAPI 3.1 specs and fails for `type: integer`, `format: int64` or `format: uint64` unless one of these is true:

- `maximum <= 9007199254740991`
- `x-js-wire: bigint`
- the value is encoded as an `apiwire` decimal string DTO instead of an integer schema

The checker also treats OpenAPI 3.1 nullable integer schemas such as `type: [integer, "null"]` as integer schemas.

## Go Service Client Pattern

Go services consume other Go services through generated `oapi-codegen` clients from committed OpenAPI 3.0 specs. Use the public `client` package for customer-authenticated API shapes and the `internalclient` package for SPIFFE-only operations. The generated client owns URL construction, request/response JSON, and problem parsing; the caller owns only the base URL and the `http.Client`.

For repo-owned service-to-service traffic, construct the `http.Client` with `auth-middleware/workload.MTLSClientForService` and pass it via the generated client's `WithHTTPClient` option. Do not hand-write service calls with `http.NewRequest`, `Do`, `json.Marshal`, or `json.NewDecoder`; if the generated package cannot express the operation, add or correct the Huma route/OpenAPI source and regenerate.

## TypeScript Boundary Pattern

Generated clients stay generated and are the supported service SDK surface. TanStack Start server functions, service-side adapters, and external customers may call generated SDK functions directly with the appropriate bearer token, headers, and base URL.

Browser UI code should not hand-roll service fetches. It should go through server functions or app-facing wrapper modules so bearer forwarding, audience selection, idempotency headers, Valibot parsing, and DTO conversion stay in one server-owned boundary. Those wrappers parse generated responses with Valibot before values reach app logic, then deliberately convert DTO fields:

- identifiers usually remain branded decimal strings
- safe bounded quantities become `number`
- unbounded quantities become `bigint` only when the wrapper intentionally exposes that type

Do not parse `response.json()` directly for service APIs when a generated SDK exists. If a wrapper or server function needs a field that the generated client does not expose cleanly, fix the Go DTO/OpenAPI source and regenerate.
