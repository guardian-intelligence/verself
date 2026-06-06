variable "guardian_repo_root" {
  type    = string
  default = "/home/ubuntu/.local/state/guardian/repo/current"
}

variable "nats_resource_name" {
  type    = string
  default = "nats"
}

job "nats" {
  name        = "nats"
  datacenters = ["*"]
  type        = "service"

  group "nats" {
    count = 1

    network {
      mode = "host"
      port "client" {
        host_network = "loopback"
        static = 4222
        to = 4222
      }
      port "monitoring" {
        host_network = "loopback"
        static = 8222
        to = 8222
      }
    }

    task "recover" {
      driver = "raw_exec"
      user   = "root"

      lifecycle {
        hook    = "prestart"
        sidecar = false
      }

      config {
        command = "${var.guardian_repo_root}/bazel-bin/src/infrastructure-components/nats/cmd/nats-recover/nats-recover_/nats-recover"
        args = [
          "recover",
          "--repo-root=${var.guardian_repo_root}",
          "--resource-graph=${var.guardian_repo_root}/workspace/.guardian/fly/document.json",
          "--resource-name=${var.nats_resource_name}",
        ]
      }

      resources {
        cpu    = 50
        memory = 64
      }
    }

    task "spiffe-helper" {
      driver = "raw_exec"
      user   = "nats"

      lifecycle {
        hook    = "prestart"
        sidecar = true
      }

      config {
        command = "/var/lib/nats/runtime/current/bin/spiffe-helper"
        args    = ["-config", "/etc/nats/nats-spiffe-helper.conf"]
      }

      resources {
        cpu    = 50
        memory = 64
      }
    }

    task "server" {
      driver = "raw_exec"
      user   = "nats"

      config {
        command = "/var/lib/nats/runtime/current/bin/nats-server"
        args    = ["-c", "/etc/nats/nats-server.conf"]
      }

      resources {
        cpu    = 300
        memory = 512
      }

      service {
        name         = "nats-client"
        port         = "client"
        provider     = "nomad"
        address_mode = "auto"
      }

      service {
        name         = "nats-monitoring"
        port         = "monitoring"
        provider     = "nomad"
        address_mode = "auto"

        check {
          type     = "http"
          path     = "/varz"
          interval = "10s"
          timeout  = "2s"
        }
      }
    }
  }
}
