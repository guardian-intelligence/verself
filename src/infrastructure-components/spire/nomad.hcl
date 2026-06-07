variable "guardian_repo_root" {
  type    = string
  default = "/home/ubuntu/.local/state/guardian/repo"
}

variable "spire_resource_name" {
  type    = string
  default = "spire"
}

job "spire" {
  name        = "spire"
  datacenters = ["*"]
  type        = "service"

  group "spire" {
    count = 1

    reschedule {
      attempts  = 0
      unlimited = false
    }

    network {
      mode = "host"
      port "server" {
        host_network = "loopback"
        static       = 8081
        to           = 8081
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
        command = "${var.guardian_repo_root}/bazel-bin/src/infrastructure-components/spire/spire-recover_/spire-recover"
        args = [
          "recover",
          "--repo-root=${var.guardian_repo_root}",
          "--resource-graph=${var.guardian_repo_root}/workspace/.guardian/fly/document.json",
          "--resource-name=${var.spire_resource_name}",
        ]
      }

      resources {
        cpu    = 50
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
        command = "/var/lib/spire/runtime/current/bin/spire-recover"
        args = [
          "exec",
          "--user=spire",
          "--",
          "/var/lib/spire/runtime/current/bin/spire-server",
          "run",
          "-config",
          "/etc/spire/server.conf",
        ]
      }

      resources {
        cpu    = 200
        memory = 512
      }
    }

    task "agent" {
      driver = "raw_exec"
      user   = "root"

      restart {
        attempts = 0
        mode     = "fail"
      }

      config {
        command = "/var/lib/spire/runtime/current/bin/spire-recover"
        args = [
          "agent",
          "--repo-root=${var.guardian_repo_root}",
          "--resource-graph=${var.guardian_repo_root}/workspace/.guardian/fly/document.json",
          "--resource-name=${var.spire_resource_name}",
        ]
      }

      resources {
        cpu    = 200
        memory = 512
      }
    }

    task "registrar" {
      driver = "raw_exec"
      user   = "root"

      restart {
        attempts = 0
        mode     = "fail"
      }

      lifecycle {
        hook    = "poststart"
        sidecar = true
      }

      config {
        command = "/var/lib/spire/runtime/current/bin/spire-recover"
        args = [
          "reconcile",
          "--loop",
          "--repo-root=${var.guardian_repo_root}",
          "--resource-graph=${var.guardian_repo_root}/workspace/.guardian/fly/document.json",
          "--resource-name=${var.spire_resource_name}",
        ]
      }

      resources {
        cpu    = 100
        memory = 128
      }
    }
  }
}
