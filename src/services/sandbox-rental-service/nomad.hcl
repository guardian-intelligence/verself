job "sandbox-rental" {
  name = "sandbox-rental"
  datacenters = ["*"]
  type = "service"
  group "sandbox-rental-service" {
    count = 2
    network {
      mode = "host"
      port "internal_https" {
        host_network = "loopback"
      }
      port "public_http" {
        host_network = "loopback"
      }
    }
    task "sandbox-rental-service-migrate" {
      driver = "raw_exec"
      user = "sandbox_rental"
      vault {
        role = "sandbox-rental-runtime"
      }

      identity {
        name = "vault_default"
        aud  = ["vault.io"]
        ttl  = "1h"
      }

      lifecycle {
        hook = "prestart"
        sidecar = false
      }
      artifact {
        source = "verself-artifact://sandbox-rental-service"
        destination = "local"
        chown = true
      }
      config {
        args = ["migrate", "up"]
        command = "local/bin/sandbox-rental-service"
      }
      env {
        OTEL_EXPORTER_OTLP_ENDPOINT = "http://127.0.0.1:4317"
        OTEL_RESOURCE_ATTRIBUTES = "verself.supervisor=nomad"
        OTEL_SERVICE_NAME = "sandbox-rental-service-migration"
        SANDBOX_EXECUTION_MAX_WORKERS = "4"
        SANDBOX_GITHUB_CHECKOUT_BUNDLE_STORE_DIR = "/var/lib/verself/sandbox-rental/github-checkout-bundles"
        SANDBOX_GITHUB_WEB_BASE_URL = "https://github.com"
        SANDBOX_PUBLIC_BASE_URL = "__VERSELF_SANDBOX_RENTAL_SERVICE_BASE_URL__"
        SANDBOX_TEMPORAL_NAMESPACE = "sandbox-rental-service"
        SANDBOX_TEMPORAL_TASK_QUEUE_RECURRING = "sandbox-rental-service.recurring-vm"
        SANDBOX_VM_ORCHESTRATOR_SOCKET = "/run/vm-orchestrator/api.sock"
        SPIFFE_ENDPOINT_SOCKET = "unix:///run/spire-agent/sockets/agent.sock"
        VERSELF_AUTH_ISSUER_URL = "__VERSELF_AUTH_ISSUER_URL__"
        VERSELF_CLICKHOUSE_ADDRESS = "127.0.0.1:9440"
        VERSELF_CLICKHOUSE_USER = "sandbox_rental"
        VERSELF_CRED_CLICKHOUSE_CA_CERT = "/etc/verself/clickhouse/server-ca.pem"
        VERSELF_CRED_RUNNER_BOOTSTRAP_SECRET = "/etc/credstore/sandbox-rental/runner-bootstrap-secret"
        VERSELF_INSTALLATION_ID = "__VERSELF_INSTALLATION_ID__"
        VERSELF_INTERNAL_LISTEN_ADDR = "127.0.0.1:$${NOMAD_PORT_internal_https}"
        VERSELF_LISTEN_ADDR = "127.0.0.1:$${NOMAD_PORT_public_http}"
        VERSELF_PG_CONN_MAX_IDLE_SECONDS = "300"
        VERSELF_PG_CONN_MAX_LIFETIME_SECONDS = "1800"
        VERSELF_PG_DSN = "postgres://sandbox_rental@/sandbox_rental?host=/var/run/postgresql&sslmode=disable"
        VERSELF_PG_MAX_CONNS = "16"
        VERSELF_PG_MIN_CONNS = "1"
        VERSELF_SUPERVISOR = "nomad"
      }
      template {
        change_mode = "restart"
        destination = "secrets/auth-audience.env"
        env = true
        data = <<-EOT
VERSELF_CRED_VALUE_AUTH_AUDIENCE={{ with secret "kv-runtime/data/secret/org/iam-service.zitadel.auth_audience" }}{{ .Data.data.value }}{{ end }}
EOT
      }
      resources {
        cpu = 100
        memory = 128
      }
    }
    task "sandbox-rental-service" {
      driver = "raw_exec"
      user = "sandbox_rental"
      kill_signal = "SIGTERM"
      kill_timeout = "30s"
      shutdown_delay = "5s"
      vault {
        role = "sandbox-rental-runtime"
      }

      identity {
        name = "vault_default"
        aud  = ["vault.io"]
        ttl  = "1h"
      }

      artifact {
        source = "verself-artifact://sandbox-rental-service"
        destination = "local"
        chown = true
      }
      config {
        command = "local/bin/sandbox-rental-service"
      }
      env {
        OTEL_EXPORTER_OTLP_ENDPOINT = "http://127.0.0.1:4317"
        OTEL_RESOURCE_ATTRIBUTES = "verself.supervisor=nomad"
        OTEL_SERVICE_NAME = "sandbox-rental-service"
        SANDBOX_EXECUTION_MAX_WORKERS = "4"
        SANDBOX_GITHUB_CHECKOUT_BUNDLE_STORE_DIR = "/var/lib/verself/sandbox-rental/github-checkout-bundles"
        SANDBOX_GITHUB_WEB_BASE_URL = "https://github.com"
        SANDBOX_PUBLIC_BASE_URL = "__VERSELF_SANDBOX_RENTAL_SERVICE_BASE_URL__"
        SANDBOX_TEMPORAL_NAMESPACE = "sandbox-rental-service"
        SANDBOX_TEMPORAL_TASK_QUEUE_RECURRING = "sandbox-rental-service.recurring-vm"
        SANDBOX_VM_ORCHESTRATOR_SOCKET = "/run/vm-orchestrator/api.sock"
        SPIFFE_ENDPOINT_SOCKET = "unix:///run/spire-agent/sockets/agent.sock"
        VERSELF_AUTH_ISSUER_URL = "__VERSELF_AUTH_ISSUER_URL__"
        VERSELF_CLICKHOUSE_ADDRESS = "127.0.0.1:9440"
        VERSELF_CLICKHOUSE_USER = "sandbox_rental"
        VERSELF_CRED_CLICKHOUSE_CA_CERT = "/etc/verself/clickhouse/server-ca.pem"
        VERSELF_CRED_RUNNER_BOOTSTRAP_SECRET = "/etc/credstore/sandbox-rental/runner-bootstrap-secret"
        VERSELF_INSTALLATION_ID = "__VERSELF_INSTALLATION_ID__"
        VERSELF_INTERNAL_LISTEN_ADDR = "127.0.0.1:$${NOMAD_PORT_internal_https}"
        VERSELF_LISTEN_ADDR = "127.0.0.1:$${NOMAD_PORT_public_http}"
        VERSELF_PG_CONN_MAX_IDLE_SECONDS = "300"
        VERSELF_PG_CONN_MAX_LIFETIME_SECONDS = "1800"
        VERSELF_PG_DSN = "postgres://sandbox_rental@/sandbox_rental?host=/var/run/postgresql&sslmode=disable"
        VERSELF_PG_MAX_CONNS = "16"
        VERSELF_PG_MIN_CONNS = "1"
        VERSELF_SUPERVISOR = "nomad"
      }
      template {
        change_mode = "restart"
        destination = "secrets/auth-audience.env"
        env = true
        data = <<-EOT
VERSELF_CRED_VALUE_AUTH_AUDIENCE={{ with secret "kv-runtime/data/secret/org/iam-service.zitadel.auth_audience" }}{{ .Data.data.value }}{{ end }}
EOT
      }
      resources {
        cpu = 500
        memory = 512
      }
      restart {
        attempts = 3
        delay = "15s"
        interval = "300s"
        mode = "delay"
      }
      service {
        name = "sandbox-rental-service-internal-https"
        port = "internal_https"
        provider = "nomad"
        address_mode = "auto"
        check {
          name = "sandbox-rental-service-http-public-readiness"
          type = "http"
          path = "/readyz"
          port = "public_http"
          interval = "1s"
          timeout = "3s"
        }
      }
      service {
        name = "sandbox-rental-public-http"
        port = "public_http"
        provider = "nomad"
        address_mode = "auto"
        check {
          name = "sandbox-rental-service-http-public_http"
          type = "http"
          path = "/readyz"
          port = "public_http"
          interval = "1s"
          timeout = "3s"
        }
      }
      template {
        change_mode = "restart"
        destination = "secrets/upstreams.env"
        data = <<-EOT
SANDBOX_TEMPORAL_FRONTEND_ADDRESS={{- with nomadService "temporal-frontend-grpc" }}{{ with index . 0 }}{{ .Address }}:{{ .Port }}{{ end }}{{- else }}127.0.0.1:1{{- end }}
EOT
        env = true
      }
    }
    update {
      max_parallel = 1
      health_check = "checks"
      min_healthy_time = "3s"
      healthy_deadline = "300s"
      progress_deadline = "600s"
      canary = 1
      auto_revert = true
      auto_promote = true
    }
  }
  group "sandbox-rental-recurring-worker" {
    count = 1
    task "sandbox-rental-recurring-worker" {
      driver = "raw_exec"
      user = "sandbox_rental"
      kill_signal = "SIGTERM"
      kill_timeout = "30s"
      shutdown_delay = "5s"
      artifact {
        source = "verself-artifact://sandbox-rental-recurring-worker"
        destination = "local"
        chown = true
      }
      config {
        command = "local/bin/sandbox-rental-recurring-worker"
      }
      env {
        OTEL_EXPORTER_OTLP_ENDPOINT = "http://127.0.0.1:4317"
        OTEL_RESOURCE_ATTRIBUTES = "verself.supervisor=nomad"
        OTEL_SERVICE_NAME = "sandbox-rental-recurring-worker"
        SANDBOX_TEMPORAL_NAMESPACE = "sandbox-rental-service"
        SANDBOX_TEMPORAL_TASK_QUEUE_RECURRING = "sandbox-rental-service.recurring-vm"
        SPIFFE_ENDPOINT_SOCKET = "unix:///run/spire-agent/sockets/agent.sock"
        VERSELF_PG_CONN_MAX_IDLE_SECONDS = "300"
        VERSELF_PG_CONN_MAX_LIFETIME_SECONDS = "1800"
        VERSELF_PG_DSN = "postgres://sandbox_rental@/sandbox_rental?host=/var/run/postgresql&sslmode=disable"
        VERSELF_PG_MAX_CONNS = "4"
        VERSELF_PG_MIN_CONNS = "1"
        VERSELF_SUPERVISOR = "nomad"
      }
      resources {
        cpu = 500
        memory = 512
      }
      restart {
        attempts = 3
        delay = "15s"
        interval = "300s"
        mode = "delay"
      }
      template {
        change_mode = "restart"
        destination = "secrets/upstreams.env"
        data = <<-EOT
SANDBOX_TEMPORAL_FRONTEND_ADDRESS={{- with nomadService "temporal-frontend-grpc" }}{{ with index . 0 }}{{ .Address }}:{{ .Port }}{{ end }}{{- else }}127.0.0.1:1{{- end }}
EOT
        env = true
      }
    }
    update {
      max_parallel = 1
      health_check = "task_states"
      min_healthy_time = "1s"
      healthy_deadline = "30s"
      progress_deadline = "60s"
      canary = 0
      auto_revert = true
      auto_promote = false
    }
  }
}
