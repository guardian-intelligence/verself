job "distribution-service" {
  name = "distribution-service"
  datacenters = ["*"]
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
        CREDENTIALS_DIRECTORY = "$${NOMAD_SECRETS_DIR}"
        DISTRIBUTION_TRUSTED_BUILDERS = "__VERSELF_DISTRIBUTION_TRUSTED_BUILDERS__"
        DISTRIBUTION_TRUSTED_SIGNERS = "https://github.com/guardian-intelligence/verself/.github/workflows/release.yml@refs/heads/main"
        OTEL_EXPORTER_OTLP_ENDPOINT = "http://127.0.0.1:4317"
        OTEL_RESOURCE_ATTRIBUTES = "verself.supervisor=nomad"
        OTEL_SERVICE_NAME = "distribution-service-migration"
        SPIFFE_ENDPOINT_SOCKET = "unix:///run/spire-agent/sockets/agent.sock"
        VERSELF_CLICKHOUSE_ADDRESS = "127.0.0.1:9440"
        VERSELF_CLICKHOUSE_USER = "distribution_service"
        VERSELF_CRED_CLICKHOUSE_CA_CERT = "/etc/verself/clickhouse/server-ca.pem"
        VERSELF_INSTALLATION_ID = "__VERSELF_INSTALLATION_ID__"
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

      vault {
        role = "distribution-service-runtime"
      }
      identity {
        name = "vault_default"
        aud  = ["vault.io"]
        ttl  = "1h"
      }
      # Deployment evidence trust ring: every version of the site's OpenBao
      # Transit deployment-signing public key, concatenated PEM. OpenBao is the
      # trust source; Nomad delivers it and restarts the task on key rotation.
      # The template blocks until openbao-up has created the key, which also
      # sequences bootstrap-from-zero without operator key export.
      template {
        change_mode = "restart"
        destination = "secrets/distribution-trusted-deploy-keys"
        perms = "0600"
        data = <<-EOT
{{ with secret "transit/keys/deployment-signing" }}{{ range $version, $key := .Data.keys }}{{ $key.public_key }}
{{ end }}{{ end }}
EOT
      }
      artifact {
        source = "verself-artifact://distribution-service"
        destination = "local"
        chown = true
      }
      config {
        command = "local/bin/distribution-service"
      }
      env {
        CREDENTIALS_DIRECTORY = "$${NOMAD_SECRETS_DIR}"
        DISTRIBUTION_TRUSTED_BUILDERS = "__VERSELF_DISTRIBUTION_TRUSTED_BUILDERS__"
        DISTRIBUTION_TRUSTED_SIGNERS = "https://github.com/guardian-intelligence/verself/.github/workflows/release.yml@refs/heads/main"
        OTEL_EXPORTER_OTLP_ENDPOINT = "http://127.0.0.1:4317"
        OTEL_RESOURCE_ATTRIBUTES = "verself.supervisor=nomad"
        OTEL_SERVICE_NAME = "distribution-service"
        SPIFFE_ENDPOINT_SOCKET = "unix:///run/spire-agent/sockets/agent.sock"
        VERSELF_CLICKHOUSE_ADDRESS = "127.0.0.1:9440"
        VERSELF_CLICKHOUSE_USER = "distribution_service"
        VERSELF_CRED_CLICKHOUSE_CA_CERT = "/etc/verself/clickhouse/server-ca.pem"
        VERSELF_INSTALLATION_ID = "__VERSELF_INSTALLATION_ID__"
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
}
