variable "guardian_repo_root" {
  type    = string
  default = "/home/ubuntu/.local/state/guardian/repo/current"
}

variable "source_code_hosting_service_resource_name" {
  type    = string
  default = "source-code-hosting-service"
}

variable "source_code_hosting_service_runtime_root" {
  type    = string
  default = "/var/lib/source-code-hosting-service/runtime"
}

variable "source_code_hosting_service_projected_graph" {
  type    = string
  default = "/run/verself/recovery/source-code-hosting-service/document.json"
}

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

    task "setup" {
      driver = "raw_exec"
      user = "root"

      lifecycle {
        hook = "prestart"
        sidecar = false
      }

      config {
        command = "${var.guardian_repo_root}/bazel-bin/src/services/source-code-hosting-service/cmd/source-code-hosting-service/source-code-hosting-service_/source-code-hosting-service"
        args = [
          "recover",
          "--repo-root=${var.guardian_repo_root}",
          "--resource-graph=${var.guardian_repo_root}/workspace/.guardian/fly/document.json",
          "--resource-name=${var.source_code_hosting_service_resource_name}",
          "--runtime-root=${var.source_code_hosting_service_runtime_root}",
          "--projected-graph=${var.source_code_hosting_service_projected_graph}",
          "--migrate",
        ]
      }

      resources {
        cpu = 100
        memory = 128
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

      config {
        command = "${var.source_code_hosting_service_runtime_root}/current/bin/source-code-hosting-service"
        args = [
          "--resource-graph=${var.source_code_hosting_service_projected_graph}",
          "--resource-name=${var.source_code_hosting_service_resource_name}",
          "--listen-addr=127.0.0.1:$${NOMAD_PORT_public_http}",
          "--internal-listen-addr=127.0.0.1:$${NOMAD_PORT_internal_https}",
        ]
      }
      env {
        CREDENTIALS_DIRECTORY = "$${NOMAD_SECRETS_DIR}"
        OTEL_EXPORTER_OTLP_ENDPOINT = "http://127.0.0.1:4317"
        OTEL_RESOURCE_ATTRIBUTES = "verself.supervisor=nomad"
        OTEL_SERVICE_NAME = "source-code-hosting-service"
        HOME = "/var/lib/source-code-hosting-service/home"
        TMPDIR = "/var/lib/source-code-hosting-service/home/tmp"
        VERSELF_SUPERVISOR = "nomad"
      }
      template {
        change_mode = "restart"
        destination = "secrets/iam-service.zitadel.auth_audience"
        perms = "0600"
        uid = "974"
        gid = "967"
        data = <<-EOT
{{ with secret "kv-runtime/data/secret/org/iam-service.zitadel.auth_audience" }}{{ .Data.data.value }}{{ end }}
EOT
      }
      template {
        change_mode = "restart"
        destination = "secrets/source-code-hosting-service.forgejo.automation_token"
        perms = "0600"
        uid = "974"
        gid = "967"
        data = <<-EOT
{{ with secret "kv-runtime/data/secret/org/source-code-hosting-service.forgejo.automation_token" }}{{ .Data.data.value }}{{ end }}
EOT
      }
      template {
        change_mode = "restart"
        destination = "secrets/source-code-hosting-service.forgejo.webhook_secret"
        perms = "0600"
        uid = "974"
        gid = "967"
        data = <<-EOT
{{ with secret "kv-runtime/data/secret/org/source-code-hosting-service.forgejo.webhook_secret" }}{{ .Data.data.value }}{{ end }}
EOT
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
