job "source-code-hosting-service" {
  name = "source-code-hosting-service"
  datacenters = ["*"]
  type = "service"
  group "source-code-hosting-service" {
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
    task "source-code-hosting-service-migrate" {
      driver = "raw_exec"
      user = "source_code_hosting_service"
      lifecycle {
        hook = "prestart"
        sidecar = false
      }
      artifact {
        source = "verself-artifact://source-code-hosting-service"
        destination = "local"
        chown = true
      }
      config {
        args = ["migrate", "up"]
        command = "local/bin/source-code-hosting-service"
      }
      env {
        OTEL_EXPORTER_OTLP_ENDPOINT = "http://127.0.0.1:4317"
        OTEL_RESOURCE_ATTRIBUTES = "verself.supervisor=nomad"
        OTEL_SERVICE_NAME = "source-code-hosting-service-migration"
        SOURCE_FORGEJO_OWNER = "forgejo-automation"
        SOURCE_PUBLIC_BASE_URL = "__VERSELF_FORGEJO_BASE_URL__"
        SPIFFE_ENDPOINT_SOCKET = "unix:///run/spire-agent/sockets/agent.sock"
        VERSELF_AUTH_ISSUER_URL = "__VERSELF_AUTH_ISSUER_URL__"
        VERSELF_CRED_AUTH_AUDIENCE = "/etc/credstore/source-code-hosting-service/auth-audience"
        VERSELF_CRED_WEBHOOK_SECRET = "/etc/credstore/source-code-hosting-service/webhook-secret"
        VERSELF_INSTALLATION_ID = "__VERSELF_INSTALLATION_ID__"
        VERSELF_INTERNAL_LISTEN_ADDR = "127.0.0.1:$${NOMAD_PORT_internal_https}"
        VERSELF_LISTEN_ADDR = "127.0.0.1:$${NOMAD_PORT_public_http}"
        VERSELF_PG_CONN_MAX_IDLE_SECONDS = "300"
        VERSELF_PG_CONN_MAX_LIFETIME_SECONDS = "1800"
        VERSELF_PG_DSN = "postgres://source_code_hosting_service@/source_code_hosting?host=/var/run/postgresql&sslmode=disable"
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
        destination = "secrets/forgejo.env"
        data = <<-EOT
SOURCE_FORGEJO_BASE_URL=http://{{- with nomadService "forgejo-http" }}{{ with index . 0 }}{{ .Address }}:{{ .Port }}{{ end }}{{- else }}127.0.0.1:1{{- end }}
EOT
        env = true
      }
    }
    task "source-code-hosting-service" {
      driver = "raw_exec"
      user = "source_code_hosting_service"
      kill_signal = "SIGTERM"
      kill_timeout = "30s"
      shutdown_delay = "5s"
      vault {
        role = "source-code-hosting-service-runtime"
      }
      identity {
        name = "vault_default"
        aud  = ["vault.io"]
        ttl  = "1h"
      }
      artifact {
        source = "verself-artifact://source-code-hosting-service"
        destination = "local"
        chown = true
      }
      config {
        command = "local/bin/source-code-hosting-service"
      }
      env {
        OTEL_EXPORTER_OTLP_ENDPOINT = "http://127.0.0.1:4317"
        OTEL_RESOURCE_ATTRIBUTES = "verself.supervisor=nomad"
        OTEL_SERVICE_NAME = "source-code-hosting-service"
        SOURCE_FORGEJO_OWNER = "forgejo-automation"
        SOURCE_PUBLIC_BASE_URL = "__VERSELF_FORGEJO_BASE_URL__"
        SPIFFE_ENDPOINT_SOCKET = "unix:///run/spire-agent/sockets/agent.sock"
        VERSELF_AUTH_ISSUER_URL = "__VERSELF_AUTH_ISSUER_URL__"
        VERSELF_CRED_AUTH_AUDIENCE = "/etc/credstore/source-code-hosting-service/auth-audience"
        VERSELF_CRED_WEBHOOK_SECRET = "/etc/credstore/source-code-hosting-service/webhook-secret"
        VERSELF_INSTALLATION_ID = "__VERSELF_INSTALLATION_ID__"
        VERSELF_INTERNAL_LISTEN_ADDR = "127.0.0.1:$${NOMAD_PORT_internal_https}"
        VERSELF_LISTEN_ADDR = "127.0.0.1:$${NOMAD_PORT_public_http}"
        VERSELF_PG_CONN_MAX_IDLE_SECONDS = "300"
        VERSELF_PG_CONN_MAX_LIFETIME_SECONDS = "1800"
        VERSELF_PG_DSN = "postgres://source_code_hosting_service@/source_code_hosting?host=/var/run/postgresql&sslmode=disable"
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
        name = "source-code-hosting-service-internal-https"
        port = "internal_https"
        provider = "nomad"
        address_mode = "auto"
        check {
          name = "source-code-hosting-service-tcp-internal_https"
          type = "tcp"
          port = "internal_https"
          interval = "1s"
          timeout = "3s"
        }
      }
      service {
        name = "source-code-hosting-service-public-http"
        port = "public_http"
        provider = "nomad"
        address_mode = "auto"
        check {
          name = "source-code-hosting-service-http-public_http"
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
SOURCE_FORGEJO_AUTOMATION_TOKEN={{ with secret "kv-runtime/data/secret/org/source-code-hosting-service.forgejo.automation_token" }}{{ .Data.data.value }}{{ end }}
EOT
        env = true
      }
      template {
        change_mode = "restart"
        destination = "secrets/upstreams.env"
        data = <<-EOT
SOURCE_FORGEJO_BASE_URL=http://{{- with nomadService "forgejo-http" }}{{ with index . 0 }}{{ .Address }}:{{ .Port }}{{ end }}{{- else }}127.0.0.1:1{{- end }}
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
