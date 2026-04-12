# apiwire

`src/apiwire` owns the shared DTO language used at Go service HTTP boundaries, committed OpenAPI specs, generated Go/TypeScript clients, and app-facing TypeScript wrappers. Domain packages stay free to use native Go types such as `uint64`, `int64`, service-local aliases, and richer business structs; this package owns only the wire shape that another service, generated client, or frontend consumes.

## Ownership

- Put a DTO in `apiwire` when its field language is shared across services, OpenAPI codegen, or frontend wrappers.
- Keep purely internal domain structs, persistence records, provider webhook payloads, and one-off adapter structs in the owning service package.
- Keep conversion between domain structs and DTOs next to the Huma handler or client adapter that knows the local domain model.
- Do not import service packages from `apiwire`; dependencies should stay limited to standard-library encoding/parsing packages plus schema-generation dependencies such as Huma.
- Do not add compatibility wrappers or legacy shims. When a wire shape moves here, cut callers over and delete the old service-local DTO.

## Numeric Wire Rules

- Frontend-facing 64-bit identifiers are decimal strings, never JSON numbers.
- Use `DecimalUint64`, `DecimalInt64`, or a semantic alias such as `OrgID` for those DTO fields.
- Use `Uint64`, `Int64`, `ParseUint64`, and `ParseInt64`; do not hand-roll `strconv.FormatUint`, `strconv.ParseUint`, or raw string fields in service-local DTOs for shared 64-bit values.
- Frontend-facing quantities may remain JSON numbers only when the OpenAPI schema proves `maximum <= 9007199254740991`.
- Unbounded 64-bit quantities must be decimal string DTOs or an explicitly documented `x-js-wire: bigint` contract that a TypeScript wrapper converts deliberately.
- Internal Go request handling can parse path/query strings into native numeric domain types after the transport boundary.

## Schema Contract

- Decimal wire types must implement `json.Marshaler`, `json.Unmarshaler`, text marshal/unmarshal, and `huma.SchemaProvider`.
- `DecimalUint64` schemas must emit `type: string` and `pattern: "^[0-9]+$"`; signed decimal types must use the signed pattern.
- If you add a new primitive wire type, add focused tests for JSON rejection of unsafe representations, parser edge cases, and Huma schema generation.
- If a DTO intentionally exposes an integer number to TypeScript, include `minimum` and `maximum` tags where needed so the generated OpenAPI contract proves JavaScript safety.

## DTO Shape

- Prefer boring exported structs with JSON tags and service-prefix names when the concept is service-specific but shared on the wire, for example `BillingEntitlementsView`, `SandboxRepo`, or `MailboxEmail`.
- Use semantic aliases for shared identifiers when the alias improves contract readability without hiding the underlying wire encoding.
- Keep DTO structs free of persistence tags, auth policy state, database handles, and methods with side effects.
- Keep timestamps as explicit wire types already accepted by the existing codegen path; do not introduce custom time formats without updating Go clients, TypeScript wrappers, and docs together.
- Keep maps only when the wire vocabulary is genuinely open-ended; use named structs when the set of fields is part of the contract.

## Boundary Pattern

Service handlers should return DTOs from Huma operations and convert close to the handler:

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

Generated clients should parse decimal-string DTO fields back into the app-facing type they intentionally expose. TypeScript app code should call wrapper modules that Valibot-parse generated responses before business logic sees them.

## Verification

- Run `go test ./...` from `src/apiwire` after changes in this package.
- Regenerate affected OpenAPI specs and generated clients when a DTO shape changes.
- Run `make openapi-check` after spec regeneration; it includes the wire checker for unsafe `int64` and `uint64` schemas.
- Run affected service tests and frontend typechecks for wrappers consuming the regenerated clients.
- For behavior-affecting contract changes, prove the deployed path through ClickHouse traces/logs, not just local tests.
