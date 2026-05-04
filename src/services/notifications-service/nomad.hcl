job "notifications-service" {
  name = "notifications-service"
  datacenters = ["dc1"]
  type = "service"
  group "notifications-service" {
    count = 2
    network {
      mode = "host"
      port "public_http" {
        host_network = "loopback"
      }
    }
    task "notifications-service-migrate" {
      driver = "raw_exec"
      user = "notifications_service"
      lifecycle {
        hook = "prestart"
        sidecar = false
      }
      artifact {
        source = "verself-artifact://notifications-service"
        destination = "local"
        chown = true
      }
      config {
        args = ["migrate", "up"]
        command = "local/bin/notifications-service"
      }
      env {
        NOTIFICATIONS_NATS_URL = "tls://127.0.0.1:4222"
        OTEL_EXPORTER_OTLP_ENDPOINT = "http://127.0.0.1:4317"
        OTEL_RESOURCE_ATTRIBUTES = "verself.supervisor=nomad"
        OTEL_SERVICE_NAME = "notifications-service-migration"
        SPIFFE_ENDPOINT_SOCKET = "unix:///run/spire-agent/sockets/agent.sock"
        VERSELF_AUTH_AUDIENCE = "370200564807548260"
        VERSELF_AUTH_ISSUER_URL = "https://auth.verself.sh"
        VERSELF_CLICKHOUSE_ADDRESS = "127.0.0.1:9440"
        VERSELF_CLICKHOUSE_USER = "notifications_service"
        VERSELF_CRED_CLICKHOUSE_CA_CERT = "/etc/credstore/notifications-service/clickhouse-ca-cert"
        VERSELF_LISTEN_ADDR = "127.0.0.1:$${NOMAD_PORT_public_http}"
        VERSELF_PG_CONN_MAX_IDLE_SECONDS = "300"
        VERSELF_PG_CONN_MAX_LIFETIME_SECONDS = "1800"
        VERSELF_PG_DSN = "postgres://notifications_service@/notifications_service?host=/var/run/postgresql&sslmode=disable"
        VERSELF_PG_MAX_CONNS = "8"
        VERSELF_PG_MIN_CONNS = "1"
        VERSELF_SUPERVISOR = "nomad"
      }
      resources {
        cpu = 100
        memory = 128
      }
    }
    task "notifications-service" {
      driver = "raw_exec"
      user = "notifications_service"
      kill_signal = "SIGTERM"
      kill_timeout = "30s"
      artifact {
        source = "verself-artifact://notifications-service"
        destination = "local"
        chown = true
      }
      config {
        command = "local/bin/notifications-service"
      }
      env {
        NOTIFICATIONS_NATS_URL = "tls://127.0.0.1:4222"
        OTEL_EXPORTER_OTLP_ENDPOINT = "http://127.0.0.1:4317"
        OTEL_RESOURCE_ATTRIBUTES = "verself.supervisor=nomad"
        OTEL_SERVICE_NAME = "notifications-service"
        SPIFFE_ENDPOINT_SOCKET = "unix:///run/spire-agent/sockets/agent.sock"
        VERSELF_AUTH_AUDIENCE = "370200564807548260"
        VERSELF_AUTH_ISSUER_URL = "https://auth.verself.sh"
        VERSELF_CLICKHOUSE_ADDRESS = "127.0.0.1:9440"
        VERSELF_CLICKHOUSE_USER = "notifications_service"
        VERSELF_CRED_CLICKHOUSE_CA_CERT = "/etc/credstore/notifications-service/clickhouse-ca-cert"
        VERSELF_LISTEN_ADDR = "127.0.0.1:$${NOMAD_PORT_public_http}"
        VERSELF_PG_CONN_MAX_IDLE_SECONDS = "300"
        VERSELF_PG_CONN_MAX_LIFETIME_SECONDS = "1800"
        VERSELF_PG_DSN = "postgres://notifications_service@/notifications_service?host=/var/run/postgresql&sslmode=disable"
        VERSELF_PG_MAX_CONNS = "8"
        VERSELF_PG_MIN_CONNS = "1"
        VERSELF_SUPERVISOR = "nomad"
      }
      resources {
        cpu = 500
        memory = 256
      }
      restart {
        attempts = 3
        delay = "15s"
        interval = "300s"
        mode = "delay"
      }
      service {
        name = "notifications-service-public-http"
        port = "public_http"
        provider = "nomad"
        address_mode = "auto"
        check {
          name = "notifications-service-http-public_http"
          type = "http"
          path = "/readyz"
          port = "public_http"
          interval = "1s"
          timeout = "3s"
        }
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
