job "sandbox-rental" {
  name = "sandbox-rental"
  datacenters = ["dc1"]
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
        SANDBOX_BILLING_RETURN_ORIGINS = "https://verself.sh"
        SANDBOX_EXECUTION_MAX_WORKERS = "4"
        SANDBOX_FORGEJO_API_BASE_URL = "http://127.0.0.1:3000"
        SANDBOX_FORGEJO_RUNNER_BASE_URL = "http://10.255.0.1:18080"
        SANDBOX_FORGEJO_WEBHOOK_BASE_URL = "https://sandbox.api.verself.sh"
        SANDBOX_GITHUB_API_BASE_URL = "https://api.github.com"
        SANDBOX_GITHUB_APP_CLIENT_ID = "Iv23liDpxGOmBSQwSJ5i"
        SANDBOX_GITHUB_APP_ID = "3370540"
        SANDBOX_GITHUB_APP_SLUG = "verself-ci"
        SANDBOX_GITHUB_CHECKOUT_CACHE_DIR = "/var/lib/verself/sandbox-rental/github-checkout"
        SANDBOX_GITHUB_RUNNER_GROUP_ID = "1"
        SANDBOX_GITHUB_WEB_BASE_URL = "https://github.com"
        SANDBOX_PUBLIC_BASE_URL = "https://sandbox.api.verself.sh"
        SANDBOX_TEMPORAL_NAMESPACE = "sandbox-rental-service"
        SANDBOX_TEMPORAL_TASK_QUEUE_RECURRING = "sandbox-rental-service.recurring-vm"
        SANDBOX_VM_ORCHESTRATOR_SOCKET = "/run/vm-orchestrator/api.sock"
        SPIFFE_ENDPOINT_SOCKET = "unix:///run/spire-agent/sockets/agent.sock"
        VERSELF_AUTH_AUDIENCE = "370200928688586084"
        VERSELF_AUTH_ISSUER_URL = "https://auth.verself.sh"
        VERSELF_CLICKHOUSE_ADDRESS = "127.0.0.1:9440"
        VERSELF_CLICKHOUSE_USER = "sandbox_rental"
        VERSELF_CRED_CLICKHOUSE_CA_CERT = "/etc/credstore/sandbox-rental/clickhouse-ca-cert"
        VERSELF_CRED_FORGEJO_BOOTSTRAP_SECRET = "/etc/credstore/sandbox-rental/forgejo-bootstrap-secret"
        VERSELF_CRED_FORGEJO_TOKEN = "/etc/credstore/sandbox-rental/forgejo-token"
        VERSELF_CRED_FORGEJO_WEBHOOK_SECRET = "/etc/credstore/sandbox-rental/forgejo-webhook-secret"
        VERSELF_INTERNAL_LISTEN_ADDR = "127.0.0.1:$${NOMAD_PORT_internal_https}"
        VERSELF_LISTEN_ADDR = "127.0.0.1:$${NOMAD_PORT_public_http}"
        VERSELF_PG_CONN_MAX_IDLE_SECONDS = "300"
        VERSELF_PG_CONN_MAX_LIFETIME_SECONDS = "1800"
        VERSELF_PG_DSN = "postgres://sandbox_rental@/sandbox_rental?host=/var/run/postgresql&sslmode=disable"
        VERSELF_PG_MAX_CONNS = "16"
        VERSELF_PG_MIN_CONNS = "1"
        VERSELF_SUPERVISOR = "nomad"
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
        SANDBOX_BILLING_RETURN_ORIGINS = "https://verself.sh"
        SANDBOX_EXECUTION_MAX_WORKERS = "4"
        SANDBOX_FORGEJO_API_BASE_URL = "http://127.0.0.1:3000"
        SANDBOX_FORGEJO_RUNNER_BASE_URL = "http://10.255.0.1:18080"
        SANDBOX_FORGEJO_WEBHOOK_BASE_URL = "https://sandbox.api.verself.sh"
        SANDBOX_GITHUB_API_BASE_URL = "https://api.github.com"
        SANDBOX_GITHUB_APP_CLIENT_ID = "Iv23liDpxGOmBSQwSJ5i"
        SANDBOX_GITHUB_APP_ID = "3370540"
        SANDBOX_GITHUB_APP_SLUG = "verself-ci"
        SANDBOX_GITHUB_CHECKOUT_CACHE_DIR = "/var/lib/verself/sandbox-rental/github-checkout"
        SANDBOX_GITHUB_RUNNER_GROUP_ID = "1"
        SANDBOX_GITHUB_WEB_BASE_URL = "https://github.com"
        SANDBOX_PUBLIC_BASE_URL = "https://sandbox.api.verself.sh"
        SANDBOX_TEMPORAL_NAMESPACE = "sandbox-rental-service"
        SANDBOX_TEMPORAL_TASK_QUEUE_RECURRING = "sandbox-rental-service.recurring-vm"
        SANDBOX_VM_ORCHESTRATOR_SOCKET = "/run/vm-orchestrator/api.sock"
        SPIFFE_ENDPOINT_SOCKET = "unix:///run/spire-agent/sockets/agent.sock"
        VERSELF_AUTH_AUDIENCE = "370200928688586084"
        VERSELF_AUTH_ISSUER_URL = "https://auth.verself.sh"
        VERSELF_CLICKHOUSE_ADDRESS = "127.0.0.1:9440"
        VERSELF_CLICKHOUSE_USER = "sandbox_rental"
        VERSELF_CRED_CLICKHOUSE_CA_CERT = "/etc/credstore/sandbox-rental/clickhouse-ca-cert"
        VERSELF_CRED_FORGEJO_BOOTSTRAP_SECRET = "/etc/credstore/sandbox-rental/forgejo-bootstrap-secret"
        VERSELF_CRED_FORGEJO_TOKEN = "/etc/credstore/sandbox-rental/forgejo-token"
        VERSELF_CRED_FORGEJO_WEBHOOK_SECRET = "/etc/credstore/sandbox-rental/forgejo-webhook-secret"
        VERSELF_INTERNAL_LISTEN_ADDR = "127.0.0.1:$${NOMAD_PORT_internal_https}"
        VERSELF_LISTEN_ADDR = "127.0.0.1:$${NOMAD_PORT_public_http}"
        VERSELF_PG_CONN_MAX_IDLE_SECONDS = "300"
        VERSELF_PG_CONN_MAX_LIFETIME_SECONDS = "1800"
        VERSELF_PG_DSN = "postgres://sandbox_rental@/sandbox_rental?host=/var/run/postgresql&sslmode=disable"
        VERSELF_PG_MAX_CONNS = "16"
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
      service {
        name = "sandbox-rental-internal-https"
        port = "internal_https"
        provider = "nomad"
        address_mode = "auto"
        check {
          name = "sandbox-rental-service-tcp-internal_https"
          type = "tcp"
          port = "internal_https"
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
          name = "sandbox-rental-service-tcp-public_http"
          type = "tcp"
          port = "public_http"
          interval = "1s"
          timeout = "3s"
        }
      }
      template {
        change_mode = "restart"
        destination = "secrets/upstreams.env"
        data = <<-EOT
SANDBOX_BILLING_URL=https://{{- with nomadService "billing-internal-https" }}{{ with index . 0 }}{{ .Address }}:{{ .Port }}{{ end }}{{- else }}127.0.0.1:1{{- end }}
SANDBOX_GOVERNANCE_AUDIT_URL=https://{{- with nomadService "governance-service-internal-https" }}{{ with index . 0 }}{{ .Address }}:{{ .Port }}{{ end }}{{- else }}127.0.0.1:1{{- end }}
SANDBOX_SECRETS_URL=https://{{- with nomadService "secrets-service-internal-https" }}{{ with index . 0 }}{{ .Address }}:{{ .Port }}{{ end }}{{- else }}127.0.0.1:1{{- end }}
SANDBOX_SOURCE_INTERNAL_URL=https://{{- with nomadService "source-code-hosting-service-internal-https" }}{{ with index . 0 }}{{ .Address }}:{{ .Port }}{{ end }}{{- else }}127.0.0.1:1{{- end }}
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
SANDBOX_SOURCE_INTERNAL_URL=https://{{- with nomadService "source-code-hosting-service-internal-https" }}{{ with index . 0 }}{{ .Address }}:{{ .Port }}{{ end }}{{- else }}127.0.0.1:1{{- end }}
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
