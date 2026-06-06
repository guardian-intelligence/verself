variable "guardian_repo_root" {
  type    = string
  default = "/home/ubuntu/.local/state/guardian/repo/current"
}

variable "tigerbeetle_resource_name" {
  type    = string
  default = "tigerbeetle"
}

job "tigerbeetle" {
  name        = "tigerbeetle"
  datacenters = ["*"]
  type        = "service"

  group "tigerbeetle" {
    count = 1

    reschedule {
      attempts  = 0
      unlimited = false
    }

    network {
      mode = "host"

      port "client" {
        host_network = "loopback"
        static       = 3320
        to           = 3320
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
        command = "${var.guardian_repo_root}/bazel-bin/src/infrastructure-components/tigerbeetle/cmd/tigerbeetle-recover/tigerbeetle-recover_/tigerbeetle-recover"
        args = [
          "recover",
          "--repo-root=${var.guardian_repo_root}",
          "--resource-graph=${var.guardian_repo_root}/workspace/.guardian/fly/document.json",
          "--resource-name=${var.tigerbeetle_resource_name}",
        ]
      }

      resources {
        cpu    = 100
        memory = 128
      }
    }

    task "server" {
      driver = "raw_exec"
      user   = "root"

      restart {
        attempts = 0
        mode     = "fail"
      }

      config {
        command = "/var/lib/tigerbeetle/runtime/current/bin/tigerbeetle-recover"
        args = [
          "server",
          "--repo-root=${var.guardian_repo_root}",
          "--resource-graph=${var.guardian_repo_root}/workspace/.guardian/fly/document.json",
          "--resource-name=${var.tigerbeetle_resource_name}",
        ]
      }

      resources {
        cpu    = 1000
        memory = 8192
      }

      service {
        name         = "tigerbeetle-client"
        port         = "client"
        provider     = "nomad"
        address_mode = "auto"

        check {
          name     = "tigerbeetle-client-tcp"
          type     = "tcp"
          interval = "1s"
          timeout  = "3s"
        }
      }
    }
  }
}
