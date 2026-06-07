variable "guardian_repo_root" {
  type    = string
  default = "/home/ubuntu/.local/state/guardian/repo/current"
}

variable "distribution_service_resource_name" {
  type    = string
  default = "distribution-service"
}

variable "distribution_service_runtime_root" {
  type    = string
  default = "/var/lib/distribution-service/runtime"
}

variable "distribution_service_projected_graph" {
  type    = string
  default = "/run/verself/recovery/distribution-service/document.json"
}

job "distribution-service" {
  name        = "distribution-service"
  datacenters = ["*"]
  type        = "service"

  group "distribution-service" {
    count = 2

    network {
      mode = "host"

      port "internal_https" {
        host_network = "loopback"
      }

      port "public_http" {
        host_network = "loopback"
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
        command = "${var.guardian_repo_root}/bazel-bin/src/services/distribution-service/cmd/distribution-service/distribution-service_/distribution-service"
        args = [
          "recover",
          "--repo-root=${var.guardian_repo_root}",
          "--resource-graph=${var.guardian_repo_root}/workspace/.guardian/fly/document.json",
          "--resource-name=${var.distribution_service_resource_name}",
          "--runtime-root=${var.distribution_service_runtime_root}",
          "--projected-graph=${var.distribution_service_projected_graph}",
          "--migrate",
        ]
      }

      resources {
        cpu    = 100
        memory = 128
      }
    }

    task "distribution-service" {
      driver         = "raw_exec"
      user           = "distribution_service"
      kill_signal    = "SIGTERM"
      kill_timeout   = "30s"
      shutdown_delay = "5s"

      config {
        command = "${var.distribution_service_runtime_root}/current/bin/distribution-service"
        args = [
          "--resource-graph=${var.distribution_service_projected_graph}",
          "--resource-name=${var.distribution_service_resource_name}",
          "--listen-addr=127.0.0.1:$${NOMAD_PORT_public_http}",
          "--internal-listen-addr=127.0.0.1:$${NOMAD_PORT_internal_https}",
        ]
      }

      env {
        HOME                         = "/var/lib/distribution-service/home"
        OTEL_EXPORTER_OTLP_ENDPOINT = "http://127.0.0.1:4317"
        OTEL_RESOURCE_ATTRIBUTES     = "verself.supervisor=nomad"
        OTEL_SERVICE_NAME            = "distribution-service"
        TMPDIR                       = "/var/lib/distribution-service/home/tmp"
        VERSELF_SUPERVISOR           = "nomad"
      }

      resources {
        cpu    = 500
        memory = 256
      }

      restart {
        attempts = 3
        delay    = "15s"
        interval = "300s"
        mode     = "delay"
      }

      service {
        name         = "distribution-service-internal-https"
        port         = "internal_https"
        provider     = "nomad"
        address_mode = "auto"

        check {
          name     = "distribution-service-tcp-internal_https"
          type     = "tcp"
          port     = "internal_https"
          interval = "1s"
          timeout  = "3s"
        }
      }

      service {
        name         = "distribution-service-public-http"
        port         = "public_http"
        provider     = "nomad"
        address_mode = "auto"

        check {
          name     = "distribution-service-http-public_http"
          type     = "http"
          path     = "/readyz"
          port     = "public_http"
          interval = "1s"
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
      canary            = 1
      auto_revert       = true
      auto_promote      = true
    }
  }
}
