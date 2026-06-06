variable "guardian_repo_root" {
  type    = string
  default = "/home/ubuntu/.local/state/guardian/repo/current"
}

variable "stalwart_resource_name" {
  type    = string
  default = "stalwart"
}

job "stalwart" {
  name        = "stalwart"
  datacenters = ["*"]
  type        = "service"

  group "stalwart" {
    count = 1

    reschedule {
      attempts  = 0
      unlimited = false
    }

    network {
      mode = "host"

      port "smtp" {
        host_network = "loopback"
        static       = 25
        to           = 25
      }

      port "http" {
        host_network = "loopback"
        static       = 8090
        to           = 8090
      }
    }

    task "recover" {
      driver = "raw_exec"
      user   = "root"

      vault {
        env  = false
        role = "stalwart-runtime"
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
        command = "${var.guardian_repo_root}/bazel-bin/src/infrastructure-components/stalwart/cmd/stalwart-recover/stalwart-recover_/stalwart-recover"
        args = [
          "recover",
          "--repo-root=${var.guardian_repo_root}",
          "--resource-graph=${var.guardian_repo_root}/workspace/.guardian/fly/document.json",
          "--resource-name=${var.stalwart_resource_name}",
          "--openbao-token-file=$${NOMAD_SECRETS_DIR}/vault_token",
        ]
      }

      resources {
        cpu    = 150
        memory = 256
      }
    }

    task "server" {
      driver       = "raw_exec"
      user         = "root"
      kill_signal  = "SIGTERM"
      kill_timeout = "30s"

      vault {
        env  = false
        role = "stalwart-runtime"
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
        command = "/var/lib/stalwart/runtime/current/bin/stalwart-recover"
        args = [
          "server",
          "--repo-root=${var.guardian_repo_root}",
          "--resource-graph=${var.guardian_repo_root}/workspace/.guardian/fly/document.json",
          "--resource-name=${var.stalwart_resource_name}",
          "--openbao-token-file=$${NOMAD_SECRETS_DIR}/vault_token",
        ]
      }

      resources {
        cpu    = 700
        memory = 1024
      }

      service {
        name         = "stalwart-http"
        port         = "http"
        provider     = "nomad"
        address_mode = "auto"

        check {
          name     = "stalwart-http-tcp"
          type     = "tcp"
          port     = "http"
          interval = "2s"
          timeout  = "3s"
        }
      }

      service {
        name         = "stalwart-smtp"
        port         = "smtp"
        provider     = "nomad"
        address_mode = "auto"

        check {
          name     = "stalwart-smtp-tcp"
          type     = "tcp"
          port     = "smtp"
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
