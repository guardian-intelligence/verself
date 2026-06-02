job "cloudflare-r2-control-plane" {
  name = "cloudflare-r2-control-plane"
  datacenters = ["*"]
  type = "service"

  group "cloudflare-r2-control-plane" {
    count = 1

    network {
      mode = "host"
    }

    task "cloudflare-r2-control-plane" {
      driver = "raw_exec"
      user = "nobody"
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
        command = "local/bin/cloudflare-r2-control-plane"
        args = [
          "--action=serve",
          "--site=__VERSELF_SITE__",
          "--account-id=__VERSELF_CLOUDFLARE_ACCOUNT_ID__",
          "--bucket=__VERSELF_NOMAD_ARTIFACT_BUCKET__",
          "--key-prefix=sha256",
          "--credential-source=env",
          "--parent-access-key-id-env=CLOUDFLARE_R2_PUBLISHER_TOKEN_ID",
          "--parent-secret-access-key-env=CLOUDFLARE_R2_PUBLISHER_SECRET_ACCESS_KEY",
          "--listen=127.0.0.1:18732",
          "--auth-token-env=VERSELF_R2_CONTROL_PLANE_TOKEN",
        ]
      }

      env {
        OTEL_EXPORTER_OTLP_ENDPOINT = "http://127.0.0.1:4317"
        OTEL_RESOURCE_ATTRIBUTES = "verself.supervisor=nomad"
        OTEL_SERVICE_NAME = "cloudflare-r2-control-plane"
        VERSELF_SUPERVISOR = "nomad"
      }

      template {
        change_mode = "restart"
        destination = "secrets/r2-control-plane.env"
        perms = "0600"
        env = true
        data = <<-EOT
CLOUDFLARE_R2_PUBLISHER_TOKEN_ID={{ with secret "kv-runtime/data/secret/org/cloudflare-r2-control-plane.publisher_token_id" }}{{ .Data.data.value | toJSON }}{{ end }}
CLOUDFLARE_R2_PUBLISHER_SECRET_ACCESS_KEY={{ with secret "kv-runtime/data/secret/org/cloudflare-r2-control-plane.publisher_secret_access_key" }}{{ .Data.data.value | toJSON }}{{ end }}
VERSELF_R2_CONTROL_PLANE_TOKEN={{ with secret "kv-runtime/data/secret/org/deployment-service.r2_control_plane_token" }}{{ .Data.data.value | toJSON }}{{ end }}
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
    }

    update {
      max_parallel = 1
      canary = 1
      health_check = "task_states"
      min_healthy_time = "3s"
      healthy_deadline = "120s"
      progress_deadline = "300s"
      auto_revert = true
      auto_promote = true
    }
  }
}
