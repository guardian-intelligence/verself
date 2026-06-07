variable "guardian_repo_root" {
  type    = string
  default = "/home/ubuntu/.local/state/guardian/repo/current"
}

variable "secrets_service_resource_name" {
  type    = string
  default = "secrets-service"
}

variable "secrets_service_runtime_root" {
  type    = string
  default = "/var/lib/secrets-service/runtime"
}

variable "secrets_service_projected_graph" {
  type    = string
  default = "/run/verself/recovery/secrets-service/document.json"
}

job "secrets-service" {
  name = "secrets-service"
  datacenters = ["*"]
  type = "service"
  group "secrets-service" {
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
        command = "${var.guardian_repo_root}/bazel-bin/src/services/secrets-service/cmd/secrets-service/secrets-service_/secrets-service"
        args = [
          "recover",
          "--repo-root=${var.guardian_repo_root}",
          "--resource-graph=${var.guardian_repo_root}/workspace/.guardian/fly/document.json",
          "--resource-name=${var.secrets_service_resource_name}",
          "--runtime-root=${var.secrets_service_runtime_root}",
          "--projected-graph=${var.secrets_service_projected_graph}",
        ]
      }

      resources {
        cpu = 50
        memory = 64
      }
    }

    task "secrets-service" {
      driver = "raw_exec"
      user = "secrets_service"
      kill_signal = "SIGTERM"
      kill_timeout = "30s"
      shutdown_delay = "5s"
      vault {
        role = "secrets-service-runtime"
      }

      identity {
        name = "vault_default"
        aud  = ["vault.io"]
        ttl  = "1h"
      }

      config {
        command = "${var.secrets_service_runtime_root}/current/bin/secrets-service"
        args = [
          "--resource-graph=${var.secrets_service_projected_graph}",
          "--resource-name=${var.secrets_service_resource_name}",
          "--listen-addr=127.0.0.1:$${NOMAD_PORT_public_http}",
          "--internal-listen-addr=127.0.0.1:$${NOMAD_PORT_internal_https}",
        ]
      }
      env {
        CREDENTIALS_DIRECTORY = "$${NOMAD_SECRETS_DIR}"
        OTEL_EXPORTER_OTLP_ENDPOINT = "http://127.0.0.1:4317"
        OTEL_RESOURCE_ATTRIBUTES = "verself.supervisor=nomad"
        OTEL_SERVICE_NAME = "secrets-service"
        HOME = "/var/lib/secrets-service/home"
        TMPDIR = "/var/lib/secrets-service/home/tmp"
        VERSELF_SUPERVISOR = "nomad"
      }
      template {
        change_mode = "restart"
        destination = "secrets/iam-service.zitadel.auth_audience"
        perms = "0600"
        uid = "975"
        gid = "968"
        data = <<-EOT
{{ with secret "kv-runtime/data/secret/org/iam-service.zitadel.auth_audience" }}{{ .Data.data.value }}{{ end }}
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
        name = "secrets-service-internal-https"
        port = "internal_https"
        provider = "nomad"
        address_mode = "auto"
        check {
          name = "secrets-service-tcp-internal_https"
          type = "tcp"
          port = "internal_https"
          interval = "1s"
          timeout = "3s"
        }
      }
      service {
        name = "secrets-service-public-http"
        port = "public_http"
        provider = "nomad"
        address_mode = "auto"
        check {
          name = "secrets-service-http-public_http"
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
