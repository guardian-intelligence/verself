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
type balanceOutput struct {
	Body apiwire.BillingBalance
}

func toBalanceDTO(balance billing.Balance) apiwire.BillingBalance {
	return apiwire.BillingBalance{
		OrgID:          apiwire.Uint64(balance.OrgID),
		TotalAvailable: apiwire.Uint64(balance.TotalAvailable),
	}
}
```

Do not use hand-written `strconv.FormatUint` string fields in service-local DTOs for cross-service fields. The decimal type owns JSON encoding and the Huma schema provider.

## Generated Contract Gate

`make openapi-check` runs `make openapi-wire-check`, which scans committed frontend-consumed OpenAPI 3.1 specs. It fails for `type: integer`, `format: int64` or `format: uint64` unless one of these is true:

- `maximum <= 9007199254740991`
- `x-js-wire: bigint`
- the value is encoded as an `apiwire` decimal string DTO instead of an integer schema

The checker also treats OpenAPI 3.1 nullable integer schemas such as `type: [integer, "null"]` as integer schemas.

## TypeScript Boundary Pattern

Generated clients stay generated. App code imports wrapper functions, not raw generated SDK calls. Wrappers parse generated responses with Valibot before values reach app logic, then deliberately convert DTO fields:

- identifiers usually remain branded decimal strings
- safe bounded quantities become `number`
- unbounded quantities become `bigint` only when the wrapper intentionally exposes that type

Do not parse `response.json()` directly in app code for service APIs. If a wrapper needs a field that the generated client does not expose cleanly, fix the Go DTO/OpenAPI source and regenerate.
