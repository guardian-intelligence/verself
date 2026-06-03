module github.com/verself/deployment-tools

go 1.25.8

require (
	github.com/google/uuid v1.6.0
	github.com/verself/deployment-service v0.0.0
	go.opentelemetry.io/otel v1.43.0
	golang.org/x/crypto v0.49.0
	gopkg.in/yaml.v3 v3.0.1
)

require (
	github.com/hashicorp/nomad/api v0.0.0-20260501195443-d922fcdd63e4 // indirect
	github.com/verself/integrations/cloudflare/r2-control-plane v0.0.0-00010101000000-000000000000 // indirect
	go.opentelemetry.io/otel/trace v1.43.0 // indirect
)

require (
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/davecgh/go-spew v1.1.2-0.20180830191138-d8f796af33cc // indirect
	github.com/go-logr/logr v1.4.3 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/go-viper/mapstructure/v2 v2.5.0 // indirect
	github.com/gorilla/websocket v1.5.3 // indirect
	github.com/hashicorp/cronexpr v1.1.3 // indirect
	github.com/hashicorp/errwrap v1.0.0 // indirect
	github.com/hashicorp/go-cleanhttp v0.5.2 // indirect
	github.com/hashicorp/go-multierror v1.1.1 // indirect
	github.com/hashicorp/go-rootcerts v1.0.2 // indirect
	github.com/mitchellh/go-homedir v1.1.0 // indirect
	github.com/pmezard/go-difflib v1.0.1-0.20181226105442-5d4384ee4fb2 // indirect
	go.opentelemetry.io/auto/sdk v1.2.1 // indirect
	go.opentelemetry.io/otel/metric v1.43.0 // indirect
	golang.org/x/sys v0.42.0 // indirect
)

replace github.com/verself/observability => ../observability/go

replace github.com/verself/deployment-service => ../../services/deployment-service

replace github.com/verself/integrations/cloudflare/r2-control-plane => ../../integrations/cloudflare/r2-control-plane
