job "mailbox-service" {
  name = "mailbox-service"
  datacenters = ["dc1"]
  type = "service"
  group "mailbox-service" {
    count = 2
    network {
      mode = "host"
      port "public_http" {
        host_network = "loopback"
      }
    }
    task "mailbox-service-migrate" {
      driver = "raw_exec"
      user = "mailbox_service"
      lifecycle {
        hook = "prestart"
        sidecar = false
      }
      artifact {
        source = "verself-artifact://mailbox-service"
        destination = "local"
        chown = true
      }
      config {
        args = ["migrate", "up"]
        command = "local/bin/mailbox-service"
      }
      env {
        MAILBOX_SERVICE_FORWARDER_FROM_ADDRESS = "noreply@notify.verself.sh"
        MAILBOX_SERVICE_FORWARDER_FROM_NAME = "verself"
        MAILBOX_SERVICE_FORWARDER_POLL_INTERVAL = "5s"
        MAILBOX_SERVICE_FORWARDER_STATE_PATH = "/var/lib/mailbox-service/forwarder-state.json"
        MAILBOX_SERVICE_STALWART_BASE_URL = "http://127.0.0.1:8090"
        MAILBOX_SERVICE_STALWART_LOCAL_DOMAIN = "verself.sh"
        MAILBOX_SERVICE_STALWART_MAILBOX = "ceo"
        MAILBOX_SERVICE_STALWART_PUBLIC_BASE_URL = "https://mail.verself.sh"
        MAILBOX_SERVICE_SYNC_DISCOVERY_INTERVAL = "2m"
        MAILBOX_SERVICE_SYNC_RECONCILE_INTERVAL = "10m"
        OTEL_EXPORTER_OTLP_ENDPOINT = "http://127.0.0.1:4317"
        OTEL_RESOURCE_ATTRIBUTES = "verself.supervisor=nomad"
        OTEL_SERVICE_NAME = "mailbox-service-migration"
        SPIFFE_ENDPOINT_SOCKET = "unix:///run/spire-agent/sockets/agent.sock"
        VERSELF_AUTH_ISSUER_URL = "https://auth.verself.sh"
        VERSELF_CRED_AUTH_AUDIENCE = "/etc/credstore/mailbox-service/auth-audience"
        VERSELF_CRED_FORWARD_TO = "/etc/credstore/mailbox-service/forward-to"
        VERSELF_CRED_STALWART_AGENTS_PASSWORD = "/etc/credstore/mailbox-service/stalwart-agents-password"
        VERSELF_CRED_STALWART_CEO_PASSWORD = "/etc/credstore/mailbox-service/stalwart-ceo-password"
        VERSELF_LISTEN_ADDR = "127.0.0.1:$${NOMAD_PORT_public_http}"
        VERSELF_PG_CONN_MAX_IDLE_SECONDS = "300"
        VERSELF_PG_CONN_MAX_LIFETIME_SECONDS = "1800"
        VERSELF_PG_DSN = "postgres://mailbox_service@/mailbox_service?host=/var/run/postgresql&sslmode=disable"
        VERSELF_PG_MAX_CONNS = "8"
        VERSELF_PG_MIN_CONNS = "1"
        VERSELF_SUPERVISOR = "nomad"
      }
      resources {
        cpu = 100
        memory = 128
      }
    }
    task "mailbox-service" {
      driver = "raw_exec"
      user = "mailbox_service"
      kill_signal = "SIGTERM"
      kill_timeout = "30s"
      artifact {
        source = "verself-artifact://mailbox-service"
        destination = "local"
        chown = true
      }
      config {
        command = "local/bin/mailbox-service"
      }
      env {
        MAILBOX_SERVICE_FORWARDER_FROM_ADDRESS = "noreply@notify.verself.sh"
        MAILBOX_SERVICE_FORWARDER_FROM_NAME = "verself"
        MAILBOX_SERVICE_FORWARDER_POLL_INTERVAL = "5s"
        MAILBOX_SERVICE_FORWARDER_STATE_PATH = "/var/lib/mailbox-service/forwarder-state.json"
        MAILBOX_SERVICE_STALWART_BASE_URL = "http://127.0.0.1:8090"
        MAILBOX_SERVICE_STALWART_LOCAL_DOMAIN = "verself.sh"
        MAILBOX_SERVICE_STALWART_MAILBOX = "ceo"
        MAILBOX_SERVICE_STALWART_PUBLIC_BASE_URL = "https://mail.verself.sh"
        MAILBOX_SERVICE_SYNC_DISCOVERY_INTERVAL = "2m"
        MAILBOX_SERVICE_SYNC_RECONCILE_INTERVAL = "10m"
        OTEL_EXPORTER_OTLP_ENDPOINT = "http://127.0.0.1:4317"
        OTEL_RESOURCE_ATTRIBUTES = "verself.supervisor=nomad"
        OTEL_SERVICE_NAME = "mailbox-service"
        SPIFFE_ENDPOINT_SOCKET = "unix:///run/spire-agent/sockets/agent.sock"
        VERSELF_AUTH_ISSUER_URL = "https://auth.verself.sh"
        VERSELF_CRED_AUTH_AUDIENCE = "/etc/credstore/mailbox-service/auth-audience"
        VERSELF_CRED_FORWARD_TO = "/etc/credstore/mailbox-service/forward-to"
        VERSELF_CRED_STALWART_AGENTS_PASSWORD = "/etc/credstore/mailbox-service/stalwart-agents-password"
        VERSELF_CRED_STALWART_CEO_PASSWORD = "/etc/credstore/mailbox-service/stalwart-ceo-password"
        VERSELF_LISTEN_ADDR = "127.0.0.1:$${NOMAD_PORT_public_http}"
        VERSELF_PG_CONN_MAX_IDLE_SECONDS = "300"
        VERSELF_PG_CONN_MAX_LIFETIME_SECONDS = "1800"
        VERSELF_PG_DSN = "postgres://mailbox_service@/mailbox_service?host=/var/run/postgresql&sslmode=disable"
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
        name = "mailbox-service-public-http"
        port = "public_http"
        provider = "nomad"
        address_mode = "auto"
        check {
          name = "mailbox-service-http-public_http"
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
MAILBOX_SERVICE_SECRETS_URL=https://{{- with nomadService "secrets-service-internal-https" }}{{ with index . 0 }}{{ .Address }}:{{ .Port }}{{ end }}{{- else }}127.0.0.1:1{{- end }}
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
