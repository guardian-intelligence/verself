module github.com/verself/verself-go

go 1.25.8

require (
	github.com/google/uuid v1.6.0
	github.com/spiffe/go-spiffe/v2 v2.6.0
	github.com/verself/projects-service v0.0.0
	go.opentelemetry.io/otel v1.43.0
)

require (
	github.com/Microsoft/go-winio v0.6.2 // indirect
	github.com/felixge/httpsnoop v1.0.4 // indirect
	github.com/go-jose/go-jose/v4 v4.1.3 // indirect
	go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp v0.68.0 // indirect
	golang.org/x/net v0.52.0 // indirect
	golang.org/x/sys v0.42.0 // indirect
	golang.org/x/text v0.35.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260401024825-9d38bb4040a9 // indirect
	google.golang.org/grpc v1.80.0 // indirect
	google.golang.org/protobuf v1.36.11 // indirect
)

require (
	github.com/apapsch/go-jsonmerge/v2 v2.0.0 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/go-logr/logr v1.4.3 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/oapi-codegen/runtime v1.4.0 // indirect
	github.com/verself/service-runtime v0.0.0
	go.opentelemetry.io/auto/sdk v1.2.1 // indirect
	go.opentelemetry.io/otel/metric v1.43.0 // indirect
	go.opentelemetry.io/otel/trace v1.43.0 // indirect
)

replace github.com/verself/projects-service => ../../../services/projects-service

replace github.com/verself/service-runtime => ../../../services/service-runtime/go
