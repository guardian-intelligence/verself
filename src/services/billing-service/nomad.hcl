job "billing" {
  name = "billing"
  datacenters = ["dc1"]
  type = "service"
  group "billing-service" {
    count = 2

    meta {
      verself_group_kind = "service"
      verself_allow_prestart_migration = "true"
      verself_template_fallback = "blackhole"
      verself_template_fallback_reason = "required upstream is registered by a separate Nomad job"
    }
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
        BILLING_TB_CLUSTER_ID = "0"
        BILLING_RETURN_ORIGINS = "https://verself.sh"
        OTEL_EXPORTER_OTLP_ENDPOINT = "http://127.0.0.1:4317"
        OTEL_RESOURCE_ATTRIBUTES = "verself.supervisor=nomad"
        OTEL_SERVICE_NAME = "billing-service-migration"
        SPIFFE_ENDPOINT_SOCKET = "unix:///run/spire-agent/sockets/agent.sock"
        VERSELF_AUTH_ISSUER_URL = "https://verself.sh"
        VERSELF_CLICKHOUSE_ADDRESS = "127.0.0.1:9440"
        VERSELF_CLICKHOUSE_USER = "billing_service"
        VERSELF_CRED_AUTH_AUDIENCE = "/etc/credstore/billing/auth-audience"
        VERSELF_CRED_CLICKHOUSE_CA_CERT = "/etc/credstore/billing/clickhouse-ca-cert"
        VERSELF_INSTALLATION_ID = "inst_5NZSEA08R8P3HN566DNH8D301M"
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
      template {
        change_mode = "restart"
        destination = "secrets/platform.env"
        data = <<-EOT
BILLING_TB_ADDRESS={{- with nomadService "tigerbeetle-client" }}{{ with index . 0 }}{{ .Address }}:{{ .Port }}{{ end }}{{- else }}127.0.0.1:1{{- end }}
EOT
        env = true
      }
    }
    task "billing-service" {
      driver = "raw_exec"
      user = "billing"
      kill_signal = "SIGTERM"
      kill_timeout = "30s"
      shutdown_delay = "5s"
      artifact {
        source = "verself-artifact://billing-service"
        destination = "local"
        chown = true
      }
      config {
        command = "local/bin/billing-service"
      }
      env {
        BILLING_TB_CLUSTER_ID = "0"
        BILLING_RETURN_ORIGINS = "https://verself.sh"
        OTEL_EXPORTER_OTLP_ENDPOINT = "http://127.0.0.1:4317"
        OTEL_RESOURCE_ATTRIBUTES = "verself.supervisor=nomad"
        OTEL_SERVICE_NAME = "billing-service"
        SPIFFE_ENDPOINT_SOCKET = "unix:///run/spire-agent/sockets/agent.sock"
        VERSELF_AUTH_ISSUER_URL = "https://verself.sh"
        VERSELF_CLICKHOUSE_ADDRESS = "127.0.0.1:9440"
        VERSELF_CLICKHOUSE_USER = "billing_service"
        VERSELF_CRED_AUTH_AUDIENCE = "/etc/credstore/billing/auth-audience"
        VERSELF_CRED_CLICKHOUSE_CA_CERT = "/etc/credstore/billing/clickhouse-ca-cert"
        VERSELF_INSTALLATION_ID = "inst_5NZSEA08R8P3HN566DNH8D301M"
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
        name = "billing-service-internal-https"
        port = "internal_https"
        provider = "nomad"
        address_mode = "auto"
        check {
          name = "billing-service-tcp-internal_https"
          type = "tcp"
          port = "internal_https"
          interval = "1s"
          timeout = "3s"
        }
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
BILLING_TB_ADDRESS={{- with nomadService "tigerbeetle-client" }}{{ with index . 0 }}{{ .Address }}:{{ .Port }}{{ end }}{{- else }}127.0.0.1:1{{- end }}
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
