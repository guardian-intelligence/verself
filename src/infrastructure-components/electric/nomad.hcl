variable "guardian_repo_root" {
  type    = string
  default = "/home/ubuntu/.local/state/guardian/repo/current"
}

variable "electric_resource_name" {
  type    = string
  default = "electric"
}

job "electric" {
  name        = "electric"
  datacenters = ["*"]
  type        = "service"

  group "electric-default" {
    count = 1

    reschedule {
      attempts  = 0
      unlimited = false
    }

    network {
      mode = "host"

      port "http" {
        host_network = "loopback"
      }
    }

    task "image" {
      driver = "raw_exec"
      user   = "root"

      lifecycle {
        hook    = "prestart"
        sidecar = false
      }

      config {
        command = "${var.guardian_repo_root}/bazel-bin/src/infrastructure-components/electric/cmd/electric-recover/electric-recover_/electric-recover"
        args = [
          "image",
          "--repo-root=${var.guardian_repo_root}",
          "--resource-graph=${var.guardian_repo_root}/workspace/.guardian/fly/document.json",
          "--resource-name=${var.electric_resource_name}",
          "--instance=electric",
        ]
      }

      resources {
        cpu    = 200
        memory = 256
      }
    }

    task "server" {
      driver       = "raw_exec"
      user         = "root"
      kill_signal  = "SIGTERM"
      kill_timeout = "45s"

      vault {
        env  = false
        role = "electric-runtime"
      }

      identity {
        name = "vault_default"
        aud  = ["vault.io"]
        ttl  = "1h"
      }

      config {
        command = "/var/lib/electric/runtime/current/bin/electric-recover"
        args = [
          "run",
          "--repo-root=${var.guardian_repo_root}",
          "--resource-graph=${var.guardian_repo_root}/workspace/.guardian/fly/document.json",
          "--resource-name=${var.electric_resource_name}",
          "--instance=electric",
          "--openbao-token-file=$${NOMAD_SECRETS_DIR}/vault_token",
        ]
      }

      resources {
        cpu    = 500
        memory = 512
      }

      restart {
        attempts = 3
        delay    = "15s"
        interval = "300s"
        mode     = "delay"
      }

      service {
        name         = "electric-http"
        port         = "http"
        provider     = "nomad"
        address_mode = "auto"

        check {
          name     = "electric-http-tcp"
          type     = "tcp"
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
      auto_revert       = true
    }
  }

  group "electric-notifications" {
    count = 1

    reschedule {
      attempts  = 0
      unlimited = false
    }

    network {
      mode = "host"

      port "http" {
        host_network = "loopback"
      }
    }

    task "image" {
      driver = "raw_exec"
      user   = "root"

      lifecycle {
        hook    = "prestart"
        sidecar = false
      }

      config {
        command = "${var.guardian_repo_root}/bazel-bin/src/infrastructure-components/electric/cmd/electric-recover/electric-recover_/electric-recover"
        args = [
          "image",
          "--repo-root=${var.guardian_repo_root}",
          "--resource-graph=${var.guardian_repo_root}/workspace/.guardian/fly/document.json",
          "--resource-name=${var.electric_resource_name}",
          "--instance=electric-notifications",
        ]
      }

      resources {
        cpu    = 200
        memory = 256
      }
    }

    task "server" {
      driver       = "raw_exec"
      user         = "root"
      kill_signal  = "SIGTERM"
      kill_timeout = "45s"

      vault {
        env  = false
        role = "electric-runtime"
      }

      identity {
        name = "vault_default"
        aud  = ["vault.io"]
        ttl  = "1h"
      }

      config {
        command = "/var/lib/electric/runtime/current/bin/electric-recover"
        args = [
          "run",
          "--repo-root=${var.guardian_repo_root}",
          "--resource-graph=${var.guardian_repo_root}/workspace/.guardian/fly/document.json",
          "--resource-name=${var.electric_resource_name}",
          "--instance=electric-notifications",
          "--openbao-token-file=$${NOMAD_SECRETS_DIR}/vault_token",
        ]
      }

      resources {
        cpu    = 500
        memory = 512
      }

      restart {
        attempts = 3
        delay    = "15s"
        interval = "300s"
        mode     = "delay"
      }

      service {
        name         = "electric-notifications-http"
        port         = "http"
        provider     = "nomad"
        address_mode = "auto"

        check {
          name     = "electric-notifications-http-tcp"
          type     = "tcp"
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
      auto_revert       = true
    }
  }

  group "electric-iam" {
    count = 1

    reschedule {
      attempts  = 0
      unlimited = false
    }

    network {
      mode = "host"

      port "http" {
        host_network = "loopback"
      }
    }

    task "image" {
      driver = "raw_exec"
      user   = "root"

      lifecycle {
        hook    = "prestart"
        sidecar = false
      }

      config {
        command = "${var.guardian_repo_root}/bazel-bin/src/infrastructure-components/electric/cmd/electric-recover/electric-recover_/electric-recover"
        args = [
          "image",
          "--repo-root=${var.guardian_repo_root}",
          "--resource-graph=${var.guardian_repo_root}/workspace/.guardian/fly/document.json",
          "--resource-name=${var.electric_resource_name}",
          "--instance=electric-iam",
        ]
      }

      resources {
        cpu    = 200
        memory = 256
      }
    }

    task "server" {
      driver       = "raw_exec"
      user         = "root"
      kill_signal  = "SIGTERM"
      kill_timeout = "45s"

      vault {
        env  = false
        role = "electric-runtime"
      }

      identity {
        name = "vault_default"
        aud  = ["vault.io"]
        ttl  = "1h"
      }

      config {
        command = "/var/lib/electric/runtime/current/bin/electric-recover"
        args = [
          "run",
          "--repo-root=${var.guardian_repo_root}",
          "--resource-graph=${var.guardian_repo_root}/workspace/.guardian/fly/document.json",
          "--resource-name=${var.electric_resource_name}",
          "--instance=electric-iam",
          "--openbao-token-file=$${NOMAD_SECRETS_DIR}/vault_token",
        ]
      }

      resources {
        cpu    = 500
        memory = 512
      }

      restart {
        attempts = 3
        delay    = "15s"
        interval = "300s"
        mode     = "delay"
      }

      service {
        name         = "electric-iam-http"
        port         = "http"
        provider     = "nomad"
        address_mode = "auto"

        check {
          name     = "electric-iam-http-tcp"
          type     = "tcp"
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
      auto_revert       = true
    }
  }
}
