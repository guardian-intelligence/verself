variable "guardian_repo_root" {
  type    = string
  default = "/home/ubuntu/.local/state/guardian/repo/current"
}

variable "openbao_addr" {
  type    = string
  default = "https://127.0.0.1:8200"
}

variable "openbao_init_material_path" {
  type    = string
  default = "/run/verself/recovery/openbao/init-material.json"
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
          "--runtime-root=/var/lib/openbao/runtime",
          "--data-dir=/var/lib/openbao/raft",
          "--config=/etc/openbao/openbao.hcl",
          "--addr=${var.openbao_addr}",
          "--ca-cert=/etc/openbao/tls/cert.pem",
          "--report=/run/verself/recovery/openbao/report.json",
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
        BAO_ADDR                    = "${var.openbao_addr}"
        BAO_CACERT                  = "/etc/openbao/tls/cert.pem"
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
          "--runtime-root=/var/lib/openbao/runtime",
          "--bao=/var/lib/openbao/runtime/current/bin/bao",
          "--addr=${var.openbao_addr}",
          "--ca-cert=/etc/openbao/tls/cert.pem",
          "--data-dir=/var/lib/openbao/raft",
          "--config=/etc/openbao/openbao.hcl",
          "--report=/run/verself/recovery/openbao/report.json",
          "--init-output=${var.openbao_init_material_path}",
          "--loop-interval=15s",
        ]
      }

      resources {
        cpu    = 50
        memory = 256
      }
    }
  }
}
