job "object-storage-service" {
  name = "object-storage-service"
  datacenters = ["*"]
  type = "service"
  group "object-storage-service" {
    count = 2
    network {
      mode = "host"
      port "public_http" {
        host_network = "loopback"
      }
    }
    task "object-storage-service-migrate" {
      driver = "raw_exec"
      user = "object_storage_service"
      lifecycle {
        hook = "prestart"
        sidecar = false
      }
      artifact {
        source = "verself-artifact://object-storage-service"
        destination = "local"
        chown = true
      }
      config {
        args = ["migrate", "up"]
        command = "local/bin/object-storage-service"
      }
      env {
        OBJECT_STORAGE_PROVIDER = "cloudflare_r2"
        OBJECT_STORAGE_ROLE = "s3"
        OBJECT_STORAGE_S3_REGION = "auto"
        OTEL_EXPORTER_OTLP_ENDPOINT = "http://127.0.0.1:4317"
        OTEL_RESOURCE_ATTRIBUTES = "verself.supervisor=nomad"
        OTEL_SERVICE_NAME = "object-storage-service-migration"
        SPIFFE_ENDPOINT_SOCKET = "unix:///run/spire-agent/sockets/agent.sock"
        VERSELF_CLICKHOUSE_ADDRESS = "127.0.0.1:9440"
        VERSELF_CLICKHOUSE_USER = "object_storage_service"
        VERSELF_CRED_CLICKHOUSE_CA_CERT = "/etc/verself/clickhouse/server-ca.pem"
        VERSELF_LISTEN_ADDR = "127.0.0.1:$${NOMAD_PORT_public_http}"
        VERSELF_PG_CONN_MAX_IDLE_SECONDS = "300"
        VERSELF_PG_CONN_MAX_LIFETIME_SECONDS = "1800"
        VERSELF_PG_DSN = "postgres://object_storage_service@/object_storage_service?host=/var/run/postgresql&sslmode=disable"
        VERSELF_PG_MAX_CONNS = "12"
        VERSELF_PG_MIN_CONNS = "1"
        VERSELF_SUPERVISOR = "nomad"
      }
      resources {
        cpu = 100
        memory = 128
      }
    }
    task "object-storage-service" {
      driver = "raw_exec"
      user = "object_storage_service"
      kill_signal = "SIGTERM"
      kill_timeout = "30s"
      shutdown_delay = "5s"
      vault {
        role = "object-storage-service-runtime"
      }
      identity {
        name = "vault_default"
        aud  = ["vault.io"]
        ttl  = "1h"
      }
      artifact {
        source = "verself-artifact://object-storage-service"
        destination = "local"
        chown = true
      }
      config {
        command = "local/bin/object-storage-service"
      }
      env {
        CREDENTIALS_DIRECTORY = "secrets"
        OBJECT_STORAGE_PROVIDER = "cloudflare_r2"
        OBJECT_STORAGE_ROLE = "s3"
        OBJECT_STORAGE_S3_REGION = "auto"
        OTEL_EXPORTER_OTLP_ENDPOINT = "http://127.0.0.1:4317"
        OTEL_RESOURCE_ATTRIBUTES = "verself.supervisor=nomad"
        OTEL_SERVICE_NAME = "object-storage-service"
        SPIFFE_ENDPOINT_SOCKET = "unix:///run/spire-agent/sockets/agent.sock"
        VERSELF_CLICKHOUSE_ADDRESS = "127.0.0.1:9440"
        VERSELF_CLICKHOUSE_USER = "object_storage_service"
        VERSELF_CRED_CLICKHOUSE_CA_CERT = "/etc/verself/clickhouse/server-ca.pem"
        VERSELF_LISTEN_ADDR = "127.0.0.1:$${NOMAD_PORT_public_http}"
        VERSELF_PG_CONN_MAX_IDLE_SECONDS = "300"
        VERSELF_PG_CONN_MAX_LIFETIME_SECONDS = "1800"
        VERSELF_PG_DSN = "postgres://object_storage_service@/object_storage_service?host=/var/run/postgresql&sslmode=disable"
        VERSELF_PG_MAX_CONNS = "12"
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
      template {
        change_mode = "restart"
        destination = "secrets/r2.env"
        perms = "0600"
        data = <<-EOT
OBJECT_STORAGE_S3_URLS=__VERSELF_CLOUDFLARE_R2_ENDPOINT__
VERSELF_CRED_VALUE_CREDENTIAL_KEK={{ with secret "kv-runtime/data/secret/org/object-storage-service.credential_kek" }}{{ .Data.data.value | toJSON }}{{ end }}
VERSELF_CRED_VALUE_R2_PROXY_ACCESS_KEY_ID={{ with secret "kv-runtime/data/secret/org/object-storage-service.r2.proxy_access_key_id" }}{{ .Data.data.value | toJSON }}{{ end }}
VERSELF_CRED_VALUE_R2_PROXY_SECRET_ACCESS_KEY={{ with secret "kv-runtime/data/secret/org/object-storage-service.r2.proxy_secret_access_key" }}{{ .Data.data.value | toJSON }}{{ end }}
EOT
        env = true
      }
      service {
        name = "object-storage-service-public-http"
        port = "public_http"
        provider = "nomad"
        address_mode = "auto"
        check {
          name = "object-storage-service-http-public_http"
          type = "http"
          path = "/healthz"
          protocol = "https"
          tls_skip_verify = true
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
  group "object-storage-admin" {
    count = 1
    network {
      mode = "host"
      port "admin_http" {
        host_network = "loopback"
      }
    }
    task "object-storage-admin" {
      driver = "raw_exec"
      user = "object_storage_admin"
      kill_signal = "SIGTERM"
      kill_timeout = "30s"
      shutdown_delay = "5s"
      vault {
        role = "object-storage-service-runtime"
      }
      identity {
        name = "vault_default"
        aud  = ["vault.io"]
        ttl  = "1h"
      }
      artifact {
        source = "verself-artifact://object-storage-service"
        destination = "local"
        chown = true
      }
      config {
        command = "local/bin/object-storage-service"
      }
      env {
        OBJECT_STORAGE_ADMIN_LISTEN_ADDR = "127.0.0.1:$${NOMAD_PORT_admin_http}"
        CREDENTIALS_DIRECTORY = "secrets"
        OBJECT_STORAGE_PROVIDER = "cloudflare_r2"
        OBJECT_STORAGE_R2_ENDPOINT = "__VERSELF_CLOUDFLARE_R2_ENDPOINT__"
        OBJECT_STORAGE_ROLE = "admin"
        OBJECT_STORAGE_S3_REGION = "auto"
        OTEL_EXPORTER_OTLP_ENDPOINT = "http://127.0.0.1:4317"
        OTEL_RESOURCE_ATTRIBUTES = "verself.supervisor=nomad"
        OTEL_SERVICE_NAME = "object-storage-admin"
        SPIFFE_ENDPOINT_SOCKET = "unix:///run/spire-agent/sockets/agent.sock"
        VERSELF_AUTH_ISSUER_URL = "__VERSELF_AUTH_ISSUER_URL__"
        VERSELF_PG_CONN_MAX_IDLE_SECONDS = "300"
        VERSELF_PG_CONN_MAX_LIFETIME_SECONDS = "1800"
        VERSELF_PG_DSN = "postgres://object_storage_service@/object_storage_service?host=/var/run/postgresql&sslmode=disable"
        VERSELF_PG_MAX_CONNS = "12"
        VERSELF_PG_MIN_CONNS = "1"
        VERSELF_SUPERVISOR = "nomad"
      }
      template {
        change_mode = "restart"
        destination = "secrets/auth-audience.env"
        perms = "0600"
        env = true
        data = <<-EOT
VERSELF_CRED_VALUE_AUTH_AUDIENCE={{ with secret "kv-runtime/data/secret/org/iam-service.zitadel.auth_audience" }}{{ .Data.data.value | toJSON }}{{ end }}
VERSELF_CRED_VALUE_CREDENTIAL_KEK={{ with secret "kv-runtime/data/secret/org/object-storage-service.credential_kek" }}{{ .Data.data.value | toJSON }}{{ end }}
VERSELF_CRED_VALUE_R2_ADMIN_ACCESS_KEY_ID={{ with secret "kv-runtime/data/secret/org/object-storage-service.r2.admin_access_key_id" }}{{ .Data.data.value | toJSON }}{{ end }}
VERSELF_CRED_VALUE_R2_ADMIN_SECRET_ACCESS_KEY={{ with secret "kv-runtime/data/secret/org/object-storage-service.r2.admin_secret_access_key" }}{{ .Data.data.value | toJSON }}{{ end }}
EOT
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
        name = "object-storage-service-admin-http"
        port = "admin_http"
        provider = "nomad"
        address_mode = "auto"
        check {
          name = "object-storage-admin-tcp-admin_http"
          type = "tcp"
          port = "admin_http"
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
