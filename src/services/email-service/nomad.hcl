job "email-service" {
  name = "email-service"
  datacenters = ["*"]
  type = "service"
  group "email-service" {
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
    task "email-service-migrate" {
      driver = "raw_exec"
      user = "email_service"
      lifecycle {
        hook = "prestart"
        sidecar = false
      }
      artifact {
        source = "verself-artifact://email-service"
        destination = "local"
        chown = true
      }
      config {
        args = ["migrate", "up"]
        command = "local/bin/email-service"
      }
      env {
        EMAIL_SERVICE_RESEND_FROM_NAME = "verself"
        EMAIL_SERVICE_STALWART_PUBLIC_BASE_URL = "__VERSELF_STALWART_PUBLIC_BASE_URL__"
        EMAIL_SERVICE_SYNC_DISCOVERY_INTERVAL = "2m"
        EMAIL_SERVICE_SYNC_RECONCILE_INTERVAL = "10m"
        OTEL_EXPORTER_OTLP_ENDPOINT = "http://127.0.0.1:4317"
        OTEL_RESOURCE_ATTRIBUTES = "verself.supervisor=nomad"
        OTEL_SERVICE_NAME = "email-service-migration"
        SPIFFE_ENDPOINT_SOCKET = "unix:///run/spire-agent/sockets/agent.sock"
        VERSELF_AUTH_ISSUER_URL = "__VERSELF_AUTH_ISSUER_URL__"
        VERSELF_CRED_AUTH_AUDIENCE = "/etc/credstore/email-service/auth-audience"
        VERSELF_INTERNAL_LISTEN_ADDR = "127.0.0.1:$${NOMAD_PORT_internal_https}"
        VERSELF_LISTEN_ADDR = "127.0.0.1:$${NOMAD_PORT_public_http}"
        VERSELF_PG_CONN_MAX_IDLE_SECONDS = "300"
        VERSELF_PG_CONN_MAX_LIFETIME_SECONDS = "1800"
        VERSELF_PG_DSN = "postgres://email_service@/email_service?host=/var/run/postgresql&sslmode=disable"
        VERSELF_PG_MAX_CONNS = "8"
        VERSELF_PG_MIN_CONNS = "1"
        VERSELF_SUPERVISOR = "nomad"
      }
      resources {
        cpu = 100
        memory = 128
      }
      template {
        change_mode = "restart"
        destination = "secrets/stalwart.env"
        data = <<-EOT
EMAIL_SERVICE_STALWART_BASE_URL=http://{{- with nomadService "stalwart-http" }}{{ with index . 0 }}{{ .Address }}:{{ .Port }}{{ end }}{{- else }}127.0.0.1:1{{- end }}
EOT
        env = true
      }
    }
    task "email-service" {
      driver = "raw_exec"
      user = "email_service"
      kill_signal = "SIGTERM"
      kill_timeout = "30s"
      shutdown_delay = "5s"
      vault {
        role = "email-service-runtime"
      }
      identity {
        name = "vault_default"
        aud  = ["vault.io"]
        ttl  = "1h"
      }
      artifact {
        source = "verself-artifact://email-service"
        destination = "local"
        chown = true
      }
      config {
        command = "local/bin/email-service"
      }
      env {
        EMAIL_SERVICE_AGENT_SENDER_ADDRESS = "__VERSELF_AGENT_SENDER_ADDRESS__"
        EMAIL_SERVICE_AGENT_SENDER_NAME = "Verself Agents"
        EMAIL_SERVICE_AGENT_SENDER_ORG_ID = "org_B7HWGKW0SH7G4EXW9XT8TCT60C"
        EMAIL_SERVICE_RESEND_FROM_NAME = "verself"
        EMAIL_SERVICE_STALWART_PUBLIC_BASE_URL = "__VERSELF_STALWART_PUBLIC_BASE_URL__"
        EMAIL_SERVICE_SYNC_DISCOVERY_INTERVAL = "2m"
        EMAIL_SERVICE_SYNC_RECONCILE_INTERVAL = "10m"
        OTEL_EXPORTER_OTLP_ENDPOINT = "http://127.0.0.1:4317"
        OTEL_RESOURCE_ATTRIBUTES = "verself.supervisor=nomad"
        OTEL_SERVICE_NAME = "email-service"
        SPIFFE_ENDPOINT_SOCKET = "unix:///run/spire-agent/sockets/agent.sock"
        VERSELF_AUTH_ISSUER_URL = "__VERSELF_AUTH_ISSUER_URL__"
        VERSELF_CRED_AUTH_AUDIENCE = "/etc/credstore/email-service/auth-audience"
        VERSELF_INTERNAL_LISTEN_ADDR = "127.0.0.1:$${NOMAD_PORT_internal_https}"
        VERSELF_LISTEN_ADDR = "127.0.0.1:$${NOMAD_PORT_public_http}"
        VERSELF_PG_CONN_MAX_IDLE_SECONDS = "300"
        VERSELF_PG_CONN_MAX_LIFETIME_SECONDS = "1800"
        VERSELF_PG_DSN = "postgres://email_service@/email_service?host=/var/run/postgresql&sslmode=disable"
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
        name = "email-service-internal-https"
        port = "internal_https"
        provider = "nomad"
        address_mode = "auto"
        check {
          name = "email-service-tcp-internal_https"
          type = "tcp"
          port = "internal_https"
          interval = "1s"
          timeout = "3s"
        }
      }
      service {
        name = "email-service-public-http"
        port = "public_http"
        provider = "nomad"
        address_mode = "auto"
        check {
          name = "email-service-http-public_http"
          type = "http"
          path = "/readyz"
          port = "public_http"
          interval = "1s"
          timeout = "3s"
        }
      }
      template {
        change_mode = "restart"
        destination = "secrets/provider.env"
        data = <<-EOT
EMAIL_SERVICE_RESEND_API_KEY={{ with secret "kv-runtime/data/secret/org/email-service.resend.api_key" }}{{ .Data.data.value }}{{ end }}
EMAIL_SERVICE_STALWART_ADMIN_PASSWORD={{ with secret "kv-runtime/data/secret/org/email-service.stalwart.admin_password" }}{{ .Data.data.value }}{{ end }}
EOT
        env = true
      }
      template {
        change_mode = "restart"
        destination = "secrets/upstreams.env"
        data = <<-EOT
EMAIL_SERVICE_STALWART_BASE_URL=http://{{- with nomadService "stalwart-http" }}{{ with index . 0 }}{{ .Address }}:{{ .Port }}{{ end }}{{- else }}127.0.0.1:1{{- end }}
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
