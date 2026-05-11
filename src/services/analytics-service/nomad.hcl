job "analytics-service" {
  name = "analytics-service"
  datacenters = ["dc1"]
  type = "service"
  group "analytics-service" {
    count = 2
    network {
      mode = "host"
      port "public_http" {
        host_network = "loopback"
      }
    }
    task "analytics-service" {
      driver = "raw_exec"
      user = "analytics_service"
      kill_signal = "SIGTERM"
      kill_timeout = "30s"
      shutdown_delay = "5s"
      artifact {
        source = "verself-artifact://analytics-service"
        destination = "local"
        chown = true
      }
      config {
        command = "local/bin/analytics-service"
      }
      env {
        ANALYTICS_ALLOWED_EVENT_PREFIXES = "build."
        ANALYTICS_GITHUB_ALLOWED_REPOSITORIES = "guardian-intelligence/verself"
        ANALYTICS_GITHUB_OIDC_AUDIENCE = "https://analytics.api.verself.sh"
        OTEL_EXPORTER_OTLP_ENDPOINT = "http://127.0.0.1:4317"
        OTEL_RESOURCE_ATTRIBUTES = "verself.supervisor=nomad"
        OTEL_SERVICE_NAME = "analytics-service"
        SPIFFE_ENDPOINT_SOCKET = "unix:///run/spire-agent/sockets/agent.sock"
        VERSELF_CLICKHOUSE_ADDRESS = "127.0.0.1:9440"
        VERSELF_CLICKHOUSE_USER = "analytics_service"
        VERSELF_CRED_CLICKHOUSE_CA_CERT = "/etc/credstore/analytics-service/clickhouse-ca-cert"
        VERSELF_LISTEN_ADDR = "127.0.0.1:$${NOMAD_PORT_public_http}"
        VERSELF_SUPERVISOR = "nomad"
      }
      resources {
        cpu = 250
        memory = 256
      }
      restart {
        attempts = 3
        delay = "15s"
        interval = "300s"
        mode = "delay"
      }
      service {
        name = "analytics-service-public-http"
        port = "public_http"
        provider = "nomad"
        address_mode = "auto"
        check {
          name = "analytics-service-http-public_http"
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
