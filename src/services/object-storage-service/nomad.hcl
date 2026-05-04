job "object-storage-service" {
  name = "object-storage-service"
  datacenters = ["dc1"]
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
        OBJECT_STORAGE_GARAGE_REGION = "garage"
        OBJECT_STORAGE_GARAGE_S3_URLS = "http://127.0.0.1:3900,http://127.0.0.1:3910,http://127.0.0.1:3920"
        OBJECT_STORAGE_ROLE = "s3"
        OTEL_EXPORTER_OTLP_ENDPOINT = "http://127.0.0.1:4317"
        OTEL_RESOURCE_ATTRIBUTES = "verself.supervisor=nomad"
        OTEL_SERVICE_NAME = "object-storage-service-migration"
        SPIFFE_ENDPOINT_SOCKET = "unix:///run/spire-agent/sockets/agent.sock"
        VERSELF_CLICKHOUSE_ADDRESS = "127.0.0.1:9440"
        VERSELF_CLICKHOUSE_USER = "object_storage_service"
        VERSELF_CRED_CLICKHOUSE_CA_CERT = "/etc/credstore/object-storage-service/clickhouse-ca-cert"
        VERSELF_CRED_CREDENTIAL_KEK = "/etc/credstore/object-storage-service/credential-kek"
        VERSELF_CRED_GARAGE_PROXY_ACCESS_KEY_ID = "/etc/credstore/object-storage-service/garage-proxy-access-key-id"
        VERSELF_CRED_GARAGE_PROXY_SECRET_ACCESS_KEY = "/etc/credstore/object-storage-service/garage-proxy-secret-access-key"
        VERSELF_CRED_S3_TLS_CERT = "/etc/credstore/object-storage-service/s3-tls-cert"
        VERSELF_CRED_S3_TLS_KEY = "/etc/credstore/object-storage-service/s3-tls-key"
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
      artifact {
        source = "verself-artifact://object-storage-service"
        destination = "local"
        chown = true
      }
      config {
        command = "local/bin/object-storage-service"
      }
      env {
        OBJECT_STORAGE_GARAGE_REGION = "garage"
        OBJECT_STORAGE_GARAGE_S3_URLS = "http://127.0.0.1:3900,http://127.0.0.1:3910,http://127.0.0.1:3920"
        OBJECT_STORAGE_ROLE = "s3"
        OTEL_EXPORTER_OTLP_ENDPOINT = "http://127.0.0.1:4317"
        OTEL_RESOURCE_ATTRIBUTES = "verself.supervisor=nomad"
        OTEL_SERVICE_NAME = "object-storage-service"
        SPIFFE_ENDPOINT_SOCKET = "unix:///run/spire-agent/sockets/agent.sock"
        VERSELF_CLICKHOUSE_ADDRESS = "127.0.0.1:9440"
        VERSELF_CLICKHOUSE_USER = "object_storage_service"
        VERSELF_CRED_CLICKHOUSE_CA_CERT = "/etc/credstore/object-storage-service/clickhouse-ca-cert"
        VERSELF_CRED_CREDENTIAL_KEK = "/etc/credstore/object-storage-service/credential-kek"
        VERSELF_CRED_GARAGE_PROXY_ACCESS_KEY_ID = "/etc/credstore/object-storage-service/garage-proxy-access-key-id"
        VERSELF_CRED_GARAGE_PROXY_SECRET_ACCESS_KEY = "/etc/credstore/object-storage-service/garage-proxy-secret-access-key"
        VERSELF_CRED_S3_TLS_CERT = "/etc/credstore/object-storage-service/s3-tls-cert"
        VERSELF_CRED_S3_TLS_KEY = "/etc/credstore/object-storage-service/s3-tls-key"
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
      service {
        name = "object-storage-service-public-http"
        port = "public_http"
        provider = "nomad"
        address_mode = "auto"
        check {
          name = "object-storage-service-http-public_http"
          type = "http"
          path = "/healthz"
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
        OBJECT_STORAGE_GARAGE_ADMIN_URLS = "http://127.0.0.1:3903,http://127.0.0.1:3913,http://127.0.0.1:3923"
        OBJECT_STORAGE_GARAGE_REGION = "garage"
        OBJECT_STORAGE_ROLE = "admin"
        OTEL_EXPORTER_OTLP_ENDPOINT = "http://127.0.0.1:4317"
        OTEL_RESOURCE_ATTRIBUTES = "verself.supervisor=nomad"
        OTEL_SERVICE_NAME = "object-storage-admin"
        SPIFFE_ENDPOINT_SOCKET = "unix:///run/spire-agent/sockets/agent.sock"
        VERSELF_CRED_CREDENTIAL_KEK = "/etc/credstore/object-storage-service/credential-kek"
        VERSELF_CRED_GARAGE_ADMIN_TOKEN = "/etc/credstore/object-storage-service/garage-admin-token"
        VERSELF_CRED_GARAGE_PROXY_ACCESS_KEY_ID = "/etc/credstore/object-storage-service/garage-proxy-access-key-id"
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
      template {
        change_mode = "restart"
        destination = "secrets/upstreams.env"
        data = <<-EOT
OBJECT_STORAGE_GOVERNANCE_AUDIT_URL=https://{{- with nomadService "governance-service-internal-https" }}{{ with index . 0 }}{{ .Address }}:{{ .Port }}{{ end }}{{- else }}127.0.0.1:1{{- end }}
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
