// Package dto owns the shared DTO language for service HTTP boundaries.
//
// Go domain packages can keep native uint64/int64 identifiers and quantities.
// Huma request/response DTOs use this package when a value crosses a service or
// frontend boundary and needs an explicit JSON/OpenAPI contract.
//
// Frontend-facing 64-bit identifiers are decimal strings. Use DecimalUint64 or
// DecimalInt64 in DTOs and ParseUint64 or ParseInt64 for inbound path/query
// values. Frontend-facing quantities may stay JSON numbers only when the OpenAPI
// schema proves maximum <= Number.MAX_SAFE_INTEGER.
//
// Keep service-specific conversion next to the boundary that speaks domain
// types. Keep the DTO struct definitions here when more than one service,
// generated client, or frontend wrapper depends on the field language.
package dto
