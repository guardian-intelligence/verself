job "iam-service" {
  name = "iam-service"
  datacenters = ["*"]
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
        IAM_BROWSER_AUTH_PUBLIC_BASE_URL = "__VERSELF_PRODUCT_BASE_URL__"
        IAM_HIBP_PWNED_PASSWORDS_RANGE_ENDPOINT = "https://api.pwnedpasswords.com"
        IAM_ZITADEL_HOST = "__VERSELF_PRODUCT_DOMAIN__"
        OTEL_EXPORTER_OTLP_ENDPOINT = "http://127.0.0.1:4317"
        OTEL_RESOURCE_ATTRIBUTES = "verself.supervisor=nomad"
        OTEL_SERVICE_NAME = "iam-service-migration"
        SPIFFE_ENDPOINT_SOCKET = "unix:///run/spire-agent/sockets/agent.sock"
        VERSELF_AUTH_ISSUER_URL = "__VERSELF_AUTH_ISSUER_URL__"
        VERSELF_CLICKHOUSE_ADDRESS = "127.0.0.1:9440"
        VERSELF_CLICKHOUSE_USER = "iam_service"
        VERSELF_CRED_CLICKHOUSE_CA_CERT = "/etc/verself/clickhouse/server-ca.pem"
        VERSELF_INSTALLATION_ID = "__VERSELF_INSTALLATION_ID__"
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
      template {
        change_mode = "restart"
        destination = "secrets/zitadel.env"
        data = <<-EOT
IAM_ZITADEL_BASE_URL=http://{{- with nomadService "zitadel-http" }}{{ with index . 0 }}{{ .Address }}:{{ .Port }}{{ end }}{{- else }}127.0.0.1:1{{- end }}
EOT
        env = true
      }
    }
    task "iam-service" {
      driver = "raw_exec"
      user = "iam_service"
      kill_signal = "SIGTERM"
      kill_timeout = "30s"
      shutdown_delay = "5s"
      vault {
        role = "iam-service-runtime"
      }
      identity {
        name = "vault_default"
        aud  = ["vault.io"]
        ttl  = "1h"
      }
      artifact {
        source = "verself-artifact://iam-service"
        destination = "local"
        chown = true
      }
      config {
        command = "local/bin/iam-service"
      }
      env {
        IAM_BROWSER_AUTH_PUBLIC_BASE_URL = "__VERSELF_PRODUCT_BASE_URL__"
        IAM_HIBP_PWNED_PASSWORDS_RANGE_ENDPOINT = "https://api.pwnedpasswords.com"
        IAM_ZITADEL_HOST = "__VERSELF_PRODUCT_DOMAIN__"
        OTEL_EXPORTER_OTLP_ENDPOINT = "http://127.0.0.1:4317"
        OTEL_RESOURCE_ATTRIBUTES = "verself.supervisor=nomad"
        OTEL_SERVICE_NAME = "iam-service"
        SPIFFE_ENDPOINT_SOCKET = "unix:///run/spire-agent/sockets/agent.sock"
        VERSELF_AUTH_ISSUER_URL = "__VERSELF_AUTH_ISSUER_URL__"
        VERSELF_CLICKHOUSE_ADDRESS = "127.0.0.1:9440"
        VERSELF_CLICKHOUSE_USER = "iam_service"
        VERSELF_CRED_CLICKHOUSE_CA_CERT = "/etc/verself/clickhouse/server-ca.pem"
        VERSELF_INSTALLATION_ID = "__VERSELF_INSTALLATION_ID__"
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
        destination = "secrets/provider.env"
        data = <<-EOT
IAM_EMAIL_IDENTITY_HMAC_KEY={{ with secret "kv-runtime/data/secret/org/iam-service.email_identity.hmac_key" }}{{ .Data.data.value }}{{ end }}
IAM_SPICEDB_GRPC_PRESHARED_KEY={{ with secret "kv-runtime/data/secret/org/iam-service.spicedb.grpc_preshared_key" }}{{ .Data.data.value }}{{ end }}
IAM_ZITADEL_ADMIN_TOKEN={{ with secret "kv-runtime/data/secret/org/iam-service.zitadel.admin_token" }}{{ .Data.data.value }}{{ end }}
VERSELF_CRED_VALUE_AUTH_AUDIENCE={{ with secret "kv-runtime/data/secret/org/iam-service.zitadel.auth_audience" }}{{ .Data.data.value }}{{ end }}
VERSELF_CRED_VALUE_GITHUB_LOGIN_IDP_ID={{ with secret "kv-runtime/data/secret/org/iam-service.zitadel.github_login_idp_id" }}{{ .Data.data.value }}{{ end }}
VERSELF_CRED_VALUE_OIDC_CLIENT_ID={{ with secret "kv-runtime/data/secret/org/iam-service.zitadel.oidc_client_id" }}{{ .Data.data.value }}{{ end }}
VERSELF_CRED_VALUE_OIDC_CLIENT_SECRET={{ with secret "kv-runtime/data/secret/org/iam-service.zitadel.oidc_client_secret" }}{{ .Data.data.value }}{{ end }}
VERSELF_CRED_VALUE_ZITADEL_ACTION_SIGNING_KEY={{ with secret "kv-runtime/data/secret/org/iam-service.zitadel.action_signing_key" }}{{ .Data.data.value }}{{ end }}
EOT
        env = true
      }
      template {
        change_mode = "restart"
        destination = "secrets/upstreams.env"
        data = <<-EOT
IAM_ZITADEL_BASE_URL=http://{{- with nomadService "zitadel-http" }}{{ with index . 0 }}{{ .Address }}:{{ .Port }}{{ end }}{{- else }}127.0.0.1:1{{- end }}
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
