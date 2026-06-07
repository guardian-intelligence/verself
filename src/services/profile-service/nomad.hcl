variable "guardian_repo_root" {
  type    = string
  default = "/home/ubuntu/.local/state/guardian/repo/current"
}

variable "profile_service_resource_name" {
  type    = string
  default = "profile-service"
}

variable "profile_service_runtime_root" {
  type    = string
  default = "/var/lib/profile-service/runtime"
}

variable "profile_service_projected_graph" {
  type    = string
  default = "/run/verself/recovery/profile-service/document.json"
}

variable "profile_service_image_archive" {
  type    = string
  default = "/home/ubuntu/.local/state/guardian/repo/current/bazel-bin/src/services/profile-service/cmd/profile-service/profile-service_image_load/tarball.tar"
}

variable "profile_service_projected_image" {
  type    = string
  default = "/var/lib/profile-service/runtime/image.tar"
}

variable "profile_service_image_digest" {
  type    = string
  default = "sha256:local"
}

job "profile-service" {
  name        = "profile-service"
  datacenters = ["*"]
  type        = "service"
  meta {
    image_digest = "${var.profile_service_image_digest}"
  }

  group "profile-service" {
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
        command = "${var.guardian_repo_root}/bazel-bin/src/services/profile-service/cmd/profile-service/profile-service_/profile-service"
        args = [
          "recover",
          "--resource-graph=${var.guardian_repo_root}/workspace/.guardian/fly/document.json",
          "--runtime-root=${var.profile_service_runtime_root}",
          "--projected-graph=${var.profile_service_projected_graph}",
          "--image-archive=${var.profile_service_image_archive}",
          "--projected-image=${var.profile_service_projected_image}",
        ]
      }

      resources {
        cpu    = 100
        memory = 128
      }
    }

    task "migrate" {
      driver = "podman"
      user   = "profile_service"

      lifecycle {
        hook    = "prestart"
        sidecar = false
      }

      config {
        image        = "docker-archive:${var.profile_service_projected_image}"
        network_mode = "host"
        readonly_rootfs = true
        volumes = [
          "${var.profile_service_projected_graph}:/guardian/document.json:ro,noexec",
          "/var/run/postgresql:/var/run/postgresql",
        ]
        tmpfs = ["/tmp"]
        args = [
          "migrate",
          "--resource-graph=/guardian/document.json",
          "--resource-name=${var.profile_service_resource_name}",
          "up",
        ]
      }

      env {
        HOME   = "/tmp"
        TMPDIR = "/tmp"
      }

      resources {
        cpu    = 100
        memory = 128
      }
    }

    task "profile-service" {
      driver         = "podman"
      user           = "profile_service"
      kill_signal    = "SIGTERM"
      kill_timeout   = "30s"
      shutdown_delay = "5s"

      config {
        image        = "docker-archive:${var.profile_service_projected_image}"
        network_mode = "host"
        readonly_rootfs = true
        volumes = [
          "${var.profile_service_projected_graph}:/guardian/document.json:ro,noexec",
          "/run/spire-agent/sockets:/run/spire-agent/sockets:ro",
          "/var/run/postgresql:/var/run/postgresql",
        ]
        tmpfs = ["/tmp"]
        args = [
          "--resource-graph=/guardian/document.json",
          "--resource-name=${var.profile_service_resource_name}",
          "--listen-addr=127.0.0.1:$${NOMAD_PORT_public_http}",
          "--internal-listen-addr=127.0.0.1:$${NOMAD_PORT_internal_https}",
        ]
      }

      env {
        HOME                         = "/tmp"
        OTEL_EXPORTER_OTLP_ENDPOINT = "http://127.0.0.1:4317"
        OTEL_RESOURCE_ATTRIBUTES     = "verself.supervisor=nomad"
        OTEL_SERVICE_NAME            = "profile-service"
        TMPDIR                       = "/tmp"
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
        name         = "profile-service-internal-https"
        port         = "internal_https"
        provider     = "nomad"
        address_mode = "auto"

        check {
          name     = "profile-service-tcp-internal_https"
          type     = "tcp"
          port     = "internal_https"
          interval = "1s"
          timeout  = "3s"
        }
      }

      service {
        name         = "profile-service-public-http"
        port         = "public_http"
        provider     = "nomad"
        address_mode = "auto"

        check {
          name     = "profile-service-http-public_http"
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
