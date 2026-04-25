module github.com/verself/sandbox-rental-service

go 1.25.8

require (
	github.com/ClickHouse/clickhouse-go/v2 v2.30.1
	github.com/danielgtaylor/huma/v2 v2.37.3
	github.com/golang-jwt/jwt/v5 v5.3.1
	github.com/google/uuid v1.6.0
	github.com/jackc/pgx/v5 v5.9.1
	github.com/lib/pq v1.12.3
	github.com/oapi-codegen/runtime v1.4.0
	github.com/riverqueue/river v0.34.0
	github.com/riverqueue/river/riverdriver/riverpgxv5 v0.34.0
	github.com/riverqueue/river/rivertype v0.34.0
	github.com/riverqueue/rivercontrib/otelriver v0.7.0
	github.com/spiffe/go-spiffe/v2 v2.6.0
	github.com/verself/apiwire v0.0.0
	github.com/verself/auth-middleware v0.0.0
	github.com/verself/billing-service v0.0.0
	github.com/verself/envconfig v0.0.0
	github.com/verself/governance-service v0.0.0
	github.com/verself/httpserver v0.0.0
	github.com/verself/otel v0.0.0
	github.com/verself/secrets-service v0.0.0
	github.com/verself/source-code-hosting-service v0.0.0
	github.com/verself/temporal-platform v0.0.0
	github.com/verself/vm-orchestrator v0.0.0
	go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp v0.68.0
	go.opentelemetry.io/otel v1.43.0
	go.opentelemetry.io/otel/trace v1.43.0
	go.temporal.io/api v1.62.8
	go.temporal.io/sdk v1.38.0
	gopkg.in/yaml.v3 v3.0.1
)

require (
	github.com/ClickHouse/ch-go v0.63.1 // indirect
	github.com/Microsoft/go-winio v0.6.2 // indirect
	github.com/andybalholm/brotli v1.2.0 // indirect
	github.com/apapsch/go-jsonmerge/v2 v2.0.0 // indirect
	github.com/cenkalti/backoff/v4 v4.3.0 // indirect
	github.com/cenkalti/backoff/v5 v5.0.3 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/coreos/go-oidc/v3 v3.17.0 // indirect
	github.com/davecgh/go-spew v1.1.2-0.20180830191138-d8f796af33cc // indirect
	github.com/dustin/go-humanize v1.0.1 // indirect
	github.com/facebookgo/clock v0.0.0-20150410010913-600d898af40a // indirect
	github.com/felixge/httpsnoop v1.0.4 // indirect
	github.com/go-faster/city v1.0.1 // indirect
	github.com/go-faster/errors v0.7.1 // indirect
	github.com/go-jose/go-jose/v4 v4.1.4 // indirect
	github.com/go-logr/logr v1.4.3 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/gogo/protobuf v1.3.2 // indirect
	github.com/golang/mock v1.6.0 // indirect
	github.com/grpc-ecosystem/go-grpc-middleware/v2 v2.3.2 // indirect
	github.com/grpc-ecosystem/grpc-gateway/v2 v2.28.0 // indirect
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/jackc/puddle/v2 v2.2.2 // indirect
	github.com/klauspost/compress v1.18.4 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/ncruces/go-strftime v1.0.0 // indirect
	github.com/nexus-rpc/sdk-go v0.5.1 // indirect
	github.com/oklog/ulid/v2 v2.1.1 // indirect
	github.com/paulmach/orb v0.11.1 // indirect
	github.com/pierrec/lz4/v4 v4.1.21 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/pmezard/go-difflib v1.0.1-0.20181226105442-5d4384ee4fb2 // indirect
	github.com/remyoudompheng/bigfft v0.0.0-20230129092748-24d4a6f8daec // indirect
	github.com/riverqueue/river/riverdriver v0.34.0 // indirect
	github.com/riverqueue/river/rivershared v0.34.0 // indirect
	github.com/robfig/cron v1.2.0 // indirect
	github.com/segmentio/asm v1.2.0 // indirect
	github.com/shopspring/decimal v1.4.0 // indirect
	github.com/stretchr/objx v0.5.2 // indirect
	github.com/stretchr/testify v1.11.1 // indirect
	github.com/tidwall/gjson v1.18.0 // indirect
	github.com/tidwall/match v1.2.0 // indirect
	github.com/tidwall/pretty v1.2.1 // indirect
	github.com/tidwall/sjson v1.2.5 // indirect
	github.com/vishvananda/netlink v1.3.1 // indirect
	github.com/vishvananda/netns v0.0.5 // indirect
	go.opentelemetry.io/auto/sdk v1.2.1 // indirect
	go.opentelemetry.io/contrib/bridges/otelslog v0.17.0 // indirect
	go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc v0.68.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploggrpc v0.19.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlptrace v1.35.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc v1.35.0 // indirect
	go.opentelemetry.io/otel/log v0.19.0 // indirect
	go.opentelemetry.io/otel/metric v1.43.0 // indirect
	go.opentelemetry.io/otel/sdk v1.43.0 // indirect
	go.opentelemetry.io/otel/sdk/log v0.19.0 // indirect
	go.opentelemetry.io/proto/otlp v1.10.0 // indirect
	go.temporal.io/sdk/contrib/opentelemetry v0.6.0 // indirect
	go.uber.org/goleak v1.3.0 // indirect
	golang.org/x/net v0.52.0 // indirect
	golang.org/x/oauth2 v0.35.0 // indirect
	golang.org/x/sync v0.20.0 // indirect
	golang.org/x/sys v0.42.0 // indirect
	golang.org/x/text v0.35.0 // indirect
	golang.org/x/time v0.14.0 // indirect
	google.golang.org/genproto/googleapis/api v0.0.0-20260401024825-9d38bb4040a9 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260406210006-6f92a3bedf2d // indirect
	google.golang.org/grpc v1.80.0 // indirect
	google.golang.org/protobuf v1.36.11 // indirect
	modernc.org/libc v1.70.0 // indirect
	modernc.org/mathutil v1.7.1 // indirect
	modernc.org/memory v1.11.0 // indirect
	modernc.org/sqlite v1.48.2 // indirect
)

replace (
	github.com/verself/apiwire => ../apiwire
	github.com/verself/auth-middleware => ../auth-middleware
	github.com/verself/billing-service => ../billing-service
	github.com/verself/envconfig => ../envconfig
	github.com/verself/governance-service => ../governance-service
	github.com/verself/httpserver => ../httpserver
	github.com/verself/otel => ../otel
	github.com/verself/secrets-service => ../secrets-service
	github.com/verself/source-code-hosting-service => ../source-code-hosting-service
	github.com/verself/temporal-platform => ../temporal-platform
	github.com/verself/vm-orchestrator => ../vm-orchestrator
)
