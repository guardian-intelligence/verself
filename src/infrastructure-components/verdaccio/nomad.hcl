variable "guardian_repo_root" {
  type    = string
  default = "/home/ubuntu/.local/state/guardian/repo/current"
}

variable "verdaccio_resource_name" {
  type    = string
  default = "verdaccio"
}

job "verdaccio" {
  name        = "verdaccio"
  datacenters = ["*"]
  type        = "service"

  group "verdaccio" {
    count = 1

    reschedule {
      attempts  = 0
      unlimited = false
    }

    network {
      mode = "host"

      port "http" {
        host_network = "loopback"
        static       = 4873
        to           = 4873
      }
    }

    task "recover" {
      driver = "raw_exec"
      user   = "root"

      restart {
        attempts = 0
        mode     = "fail"
      }

      lifecycle {
        hook    = "prestart"
        sidecar = false
      }

      config {
        command = "${var.guardian_repo_root}/bazel-bin/src/infrastructure-components/verdaccio/cmd/verdaccio-recover/verdaccio-recover_/verdaccio-recover"
        args = [
          "recover",
          "--repo-root=${var.guardian_repo_root}",
          "--resource-graph=${var.guardian_repo_root}/workspace/.guardian/fly/document.json",
          "--resource-name=${var.verdaccio_resource_name}",
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

      restart {
        attempts = 0
        mode     = "fail"
      }

      config {
        command = "/var/lib/verdaccio/runtime/current/bin/verdaccio-recover"
        args = [
          "server",
          "--repo-root=${var.guardian_repo_root}",
          "--resource-graph=${var.guardian_repo_root}/workspace/.guardian/fly/document.json",
          "--resource-name=${var.verdaccio_resource_name}",
        ]
      }

      resources {
        cpu    = 500
        memory = 768
      }

      service {
        name         = "verdaccio-http"
        port         = "http"
        provider     = "nomad"
        address_mode = "auto"

        check {
          name     = "verdaccio-http-ping"
          type     = "http"
          path     = "/-/ping"
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
