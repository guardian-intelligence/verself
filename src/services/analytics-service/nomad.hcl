job "analytics-service" {
  name = "analytics-service"
  datacenters = ["*"]
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
      driver = "podman"
      user = "analytics_service"
      kill_signal = "SIGTERM"
      kill_timeout = "30s"
      shutdown_delay = "5s"
      vault {
        role = "analytics-service-runtime"
      }

      identity {
        name = "vault_default"
        aud  = ["vault.io"]
        ttl  = "1h"
      }

      config {
        image = "verself-oci-archive://analytics-service-image"
        network_mode = "host"
        volumes = [
          "/etc/verself/clickhouse:/etc/verself/clickhouse:ro",
          "/run/spire-agent/sockets:/run/spire-agent/sockets:ro",
        ]
      }
      env {
        CREDENTIALS_DIRECTORY = "$${NOMAD_SECRETS_DIR}"
        ANALYTICS_ALLOWED_EVENT_PREFIXES = "build."
        ANALYTICS_DEFAULT_DATASET_ID = "build"
        ANALYTICS_DEFAULT_ENVIRONMENT = "prod"
        ANALYTICS_DEFAULT_ORG_ID = "org_B7HWGKW0SH7G4EXW9XT8TCT60C"
        ANALYTICS_DEFAULT_PROJECT_ID = "verself"
        ANALYTICS_GITHUB_ALLOWED_REPOSITORIES = "guardian-intelligence/verself"
        ANALYTICS_GITHUB_OIDC_AUDIENCE = "__VERSELF_ANALYTICS_GITHUB_OIDC_AUDIENCE__"
        OTEL_EXPORTER_OTLP_ENDPOINT = "http://127.0.0.1:4317"
        OTEL_RESOURCE_ATTRIBUTES = "verself.supervisor=nomad"
        OTEL_SERVICE_NAME = "analytics-service"
        SPIFFE_ENDPOINT_SOCKET = "unix:///run/spire-agent/sockets/agent.sock"
        VERSELF_AUTH_ISSUER_URL = "__VERSELF_AUTH_ISSUER_URL__"
        VERSELF_CLICKHOUSE_ADDRESS = "127.0.0.1:9440"
        VERSELF_CLICKHOUSE_USER = "analytics_service"
        VERSELF_CRED_CLICKHOUSE_CA_CERT = "/etc/verself/clickhouse/server-ca.pem"
        VERSELF_LISTEN_ADDR = "127.0.0.1:$${NOMAD_PORT_public_http}"
        VERSELF_SUPERVISOR = "nomad"
      }
      template {
        change_mode = "restart"
        destination = "secrets/auth-audience"
        perms = "0600"
        data = <<-EOT
{{ with secret "kv-runtime/data/secret/org/iam-service.zitadel.auth_audience" }}{{ .Data.data.value }}{{ end }}
EOT
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
