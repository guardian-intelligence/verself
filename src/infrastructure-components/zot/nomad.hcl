variable "guardian_repo_root" {
  type    = string
  default = "/home/ubuntu/.local/state/guardian/repo/current"
}

variable "zot_resource_name" {
  type    = string
  default = "zot"
}

job "zot" {
  name        = "zot"
  datacenters = ["*"]
  type        = "service"

  group "zot" {
    count = 1

    reschedule {
      attempts  = 0
      unlimited = false
    }

    network {
      mode = "host"

      port "http" {
        host_network = "loopback"
        static       = 5080
        to           = 5080
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
        command = "${var.guardian_repo_root}/bazel-bin/src/infrastructure-components/zot/cmd/zot-recover/zot-recover_/zot-recover"
        args = [
          "recover",
          "--repo-root=${var.guardian_repo_root}",
          "--resource-graph=${var.guardian_repo_root}/workspace/.guardian/fly/document.json",
          "--resource-name=${var.zot_resource_name}",
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
        command = "/var/lib/zot/runtime/current/bin/zot-recover"
        args = [
          "server",
          "--repo-root=${var.guardian_repo_root}",
          "--resource-graph=${var.guardian_repo_root}/workspace/.guardian/fly/document.json",
          "--resource-name=${var.zot_resource_name}",
        ]
      }

      resources {
        cpu    = 400
        memory = 512
      }

      service {
        name         = "zot-http"
        port         = "http"
        provider     = "nomad"
        address_mode = "auto"

        check {
          name     = "zot-registry-v2"
          type     = "http"
          path     = "/v2/"
          interval = "2s"
          timeout  = "3s"
        }
      }
    }
  }
}
