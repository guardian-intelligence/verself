variable "guardian_repo_root" {
  type    = string
  default = "/home/ubuntu/.local/state/guardian/repo/current"
}

variable "otelcol_resource_name" {
  type    = string
  default = "otelcol"
}

job "otelcol" {
  name        = "otelcol"
  datacenters = ["*"]
  type        = "service"

  group "otelcol" {
    count = 1

    network {
      mode = "host"
      port "otlp_grpc" {
        host_network = "loopback"
        static = 4317
        to = 4317
      }
      port "otlp_http" {
        host_network = "loopback"
        static = 4318
        to = 4318
      }
      port "health" {
        host_network = "loopback"
        static = 13133
        to = 13133
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
        command = "${var.guardian_repo_root}/bazel-bin/src/infrastructure-components/otelcol/cmd/otelcol-recover/otelcol-recover_/otelcol-recover"
        args = [
          "recover",
          "--repo-root=${var.guardian_repo_root}",
          "--resource-graph=${var.guardian_repo_root}/workspace/.guardian/fly/document.json",
          "--resource-name=${var.otelcol_resource_name}",
        ]
      }

      resources {
        cpu    = 50
        memory = 64
      }
    }

    task "clickhouse-spiffe-helper" {
      driver = "raw_exec"
      user   = "otelcol"

      config {
        command = "/var/lib/otelcol/runtime/current/bin/spiffe-helper"
        args    = ["-config", "/etc/otelcol/current/config/clickhouse-spiffe-helper.conf"]
      }

      resources {
        cpu = 50
        memory = 64
      }
    }

    task "collector" {
      driver = "raw_exec"
      user   = "otelcol"

      config {
        command = "/var/lib/otelcol/runtime/current/bin/otelcol-contrib"
        args    = ["--config", "/etc/otelcol/current/config/config.yaml"]
      }

      resources {
        cpu = 600
        memory = 2048
      }

      service {
        name = "otelcol-otlp-grpc"
        port = "otlp_grpc"
        provider = "nomad"
        address_mode = "auto"
      }

      service {
        name = "otelcol-otlp-http"
        port = "otlp_http"
        provider = "nomad"
        address_mode = "auto"
      }

      service {
        name         = "otelcol-health"
        port         = "health"
        provider     = "nomad"
        address_mode = "auto"

        check {
          type     = "http"
          path     = "/"
          interval = "10s"
          timeout  = "2s"
        }
      }
    }
  }
}
