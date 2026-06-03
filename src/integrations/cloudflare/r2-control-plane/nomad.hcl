job "cloudflare-r2-control-plane" {
  name = "cloudflare-r2-control-plane"
  datacenters = ["*"]
  type = "service"

  group "cloudflare-r2-control-plane" {
    count = 1

    network {
      mode = "host"
      port "internal_https" {
        host_network = "loopback"
      }
    }

    task "cloudflare-r2-control-plane" {
      driver = "raw_exec"
      kill_signal = "SIGTERM"
      kill_timeout = "30s"

      vault {
        role = "cloudflare-r2-control-plane-runtime"
      }

      identity {
        name = "vault_default"
        aud  = ["vault.io"]
        ttl  = "1h"
      }

      artifact {
        source = "verself-artifact://cloudflare-r2-control-plane"
        destination = "local"
        chown = true
      }

      config {
        command = "local/bin/nomad-chown-exec"
        args = [
          "--user=cloudflare_r2_control_plane",
          "--group=cloudflare_r2_control_plane",
          "--chown=$${NOMAD_SECRETS_DIR}/r2-publisher-token-id",
          "--chown=$${NOMAD_SECRETS_DIR}/r2-publisher-secret-access-key",
          "--",
          "local/bin/cloudflare-r2-control-plane",
          "--action=serve",
          "--site=__VERSELF_SITE__",
          "--account-id=__VERSELF_CLOUDFLARE_ACCOUNT_ID__",
          "--bucket=__VERSELF_NOMAD_ARTIFACT_BUCKET__",
          "--key-prefix=sha256",
          "--credential-source=files",
          "--parent-access-key-id-file=$${NOMAD_SECRETS_DIR}/r2-publisher-token-id",
          "--parent-secret-access-key-file=$${NOMAD_SECRETS_DIR}/r2-publisher-secret-access-key",
          "--listen=127.0.0.1:$${NOMAD_PORT_internal_https}",
        ]
      }

      env {
        OTEL_EXPORTER_OTLP_ENDPOINT = "http://127.0.0.1:4317"
        OTEL_RESOURCE_ATTRIBUTES = "verself.supervisor=nomad"
        OTEL_SERVICE_NAME = "cloudflare-r2-control-plane"
        SPIFFE_ENDPOINT_SOCKET = "unix:///run/spire-agent/sockets/agent.sock"
        VERSELF_SUPERVISOR = "nomad"
      }

      template {
        change_mode = "restart"
        destination = "secrets/r2-publisher-token-id"
        perms = "0600"
        data = <<-EOT
{{ with secret "kv-runtime/data/secret/org/cloudflare-r2-control-plane.publisher_token_id" }}{{ .Data.data.value }}{{ end }}
EOT
      }
      template {
        change_mode = "restart"
        destination = "secrets/r2-publisher-secret-access-key"
        perms = "0600"
        data = <<-EOT
{{ with secret "kv-runtime/data/secret/org/cloudflare-r2-control-plane.publisher_secret_access_key" }}{{ .Data.data.value }}{{ end }}
EOT
      }
      resources {
        cpu = 100
        memory = 128
      }

      restart {
        attempts = 3
        delay = "15s"
        interval = "300s"
        mode = "delay"
      }
      service {
        name = "cloudflare-r2-control-plane-internal-https"
        port = "internal_https"
        provider = "nomad"
        address_mode = "auto"
        check {
          name = "cloudflare-r2-control-plane-tcp-internal_https"
          type = "tcp"
          port = "internal_https"
          interval = "1s"
          timeout = "3s"
        }
      }
    }

    update {
      max_parallel = 1
      # This singleton binds a fixed host loopback port; canary rollout would start a second listener.
      canary = 0
      health_check = "task_states"
      min_healthy_time = "3s"
      healthy_deadline = "120s"
      progress_deadline = "300s"
      auto_revert = true
      auto_promote = false
    }
  }
}
