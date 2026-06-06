variable "guardian_repo_root" {
  type    = string
  default = "/home/ubuntu/.local/state/guardian/repo/current"
}

variable "grafana_resource_name" {
  type    = string
  default = "grafana"
}

job "grafana" {
  name        = "grafana"
  datacenters = ["*"]
  type        = "service"

  group "grafana" {
    count = 1

    reschedule {
      attempts  = 0
      unlimited = false
    }

    network {
      mode = "host"

      port "http" {
        host_network = "loopback"
        static       = 4300
        to           = 4300
      }
    }

    task "recover" {
      driver = "raw_exec"
      user   = "root"

      vault {
        env  = false
        role = "grafana-runtime"
      }

      identity {
        name = "vault_default"
        aud  = ["vault.io"]
        ttl  = "1h"
      }

      restart {
        attempts = 3
        delay    = "10s"
        interval = "120s"
        mode     = "delay"
      }

      lifecycle {
        hook    = "prestart"
        sidecar = false
      }

      config {
        command = "${var.guardian_repo_root}/bazel-bin/src/infrastructure-components/grafana/cmd/grafana-recover/grafana-recover_/grafana-recover"
        args = [
          "recover",
          "--repo-root=${var.guardian_repo_root}",
          "--resource-graph=${var.guardian_repo_root}/workspace/.guardian/fly/document.json",
          "--resource-name=${var.grafana_resource_name}",
          "--openbao-token-file=$${NOMAD_SECRETS_DIR}/vault_token",
        ]
      }

      resources {
        cpu    = 100
        memory = 128
      }
    }

    task "server" {
      driver       = "raw_exec"
      user         = "root"
      kill_signal  = "SIGTERM"
      kill_timeout = "30s"

      vault {
        env  = false
        role = "grafana-runtime"
      }

      identity {
        name = "vault_default"
        aud  = ["vault.io"]
        ttl  = "1h"
      }

      restart {
        attempts = 3
        delay    = "15s"
        interval = "300s"
        mode     = "delay"
      }

      config {
        command = "/var/lib/grafana/runtime/current/bin/grafana-recover"
        args = [
          "server",
          "--repo-root=${var.guardian_repo_root}",
          "--resource-graph=${var.guardian_repo_root}/workspace/.guardian/fly/document.json",
          "--resource-name=${var.grafana_resource_name}",
          "--openbao-token-file=$${NOMAD_SECRETS_DIR}/vault_token",
        ]
      }

      resources {
        cpu    = 600
        memory = 1024
      }

      service {
        name         = "grafana-http"
        port         = "http"
        provider     = "nomad"
        address_mode = "auto"

        check {
          name     = "grafana-http-health"
          type     = "http"
          path     = "/api/health"
          port     = "http"
          interval = "2s"
          timeout  = "3s"
        }
      }
    }

    update {
      max_parallel      = 1
      health_check      = "checks"
      min_healthy_time  = "3s"
      healthy_deadline  = "300s"
      progress_deadline = "600s"
      canary            = 1
      auto_revert       = true
      auto_promote      = true
    }
  }
}
