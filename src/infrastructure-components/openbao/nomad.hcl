variable "guardian_repo_root" {
  type    = string
  default = "/home/ubuntu/.local/state/guardian/repo/current"
}

job "openbao" {
  name        = "openbao"
  datacenters = ["*"]
  type        = "service"

  group "openbao" {
    count = 1

    network {
      mode = "host"

      port "api" {
        host_network = "loopback"
        static       = 8200
        to           = 8200
      }

      port "cluster" {
        host_network = "loopback"
        static       = 8201
        to           = 8201
      }
    }

    task "setup" {
      driver = "raw_exec"
      user   = "root"

      lifecycle {
        hook    = "prestart"
        sidecar = false
      }

      config {
        command = "${var.guardian_repo_root}/bazel-bin/src/infrastructure-components/openbao/cmd/openbao-recover/openbao-recover_/openbao-recover"
        args = [
          "prepare",
          "--repo-root=${var.guardian_repo_root}",
          "--resource-graph=${var.guardian_repo_root}/workspace/.guardian/fly/document.json",
          "--resource-name=openbao",
        ]
      }

      resources {
        cpu    = 50
        memory = 256
      }
    }

    task "server" {
      driver = "raw_exec"
      user   = "openbao"

      config {
        command = "/var/lib/openbao/runtime/current/bin/bao"
        args    = ["server", "-config=/etc/openbao/openbao.hcl"]
      }

      restart {
        attempts = 0
        mode     = "fail"
      }

      env {
        HOME                        = "/var/lib/openbao"
        OTEL_EXPORTER_OTLP_ENDPOINT = "http://127.0.0.1:4317"
        OTEL_RESOURCE_ATTRIBUTES    = "verself.supervisor=nomad"
        OTEL_SERVICE_NAME           = "openbao"
        VERSELF_SUPERVISOR          = "nomad"
      }

      resources {
        cpu    = 500
        memory = 4096
      }

      service {
        name         = "openbao-api"
        port         = "api"
        provider     = "nomad"
        address_mode = "auto"
      }
    }

    task "recover" {
      driver = "raw_exec"
      user   = "root"

      lifecycle {
        hook    = "poststart"
        sidecar = true
      }

      config {
        command = "/var/lib/openbao/runtime/current/bin/openbao-recover"
        args = [
          "loop",
          "--repo-root=${var.guardian_repo_root}",
          "--resource-graph=${var.guardian_repo_root}/workspace/.guardian/fly/document.json",
          "--resource-name=openbao",
        ]
      }

      resources {
        cpu    = 50
        memory = 256
      }
    }
  }
}
