module github.com/forge-metal/e2e

go 1.25.0

require (
	github.com/ClickHouse/clickhouse-go/v2 v2.44.0
	github.com/forge-metal/auth-middleware v0.0.0
	github.com/forge-metal/billing v0.0.0
	github.com/forge-metal/billing-client v0.0.0
	github.com/forge-metal/billing-service v0.0.0-00010101000000-000000000000
	github.com/forge-metal/fast-sandbox v0.0.0
	github.com/forge-metal/sandbox-rental-service v0.0.0-00010101000000-000000000000
	github.com/golang-jwt/jwt/v5 v5.3.1
	github.com/lib/pq v1.12.3
	github.com/stripe/stripe-go/v85 v85.0.0
	github.com/tigerbeetle/tigerbeetle-go v0.16.78
)

require (
	github.com/ClickHouse/ch-go v0.71.0 // indirect
	github.com/andybalholm/brotli v1.2.0 // indirect
	github.com/apapsch/go-jsonmerge/v2 v2.0.0 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/coreos/go-oidc/v3 v3.17.0 // indirect
	github.com/danielgtaylor/huma/v2 v2.37.3 // indirect
	github.com/go-faster/city v1.0.1 // indirect
	github.com/go-faster/errors v0.7.1 // indirect
	github.com/go-jose/go-jose/v4 v4.1.3 // indirect
	github.com/go-logr/logr v1.4.3 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/klauspost/compress v1.18.4 // indirect
	github.com/oapi-codegen/runtime v1.3.1 // indirect
	github.com/oklog/ulid/v2 v2.1.0 // indirect
	github.com/paulmach/orb v0.12.0 // indirect
	github.com/pierrec/lz4/v4 v4.1.25 // indirect
	github.com/segmentio/asm v1.2.1 // indirect
	github.com/shopspring/decimal v1.4.0 // indirect
	go.opentelemetry.io/auto/sdk v1.2.1 // indirect
	go.opentelemetry.io/otel v1.43.0 // indirect
	go.opentelemetry.io/otel/metric v1.43.0 // indirect
	go.opentelemetry.io/otel/trace v1.43.0 // indirect
	go.yaml.in/yaml/v3 v3.0.4 // indirect
	golang.org/x/oauth2 v0.35.0 // indirect
	golang.org/x/sys v0.42.0 // indirect
)

replace (
	github.com/forge-metal/auth-middleware => ../auth-middleware
	github.com/forge-metal/billing => ../billing
	github.com/forge-metal/billing-client => ../billing-client
	github.com/forge-metal/billing-service => ../billing-service
	github.com/forge-metal/fast-sandbox => ../fast-sandbox
	github.com/forge-metal/sandbox-rental-service => ../sandbox-rental-service
)
