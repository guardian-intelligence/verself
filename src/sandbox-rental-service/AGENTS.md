# sandbox-rental-service

Public `/api/*` Huma routes must use the secured-operation registration pattern in `internal/api`: keep the method/path/OpenAPI declaration and `operationPolicy` together in `RegisterRoutes` so IAM, rate-limit, idempotency, audit, and generated-client contracts cannot drift.
