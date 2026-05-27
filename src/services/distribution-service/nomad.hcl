job "distribution-service" {
  name = "distribution-service"
  datacenters = ["dc1"]
  type = "service"
  group "distribution-service" {
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
    task "distribution-service-migrate" {
      driver = "raw_exec"
      user = "distribution_service"
      lifecycle {
        hook = "prestart"
        sidecar = false
      }
      artifact {
        source = "verself-artifact://distribution-service"
        destination = "local"
        chown = true
      }
      config {
        args = ["migrate", "up"]
        command = "local/bin/distribution-service"
      }
      env {
        DISTRIBUTION_TRUSTED_BUILDERS = "spiffe://prod.verself.sh/svc/release-builder,spiffe://prod.verself.sh/svc/distribution-service"
        DISTRIBUTION_TRUSTED_SIGNERS = "https://github.com/guardian-intelligence/verself/.github/workflows/release.yml@refs/heads/main"
        OTEL_EXPORTER_OTLP_ENDPOINT = "http://127.0.0.1:4317"
        OTEL_RESOURCE_ATTRIBUTES = "verself.supervisor=nomad"
        OTEL_SERVICE_NAME = "distribution-service-migration"
        SPIFFE_ENDPOINT_SOCKET = "unix:///run/spire-agent/sockets/agent.sock"
        VERSELF_AUTH_ISSUER_URL = "https://verself.sh"
        VERSELF_CLICKHOUSE_ADDRESS = "127.0.0.1:9440"
        VERSELF_CLICKHOUSE_USER = "distribution_service"
        VERSELF_CRED_AUTH_AUDIENCE = "/etc/credstore/distribution-service/auth-audience"
        VERSELF_CRED_CLICKHOUSE_CA_CERT = "/etc/credstore/distribution-service/clickhouse-ca-cert"
        VERSELF_INSTALLATION_ID = "inst_5NZSEA08R8P3HN566DNH8D301M"
        VERSELF_INTERNAL_LISTEN_ADDR = "127.0.0.1:$${NOMAD_PORT_internal_https}"
        VERSELF_LISTEN_ADDR = "127.0.0.1:$${NOMAD_PORT_public_http}"
        VERSELF_PG_DSN = "postgres://distribution_service@/distribution_service?host=/var/run/postgresql&sslmode=disable"
        VERSELF_PG_MAX_CONNS = "8"
        VERSELF_SUPERVISOR = "nomad"
      }
      resources {
        cpu = 100
        memory = 128
      }
    }
    task "distribution-service" {
      driver = "raw_exec"
      user = "distribution_service"
      kill_signal = "SIGTERM"
      kill_timeout = "30s"
      shutdown_delay = "5s"
      artifact {
        source = "verself-artifact://distribution-service"
        destination = "local"
        chown = true
      }
      config {
        command = "local/bin/distribution-service"
      }
      env {
        DISTRIBUTION_TRUSTED_BUILDERS = "spiffe://prod.verself.sh/svc/release-builder,spiffe://prod.verself.sh/svc/distribution-service"
        DISTRIBUTION_TRUSTED_SIGNERS = "https://github.com/guardian-intelligence/verself/.github/workflows/release.yml@refs/heads/main"
        OTEL_EXPORTER_OTLP_ENDPOINT = "http://127.0.0.1:4317"
        OTEL_RESOURCE_ATTRIBUTES = "verself.supervisor=nomad"
        OTEL_SERVICE_NAME = "distribution-service"
        SPIFFE_ENDPOINT_SOCKET = "unix:///run/spire-agent/sockets/agent.sock"
        VERSELF_AUTH_ISSUER_URL = "https://verself.sh"
        VERSELF_CLICKHOUSE_ADDRESS = "127.0.0.1:9440"
        VERSELF_CLICKHOUSE_USER = "distribution_service"
        VERSELF_CRED_AUTH_AUDIENCE = "/etc/credstore/distribution-service/auth-audience"
        VERSELF_CRED_CLICKHOUSE_CA_CERT = "/etc/credstore/distribution-service/clickhouse-ca-cert"
        VERSELF_INSTALLATION_ID = "inst_5NZSEA08R8P3HN566DNH8D301M"
        VERSELF_INTERNAL_LISTEN_ADDR = "127.0.0.1:$${NOMAD_PORT_internal_https}"
        VERSELF_LISTEN_ADDR = "127.0.0.1:$${NOMAD_PORT_public_http}"
        VERSELF_PG_DSN = "postgres://distribution_service@/distribution_service?host=/var/run/postgresql&sslmode=disable"
        VERSELF_PG_MAX_CONNS = "8"
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
        name = "distribution-service-internal-https"
        port = "internal_https"
        provider = "nomad"
        address_mode = "auto"
        check {
          name = "distribution-service-tcp-internal_https"
          type = "tcp"
          port = "internal_https"
          interval = "1s"
          timeout = "3s"
        }
      }
      service {
        name = "distribution-service-public-http"
        port = "public_http"
        provider = "nomad"
        address_mode = "auto"
        check {
          name = "distribution-service-http-public_http"
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
  group "distribution-release-worker" {
    count = 1
    task "distribution-release-dirs" {
      driver = "raw_exec"
      user = "root"
      lifecycle {
        hook = "prestart"
        sidecar = false
      }
      config {
        command = "/bin/sh"
        args = ["-ec", "install -d -o root -g root -m 0755 /artifacts\ninstall -d -o distribution_service -g distribution_service -m 0750 /artifacts/releases /artifacts/releases/work /artifacts/releases/mksk\n"]
      }
      resources {
        cpu = 50
        memory = 64
      }
    }
    task "distribution-release-worker" {
      driver = "raw_exec"
      user = "distribution_service"
      kill_signal = "SIGTERM"
      kill_timeout = "30s"
      artifact {
        source = "verself-artifact://distribution-release-worker"
        destination = "local"
        chown = true
      }
      config {
        command = "local/bin/distribution-release-worker"
      }
      env {
        DISTRIBUTION_RELEASE_ARTIFACT_ROOT = "/artifacts/releases"
        DISTRIBUTION_RELEASE_BINARY = "local/bin/distribution-release"
        DISTRIBUTION_RELEASE_BUILDER_ID = "spiffe://prod.verself.sh/svc/distribution-service"
        DISTRIBUTION_RELEASE_GIT_BINARY = "git"
        DISTRIBUTION_RELEASE_SOURCE_REPOSITORY = "https://github.com/guardian-intelligence/verself.git"
        DISTRIBUTION_RELEASE_TEMPORAL_NAMESPACE = "distribution-service"
        DISTRIBUTION_RELEASE_TEMPORAL_TASK_QUEUE = "distribution-service.release-v1"
        DISTRIBUTION_RELEASE_TOOLS_TAR = "local/share/distribution-release-tools.tar"
        DISTRIBUTION_RELEASE_WORK_ROOT = "/artifacts/releases/work"
        OTEL_EXPORTER_OTLP_ENDPOINT = "http://127.0.0.1:4317"
        OTEL_RESOURCE_ATTRIBUTES = "verself.supervisor=nomad"
        OTEL_SERVICE_NAME = "distribution-release-worker"
        SPIFFE_ENDPOINT_SOCKET = "unix:///run/spire-agent/sockets/agent.sock"
        VERSELF_SUPERVISOR = "nomad"
      }
      resources {
        cpu = 1000
        memory = 2048
      }
      restart {
        attempts = 3
        delay = "15s"
        interval = "300s"
        mode = "delay"
      }
      template {
        change_mode = "restart"
        destination = "secrets/upstreams.env"
        data = <<-EOT
VERSELF_TEMPORAL_FRONTEND_ADDRESS={{- with nomadService "temporal-frontend-grpc" }}{{ with index . 0 }}{{ .Address }}:{{ .Port }}{{ end }}{{- else }}127.0.0.1:1{{- end }}
EOT
        env = true
      }
    }
    update {
      max_parallel = 1
      health_check = "task_states"
      min_healthy_time = "1s"
      healthy_deadline = "60s"
      progress_deadline = "120s"
      canary = 0
      auto_revert = true
      auto_promote = false
    }
  }
}
