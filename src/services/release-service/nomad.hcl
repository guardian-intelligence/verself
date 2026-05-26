job "release-service" {
  name = "release-service"
  datacenters = ["dc1"]
  type = "service"
  group "release-service" {
    count = 2
    network {
      mode = "host"
      port "service_https" {
        host_network = "loopback"
      }
      port "public_http" {
        host_network = "loopback"
      }
    }
    task "release-service-migrate" {
      driver = "raw_exec"
      user = "release_service"
      lifecycle {
        hook = "prestart"
        sidecar = false
      }
      artifact {
        source = "verself-artifact://release-service"
        destination = "local"
        chown = true
      }
      config {
        args = ["migrate", "up"]
        command = "local/bin/release-service"
      }
      env {
        OTEL_EXPORTER_OTLP_ENDPOINT = "http://127.0.0.1:4317"
        OTEL_RESOURCE_ATTRIBUTES = "verself.supervisor=nomad"
        OTEL_SERVICE_NAME = "release-service-migration"
        SPIFFE_ENDPOINT_SOCKET = "unix:///run/spire-agent/sockets/agent.sock"
        VERSELF_CLICKHOUSE_ADDRESS = "127.0.0.1:9440"
        VERSELF_CLICKHOUSE_USER = "release_service"
        VERSELF_CRED_CLICKHOUSE_CA_CERT = "/etc/credstore/release-service/clickhouse-ca-cert"
        VERSELF_INSTALLATION_ID = "inst_5NZSEA08R8P3HN566DNH8D301M"
        VERSELF_LISTEN_ADDR = "127.0.0.1:$${NOMAD_PORT_public_http}"
        VERSELF_PG_DSN = "postgres://release_service@/release_service?host=/var/run/postgresql&sslmode=disable"
        VERSELF_PG_MAX_CONNS = "8"
        VERSELF_SERVICE_LISTEN_ADDR = "127.0.0.1:$${NOMAD_PORT_service_https}"
        VERSELF_SUPERVISOR = "nomad"
      }
      resources {
        cpu = 100
        memory = 128
      }
    }
    task "release-service" {
      driver = "raw_exec"
      user = "release_service"
      kill_signal = "SIGTERM"
      kill_timeout = "30s"
      shutdown_delay = "5s"
      artifact {
        source = "verself-artifact://release-service"
        destination = "local"
        chown = true
      }
      config {
        command = "local/bin/release-service"
      }
      env {
        OTEL_EXPORTER_OTLP_ENDPOINT = "http://127.0.0.1:4317"
        OTEL_RESOURCE_ATTRIBUTES = "verself.supervisor=nomad"
        OTEL_SERVICE_NAME = "release-service"
        SPIFFE_ENDPOINT_SOCKET = "unix:///run/spire-agent/sockets/agent.sock"
        VERSELF_CLICKHOUSE_ADDRESS = "127.0.0.1:9440"
        VERSELF_CLICKHOUSE_USER = "release_service"
        VERSELF_CRED_CLICKHOUSE_CA_CERT = "/etc/credstore/release-service/clickhouse-ca-cert"
        VERSELF_INSTALLATION_ID = "inst_5NZSEA08R8P3HN566DNH8D301M"
        VERSELF_LISTEN_ADDR = "127.0.0.1:$${NOMAD_PORT_public_http}"
        VERSELF_PLATFORM_ORG_ID = "371564185181576922"
        VERSELF_PG_DSN = "postgres://release_service@/release_service?host=/var/run/postgresql&sslmode=disable"
        VERSELF_PG_MAX_CONNS = "8"
        VERSELF_RELEASE_SEED_MAKE_SKILL = "true"
        VERSELF_SERVICE_LISTEN_ADDR = "127.0.0.1:$${NOMAD_PORT_service_https}"
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
        name = "release-service-internal-https"
        port = "service_https"
        provider = "nomad"
        address_mode = "auto"
        check {
          name = "release-service-tcp-internal_https"
          type = "tcp"
          port = "service_https"
          interval = "1s"
          timeout = "3s"
        }
      }
      service {
        name = "release-service-public-http"
        port = "public_http"
        provider = "nomad"
        address_mode = "auto"
        check {
          name = "release-service-http-public_http"
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
