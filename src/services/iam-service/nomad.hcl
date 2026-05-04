job "iam-service" {
  name = "iam-service"
  datacenters = ["dc1"]
  type = "service"
  group "iam-service" {
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
    task "iam-service-migrate" {
      driver = "raw_exec"
      user = "iam_service"
      lifecycle {
        hook = "prestart"
        sidecar = false
      }
      artifact {
        source = "verself-artifact://iam-service"
        destination = "local"
        chown = true
      }
      config {
        args = ["migrate", "up"]
        command = "local/bin/iam-service"
      }
      env {
        IAM_BROWSER_AUTH_LOGIN_AUDIENCES = "370200928688586084,370200564807548260"
        IAM_BROWSER_AUTH_PUBLIC_BASE_URL = "https://verself.sh"
        IAM_ZITADEL_BASE_URL = "http://127.0.0.1:8085"
        IAM_ZITADEL_HOST = "auth.verself.sh"
        OTEL_EXPORTER_OTLP_ENDPOINT = "http://127.0.0.1:4317"
        OTEL_RESOURCE_ATTRIBUTES = "verself.supervisor=nomad"
        OTEL_SERVICE_NAME = "iam-service-migration"
        SPIFFE_ENDPOINT_SOCKET = "unix:///run/spire-agent/sockets/agent.sock"
        VERSELF_AUTH_AUDIENCE = "370200564807548260"
        VERSELF_AUTH_ISSUER_URL = "https://auth.verself.sh"
        VERSELF_CLICKHOUSE_ADDRESS = "127.0.0.1:9440"
        VERSELF_CLICKHOUSE_USER = "iam_service"
        VERSELF_CRED_CLICKHOUSE_CA_CERT = "/etc/credstore/iam-service/clickhouse-ca-cert"
        VERSELF_CRED_OIDC_CLIENT_ID = "/etc/credstore/iam-service/oidc-client-id"
        VERSELF_CRED_OIDC_CLIENT_SECRET = "/etc/credstore/iam-service/oidc-client-secret"
        VERSELF_CRED_SPICEDB_GRPC_PRESHARED_KEY = "/etc/credstore/iam-service/spicedb-grpc-preshared-key"
        VERSELF_CRED_ZITADEL_ACTION_SIGNING_KEY = "/etc/credstore/iam-service/zitadel-action-signing-key"
        VERSELF_CRED_ZITADEL_ADMIN_TOKEN = "/etc/credstore/iam-service/zitadel-admin-token"
        VERSELF_INTERNAL_LISTEN_ADDR = "127.0.0.1:$${NOMAD_PORT_internal_https}"
        VERSELF_LISTEN_ADDR = "127.0.0.1:$${NOMAD_PORT_public_http}"
        VERSELF_PG_CONN_MAX_IDLE_SECONDS = "300"
        VERSELF_PG_CONN_MAX_LIFETIME_SECONDS = "1800"
        VERSELF_PG_DSN = "postgres://iam_service@/iam_service?host=/var/run/postgresql&sslmode=disable"
        VERSELF_PG_MAX_CONNS = "8"
        VERSELF_PG_MIN_CONNS = "1"
        VERSELF_SUPERVISOR = "nomad"
      }
      resources {
        cpu = 100
        memory = 128
      }
    }
    task "iam-service" {
      driver = "raw_exec"
      user = "iam_service"
      kill_signal = "SIGTERM"
      kill_timeout = "30s"
      artifact {
        source = "verself-artifact://iam-service"
        destination = "local"
        chown = true
      }
      config {
        command = "local/bin/iam-service"
      }
      env {
        IAM_BROWSER_AUTH_LOGIN_AUDIENCES = "370200928688586084,370200564807548260"
        IAM_BROWSER_AUTH_PUBLIC_BASE_URL = "https://verself.sh"
        IAM_ZITADEL_BASE_URL = "http://127.0.0.1:8085"
        IAM_ZITADEL_HOST = "auth.verself.sh"
        OTEL_EXPORTER_OTLP_ENDPOINT = "http://127.0.0.1:4317"
        OTEL_RESOURCE_ATTRIBUTES = "verself.supervisor=nomad"
        OTEL_SERVICE_NAME = "iam-service"
        SPIFFE_ENDPOINT_SOCKET = "unix:///run/spire-agent/sockets/agent.sock"
        VERSELF_AUTH_AUDIENCE = "370200564807548260"
        VERSELF_AUTH_ISSUER_URL = "https://auth.verself.sh"
        VERSELF_CLICKHOUSE_ADDRESS = "127.0.0.1:9440"
        VERSELF_CLICKHOUSE_USER = "iam_service"
        VERSELF_CRED_CLICKHOUSE_CA_CERT = "/etc/credstore/iam-service/clickhouse-ca-cert"
        VERSELF_CRED_OIDC_CLIENT_ID = "/etc/credstore/iam-service/oidc-client-id"
        VERSELF_CRED_OIDC_CLIENT_SECRET = "/etc/credstore/iam-service/oidc-client-secret"
        VERSELF_CRED_SPICEDB_GRPC_PRESHARED_KEY = "/etc/credstore/iam-service/spicedb-grpc-preshared-key"
        VERSELF_CRED_ZITADEL_ACTION_SIGNING_KEY = "/etc/credstore/iam-service/zitadel-action-signing-key"
        VERSELF_CRED_ZITADEL_ADMIN_TOKEN = "/etc/credstore/iam-service/zitadel-admin-token"
        VERSELF_INTERNAL_LISTEN_ADDR = "127.0.0.1:$${NOMAD_PORT_internal_https}"
        VERSELF_LISTEN_ADDR = "127.0.0.1:$${NOMAD_PORT_public_http}"
        VERSELF_PG_CONN_MAX_IDLE_SECONDS = "300"
        VERSELF_PG_CONN_MAX_LIFETIME_SECONDS = "1800"
        VERSELF_PG_DSN = "postgres://iam_service@/iam_service?host=/var/run/postgresql&sslmode=disable"
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
        name = "iam-service-internal-https"
        port = "internal_https"
        provider = "nomad"
        address_mode = "auto"
        check {
          name = "iam-service-tcp-internal_https"
          type = "tcp"
          port = "internal_https"
          interval = "1s"
          timeout = "3s"
        }
      }
      service {
        name = "iam-service-public-http"
        port = "public_http"
        provider = "nomad"
        address_mode = "auto"
        check {
          name = "iam-service-http-public_http"
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
IAM_GOVERNANCE_AUDIT_URL=https://{{- with nomadService "governance-service-internal-https" }}{{ with index . 0 }}{{ .Address }}:{{ .Port }}{{ end }}{{- else }}127.0.0.1:1{{- end }}
IAM_SPICEDB_GRPC_ENDPOINT={{- with nomadService "spicedb-grpc" }}{{ with index . 0 }}{{ .Address }}:{{ .Port }}{{ end }}{{- else }}127.0.0.1:1{{- end }}
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
