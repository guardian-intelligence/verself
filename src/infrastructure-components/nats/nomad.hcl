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

    reschedule {
      attempts  = 0
      unlimited = false
    }

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

      restart {
        attempts = 0
        mode     = "fail"
      }

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
      user   = "root"

      restart {
        attempts = 0
        mode     = "fail"
      }

      config {
        command = "/var/lib/nats/runtime/current/bin/nats-recover"
        args = [
          "exec",
          "--user=nats",
          "--",
          "/var/lib/nats/runtime/current/bin/spiffe-helper",
          "-config",
          "/etc/nats/nats-spiffe-helper.conf",
        ]
      }

      resources {
        cpu    = 50
        memory = 64
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
        command = "/var/lib/nats/runtime/current/bin/nats-recover"
        args = [
          "exec",
          "--user=nats",
          "--wait-file=/var/lib/nats/spiffe/svid.pem",
          "--wait-file=/var/lib/nats/spiffe/svid_key.pem",
          "--wait-file=/var/lib/nats/spiffe/bundle.pem",
          "--",
          "/var/lib/nats/runtime/current/bin/nats-server",
          "-c",
          "/etc/nats/nats-server.conf",
        ]
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
