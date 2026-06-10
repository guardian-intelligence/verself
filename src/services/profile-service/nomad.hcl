job "profile-service" {
  name = "profile-service"
  datacenters = ["*"]
  type = "service"
  group "profile-service" {
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
    task "profile-service-migrate" {
      driver = "podman"
      user = "profile_service"
      lifecycle {
        hook = "prestart"
        sidecar = false
      }
      config {
        image = "verself-oci://profile-service"
        # No command: podman prepends the image entrypoint, so these args
        # become `profile-service migrate up`.
        args = ["migrate", "up"]
        network_mode = "host"
        volumes = [
          "/var/run/postgresql:/var/run/postgresql",
          "/run/spire-agent/sockets/agent.sock:/run/spire-agent.sock:ro",
        ]
      }
      env {
        CREDENTIALS_DIRECTORY = "$${NOMAD_SECRETS_DIR}"
        OTEL_EXPORTER_OTLP_ENDPOINT = "http://127.0.0.1:4317"
        OTEL_RESOURCE_ATTRIBUTES = "verself.supervisor=nomad"
        OTEL_SERVICE_NAME = "profile-service-migration"
        SPIFFE_ENDPOINT_SOCKET = "unix:///run/spire-agent.sock"
        VERSELF_AUTH_ISSUER_URL = "__VERSELF_AUTH_ISSUER_URL__"
        VERSELF_PRODUCT_API_AUTH_AUDIENCE = "__VERSELF_ZITADEL_PRODUCT_PROJECT_ID__"
        VERSELF_INTERNAL_LISTEN_ADDR = "127.0.0.1:$${NOMAD_PORT_internal_https}"
        VERSELF_LISTEN_ADDR = "127.0.0.1:$${NOMAD_PORT_public_http}"
        VERSELF_PG_CONN_MAX_IDLE_SECONDS = "300"
        VERSELF_PG_CONN_MAX_LIFETIME_SECONDS = "1800"
        VERSELF_PG_DSN = "postgres://profile_service@/profile?host=/var/run/postgresql&sslmode=disable"
        VERSELF_PG_MAX_CONNS = "8"
        VERSELF_PG_MIN_CONNS = "1"
        VERSELF_SUPERVISOR = "nomad"
      }
      resources {
        cpu = 100
        memory = 128
      }
    }
    task "profile-service" {
      driver = "podman"
      user = "profile_service"
      kill_signal = "SIGTERM"
      kill_timeout = "30s"
      shutdown_delay = "5s"
      config {
        image = "verself-oci://profile-service"
        network_mode = "host"
        volumes = [
          "/var/run/postgresql:/var/run/postgresql",
          "/run/spire-agent/sockets/agent.sock:/run/spire-agent.sock:ro",
        ]
      }
      env {
        CREDENTIALS_DIRECTORY = "$${NOMAD_SECRETS_DIR}"
        OTEL_EXPORTER_OTLP_ENDPOINT = "http://127.0.0.1:4317"
        OTEL_RESOURCE_ATTRIBUTES = "verself.supervisor=nomad"
        OTEL_SERVICE_NAME = "profile-service"
        SPIFFE_ENDPOINT_SOCKET = "unix:///run/spire-agent.sock"
        VERSELF_AUTH_ISSUER_URL = "__VERSELF_AUTH_ISSUER_URL__"
        VERSELF_PRODUCT_API_AUTH_AUDIENCE = "__VERSELF_ZITADEL_PRODUCT_PROJECT_ID__"
        VERSELF_INTERNAL_LISTEN_ADDR = "127.0.0.1:$${NOMAD_PORT_internal_https}"
        VERSELF_LISTEN_ADDR = "127.0.0.1:$${NOMAD_PORT_public_http}"
        VERSELF_PG_CONN_MAX_IDLE_SECONDS = "300"
        VERSELF_PG_CONN_MAX_LIFETIME_SECONDS = "1800"
        VERSELF_PG_DSN = "postgres://profile_service@/profile?host=/var/run/postgresql&sslmode=disable"
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
        name = "profile-service-internal-https"
        port = "internal_https"
        provider = "nomad"
        address_mode = "auto"
        check {
          name = "profile-service-tcp-internal_https"
          type = "tcp"
          port = "internal_https"
          interval = "1s"
          timeout = "3s"
        }
      }
      service {
        name = "profile-service-public-http"
        port = "public_http"
        provider = "nomad"
        address_mode = "auto"
        check {
          name = "profile-service-http-public_http"
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
