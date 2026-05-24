job "github-integration" {
  name = "github-integration"
  datacenters = ["dc1"]
  type = "service"
  group "github-integration-service" {
    count = 2
    network {
      mode = "host"
      port "public_http" {
        host_network = "loopback"
      }
    }
    task "github-integration-service-migrate" {
      driver = "raw_exec"
      user = "github_integration_service"
      lifecycle {
        hook = "prestart"
        sidecar = false
      }
      artifact {
        source = "verself-artifact://github-integration-service"
        destination = "local"
        chown = true
      }
      config {
        args = ["migrate", "up"]
        command = "local/bin/github-integration-service"
      }
      env {
        OTEL_EXPORTER_OTLP_ENDPOINT = "http://127.0.0.1:4317"
        OTEL_RESOURCE_ATTRIBUTES = "verself.supervisor=nomad"
        OTEL_SERVICE_NAME = "github-integration-service-migration"
        SPIFFE_ENDPOINT_SOCKET = "unix:///run/spire-agent/sockets/agent.sock"
        VERSELF_CLICKHOUSE_ADDRESS = "127.0.0.1:9440"
        VERSELF_CLICKHOUSE_USER = "github_integration_service"
        VERSELF_CRED_CLICKHOUSE_CA_CERT = "/etc/credstore/github-integration-service/clickhouse-ca-cert"
        VERSELF_LISTEN_ADDR = "127.0.0.1:$${NOMAD_PORT_public_http}"
        VERSELF_PG_CONN_MAX_IDLE_SECONDS = "300"
        VERSELF_PG_CONN_MAX_LIFETIME_SECONDS = "1800"
        VERSELF_PG_DSN = "postgres://github_integration_service@/github_integration_service?host=/var/run/postgresql&sslmode=disable"
        VERSELF_PG_MAX_CONNS = "8"
        VERSELF_PG_MIN_CONNS = "1"
        VERSELF_SUPERVISOR = "nomad"
      }
      resources {
        cpu = 100
        memory = 128
      }
    }
    task "github-integration-service" {
      driver = "raw_exec"
      user = "github_integration_service"
      kill_signal = "SIGTERM"
      kill_timeout = "30s"
      shutdown_delay = "5s"
      artifact {
        source = "verself-artifact://github-integration-service"
        destination = "local"
        chown = true
      }
      config {
        command = "local/bin/github-integration-service"
      }
      env {
        GITHUB_API_BASE_URL = "https://api.github.com"
        GITHUB_APP_ID = "3370540"
        GITHUB_APP_SETUP_URL = "https://github.com/apps/verself-runner/installations/new"
        GITHUB_APP_SLUG = "verself-runner"
        GITHUB_MAX_DELIVERY_TRIES = "8"
        GITHUB_OAUTH_AUTHORIZE_URL = "https://github.com/login/oauth/authorize"
        GITHUB_OAUTH_CLIENT_ID = "Iv23liDpxGOmBSQwSJ5i"
        GITHUB_OAUTH_REDIRECT_URL = "https://github.api.verself.sh/api/v1/github/user-authorizations/complete"
        GITHUB_OAUTH_TOKEN_URL = "https://github.com/login/oauth/access_token"
        GITHUB_RUNNER_CLASS_PREFIX = "verself-"
        GITHUB_RUNNER_GROUP_ID = "1"
        GITHUB_REPOSITORY_RUNNER_CLASS_ACTIVE_LIMIT = "15"
        GITHUB_WORKER_BATCH_SIZE = "16"
        GITHUB_WORKER_INTERVAL = "500ms"
        OTEL_EXPORTER_OTLP_ENDPOINT = "http://127.0.0.1:4317"
        OTEL_RESOURCE_ATTRIBUTES = "verself.supervisor=nomad"
        OTEL_SERVICE_NAME = "github-integration-service"
        SPIFFE_ENDPOINT_SOCKET = "unix:///run/spire-agent/sockets/agent.sock"
        VERSELF_AUTH_ISSUER_URL = "https://verself.sh"
        VERSELF_CLICKHOUSE_ADDRESS = "127.0.0.1:9440"
        VERSELF_CLICKHOUSE_USER = "github_integration_service"
        VERSELF_CRED_AUTH_AUDIENCE = "/etc/credstore/github-integration-service/auth-audience"
        VERSELF_CRED_CLICKHOUSE_CA_CERT = "/etc/credstore/github-integration-service/clickhouse-ca-cert"
        VERSELF_LISTEN_ADDR = "127.0.0.1:$${NOMAD_PORT_public_http}"
        VERSELF_PG_CONN_MAX_IDLE_SECONDS = "300"
        VERSELF_PG_CONN_MAX_LIFETIME_SECONDS = "1800"
        VERSELF_PG_DSN = "postgres://github_integration_service@/github_integration_service?host=/var/run/postgresql&sslmode=disable"
        VERSELF_PG_MAX_CONNS = "8"
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
        name = "github-integration-public-http"
        port = "public_http"
        provider = "nomad"
        address_mode = "auto"
        check {
          name = "github-integration-http-public_http"
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
