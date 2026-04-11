// Package apiwire owns shared DTO language for service HTTP boundaries.
//
// Go domain packages can keep native uint64/int64 identifiers and quantities.
// Huma request/response DTOs use this package when a value crosses a service
// or frontend boundary and needs an explicit JSON/OpenAPI contract.
package apiwire
