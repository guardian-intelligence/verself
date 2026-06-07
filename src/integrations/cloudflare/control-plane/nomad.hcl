variable "guardian_repo_root" {
  type    = string
  default = "/home/ubuntu/.local/state/guardian/repo"
}

variable "cloudflare_control_plane_image_archive" {
  type    = string
  default = ""
}

variable "cloudflare_control_plane_image_digest" {
  type    = string
  default = "sha256:local"
}

job "cloudflare-integration-recovery" {
  name        = "cloudflare-integration-recovery"
  datacenters = ["*"]
  type        = "batch"
  meta {
    image_digest = "${var.cloudflare_control_plane_image_digest}"
  }

  group "cloudflare-integration-recovery" {
    count = 1

    task "prepare-certificate-output" {
      driver = "raw_exec"
      user   = "root"

      lifecycle {
        hook    = "prestart"
        sidecar = false
      }

      config {
        command = "/usr/bin/install"
        args = [
          "-d",
          "-m",
          "0700",
          "/etc/haproxy/certs",
        ]
      }

      resources {
        cpu    = 25
        memory = 16
      }
    }

    task "recover" {
      driver = "podman"

      vault {
        role        = "cloudflare-integration-recovery"
        change_mode = "noop"
      }

      config {
        image           = "docker-archive:${var.cloudflare_control_plane_image_archive}"
        init            = true
        network_mode    = "host"
        readonly_rootfs = true
        cap_drop        = ["all"]
        security_opt    = ["no-new-privileges"]
        volumes = [
          "${var.guardian_repo_root}/workspace/.guardian/fly/document.json:/guardian/document.json:ro,noexec",
          "/etc/haproxy:/etc/haproxy:rw,noexec",
          "/etc/ssl/certs:/etc/ssl/certs:ro,noexec",
          "/etc/verself/openbao/ca.pem:/etc/verself/openbao/ca.pem:ro",
        ]
        tmpfs = ["/tmp"]
        args = [
          "--action=recover",
          "--recovery-config=/guardian/document.json",
          "--timeout=10m",
        ]
      }

      env {
        BAO_ADDR                    = "https://127.0.0.1:8200"
        BAO_CACERT                  = "/etc/verself/openbao/ca.pem"
        HOME                        = "/tmp"
        OTEL_EXPORTER_OTLP_ENDPOINT = "http://127.0.0.1:4317"
        OTEL_RESOURCE_ATTRIBUTES    = "verself.supervisor=nomad"
        OTEL_SERVICE_NAME           = "cloudflare-integration-recovery"
        TMPDIR                      = "/tmp"
        VERSELF_SITE                = "gamma"
        VERSELF_SUPERVISOR          = "nomad"
      }

      resources {
        cpu    = 150
        memory = 128
      }

      restart {
        attempts = 0
        delay    = "15s"
        interval = "10m"
        mode     = "fail"
      }
    }

    reschedule {
      attempts = 0
    }
  }
}
