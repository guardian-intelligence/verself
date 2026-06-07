variable "guardian_repo_root" {
  type    = string
  default = "/home/ubuntu/.local/state/guardian/repo/current"
}

variable "deployment_service_resource_name" {
  type    = string
  default = "deployment-service"
}

variable "deployment_service_runtime_root" {
  type    = string
  default = "/var/lib/deployment-service/runtime"
}

variable "deployment_service_projected_graph" {
  type    = string
  default = "/run/verself/recovery/deployment-service/document.json"
}

job "deployment-service" {
  name = "deployment-service"
  datacenters = ["*"]
  type = "service"
  group "deployment-service" {
    count = 1
    network {
      mode = "host"
      port "public_http" {
        host_network = "loopback"
      }
    }
    task "recover" {
      driver = "raw_exec"
      user = "root"
      lifecycle {
        hook = "prestart"
        sidecar = false
      }
      config {
        command = "${var.guardian_repo_root}/bazel-bin/src/services/deployment-service/cmd/deployment-service/deployment-service_/deployment-service"
        args = [
          "recover",
          "--repo-root=${var.guardian_repo_root}",
          "--resource-graph=${var.guardian_repo_root}/workspace/.guardian/fly/document.json",
          "--resource-name=${var.deployment_service_resource_name}",
          "--runtime-root=${var.deployment_service_runtime_root}",
          "--projected-graph=${var.deployment_service_projected_graph}",
          "--migrate",
        ]
      }
      env {
        OTEL_EXPORTER_OTLP_ENDPOINT = "http://127.0.0.1:4317"
        OTEL_RESOURCE_ATTRIBUTES = "verself.supervisor=nomad"
        OTEL_SERVICE_NAME = "deployment-service-migration"
        VERSELF_SUPERVISOR = "nomad"
      }
      resources {
        cpu = 100
        memory = 128
      }
    }
    task "deployment-service" {
      driver = "raw_exec"
      user = "deployment_service"
      kill_signal = "SIGTERM"
      kill_timeout = "30s"
      config {
        command = "${var.deployment_service_runtime_root}/current/bin/deployment-service"
        args = [
          "--resource-graph=${var.deployment_service_projected_graph}",
          "--resource-name=${var.deployment_service_resource_name}",
          "--listen-addr=127.0.0.1:$${NOMAD_PORT_public_http}",
        ]
      }
      env {
        HOME = "/var/lib/deployment-service/home"
        GIT_EXEC_PATH = "${var.deployment_service_runtime_root}/current/lib/git-core"
        GIT_TEMPLATE_DIR = "${var.deployment_service_runtime_root}/current/share/git-core/templates"
        LD_LIBRARY_PATH = "${var.deployment_service_runtime_root}/current/lib/x86_64-linux-gnu"
        LOGNAME = "deployment_service"
        OTEL_EXPORTER_OTLP_ENDPOINT = "http://127.0.0.1:4317"
        OTEL_RESOURCE_ATTRIBUTES = "verself.supervisor=nomad"
        OTEL_SERVICE_NAME = "deployment-service"
        PATH = "${var.deployment_service_runtime_root}/current/bin:/opt/verself/profile/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"
        TMPDIR = "/var/lib/deployment-service/home/tmp"
        USER = "deployment_service"
        VERSELF_SUPERVISOR = "nomad"
        XDG_CACHE_HOME = "/var/lib/deployment-service/home/.cache"
      }
      resources {
        cpu = 1500
        memory = 4096
      }
      restart {
        attempts = 3
        delay = "15s"
        interval = "300s"
        mode = "delay"
      }
      service {
        name = "deployment-service-public-api"
        port = "public_http"
        provider = "nomad"
        address_mode = "auto"
        check {
          name = "deployment-service-http-public_http"
          type = "http"
          path = "/healthz"
          port = "public_http"
          interval = "1s"
          timeout = "3s"
        }
      }
    }
    update {
      max_parallel = 1
      health_check = "checks"
      min_healthy_time = "3s"
      healthy_deadline = "300s"
      progress_deadline = "600s"
      canary = 1
      auto_revert = true
      auto_promote = true
    }
  }
}
