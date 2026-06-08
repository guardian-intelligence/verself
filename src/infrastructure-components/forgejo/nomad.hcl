variable "guardian_repo_root" {
  type    = string
  default = "/home/ubuntu/.local/state/guardian/repo"
}

variable "forgejo_resource_name" {
  type    = string
  default = "forgejo"
}

job "forgejo" {
  name        = "forgejo"
  datacenters = ["*"]
  type        = "service"

  group "forgejo" {
    count = 1

    reschedule {
      attempts  = 0
      unlimited = false
    }

    network {
      mode = "host"

      port "http" {
        host_network = "loopback"
        static       = 3000
        to           = 3000
      }
    }

    task "recover" {
      driver = "raw_exec"
      user   = "root"

      vault {
        env  = false
        role = "forgejo-runtime"
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
        command = "${var.guardian_repo_root}/bazel-bin/src/infrastructure-components/forgejo/cmd/forgejo-recover/forgejo-recover_/forgejo-recover"
        args = [
          "recover",
          "--repo-root=${var.guardian_repo_root}",
          "--resource-graph=${var.guardian_repo_root}/workspace/.guardian/fly/document.json",
          "--resource-name=${var.forgejo_resource_name}",
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
        role = "forgejo-runtime"
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
        command = "/var/lib/forgejo/runtime/current/bin/forgejo-recover"
        args = [
          "server",
          "--repo-root=${var.guardian_repo_root}",
          "--resource-graph=${var.guardian_repo_root}/workspace/.guardian/fly/document.json",
          "--resource-name=${var.forgejo_resource_name}",
          "--openbao-token-file=$${NOMAD_SECRETS_DIR}/vault_token",
        ]
      }

      resources {
        cpu    = 600
        memory = 768
      }

      service {
        name         = "forgejo-http"
        port         = "http"
        provider     = "nomad"
        address_mode = "auto"

        check {
          name     = "forgejo-tcp"
          type     = "tcp"
          port     = "http"
          interval = "2s"
          timeout  = "3s"
        }
      }
    }

    task "automation-token" {
      driver = "raw_exec"
      user   = "root"

      vault {
        env  = false
        role = "forgejo-runtime"
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
        hook    = "poststart"
        sidecar = false
      }

      config {
        command = "/var/lib/forgejo/runtime/current/bin/forgejo-recover"
        args = [
          "automation-token",
          "--repo-root=${var.guardian_repo_root}",
          "--resource-graph=${var.guardian_repo_root}/workspace/.guardian/fly/document.json",
          "--resource-name=${var.forgejo_resource_name}",
          "--openbao-token-file=$${NOMAD_SECRETS_DIR}/vault_token",
        ]
      }

      resources {
        cpu    = 100
        memory = 128
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
