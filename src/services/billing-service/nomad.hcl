job "billing" {
  name = "billing"
  datacenters = ["dc1"]
  type = "service"
  group "billing-service" {
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
    task "billing-service-migrate" {
      driver = "raw_exec"
      user = "billing"
      lifecycle {
        hook = "prestart"
        sidecar = false
      }
      artifact {
        source = "verself-artifact://billing-service"
        destination = "local"
        chown = true
      }
      config {
        args = ["migrate", "up"]
        command = "local/bin/billing-service"
      }
      env {
        BILLING_TB_ADDRESS = "127.0.0.1:3320"
        BILLING_TB_CLUSTER_ID = "0"
        OTEL_EXPORTER_OTLP_ENDPOINT = "http://127.0.0.1:4317"
        OTEL_RESOURCE_ATTRIBUTES = "verself.supervisor=nomad"
        OTEL_SERVICE_NAME = "billing-service-migration"
        SPIFFE_ENDPOINT_SOCKET = "unix:///run/spire-agent/sockets/agent.sock"
        VERSELF_AUTH_AUDIENCE = "370208441207143780"
        VERSELF_AUTH_ISSUER_URL = "https://auth.verself.sh"
        VERSELF_CLICKHOUSE_ADDRESS = "127.0.0.1:9440"
        VERSELF_CLICKHOUSE_USER = "billing_service"
        VERSELF_CRED_CLICKHOUSE_CA_CERT = "/etc/credstore/billing/clickhouse-ca-cert"
        VERSELF_INTERNAL_LISTEN_ADDR = "127.0.0.1:$${NOMAD_PORT_internal_https}"
        VERSELF_LISTEN_ADDR = "127.0.0.1:$${NOMAD_PORT_public_http}"
        VERSELF_PG_CONN_MAX_IDLE_SECONDS = "300"
        VERSELF_PG_CONN_MAX_LIFETIME_SECONDS = "1800"
        VERSELF_PG_DSN = "postgres://billing@/billing?host=/var/run/postgresql&sslmode=disable"
        VERSELF_PG_MAX_CONNS = "12"
        VERSELF_PG_MIN_CONNS = "1"
        VERSELF_SUPERVISOR = "nomad"
      }
      resources {
        cpu = 100
        memory = 128
      }
    }
    task "billing-service" {
      driver = "raw_exec"
      user = "billing"
      kill_signal = "SIGTERM"
      kill_timeout = "30s"
      artifact {
        source = "verself-artifact://billing-service"
        destination = "local"
        chown = true
      }
      config {
        command = "local/bin/billing-service"
      }
      env {
        BILLING_TB_ADDRESS = "127.0.0.1:3320"
        BILLING_TB_CLUSTER_ID = "0"
        OTEL_EXPORTER_OTLP_ENDPOINT = "http://127.0.0.1:4317"
        OTEL_RESOURCE_ATTRIBUTES = "verself.supervisor=nomad"
        OTEL_SERVICE_NAME = "billing-service"
        SPIFFE_ENDPOINT_SOCKET = "unix:///run/spire-agent/sockets/agent.sock"
        VERSELF_AUTH_AUDIENCE = "370208441207143780"
        VERSELF_AUTH_ISSUER_URL = "https://auth.verself.sh"
        VERSELF_CLICKHOUSE_ADDRESS = "127.0.0.1:9440"
        VERSELF_CLICKHOUSE_USER = "billing_service"
        VERSELF_CRED_CLICKHOUSE_CA_CERT = "/etc/credstore/billing/clickhouse-ca-cert"
        VERSELF_INTERNAL_LISTEN_ADDR = "127.0.0.1:$${NOMAD_PORT_internal_https}"
        VERSELF_LISTEN_ADDR = "127.0.0.1:$${NOMAD_PORT_public_http}"
        VERSELF_PG_CONN_MAX_IDLE_SECONDS = "300"
        VERSELF_PG_CONN_MAX_LIFETIME_SECONDS = "1800"
        VERSELF_PG_DSN = "postgres://billing@/billing?host=/var/run/postgresql&sslmode=disable"
        VERSELF_PG_MAX_CONNS = "12"
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
        name = "billing-internal-https"
        port = "internal_https"
        provider = "nomad"
        address_mode = "auto"
      }
      service {
        name = "billing-public-http"
        port = "public_http"
        provider = "nomad"
        address_mode = "auto"
        check {
          name = "billing-service-http-public_http"
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
BILLING_SECRETS_URL=https://{{- with nomadService "secrets-service-internal-https" }}{{ with index . 0 }}{{ .Address }}:{{ .Port }}{{ end }}{{- else }}127.0.0.1:1{{- end }}
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
}
