job "forgejo" {
  name = "forgejo"
  datacenters = ["dc1"]
  type = "service"

  group "forgejo" {
    count = 1

    meta {
      verself_group_kind = "service"
      verself_allow_lifecycle_bootstrap = "true"
    }

    network {
      mode = "host"
      port "http" {
        host_network = "loopback"
        static = 3000
        to = 3000
      }
    }

    task "setup" {
      driver = "raw_exec"
      user = "forgejo"

      lifecycle {
        hook = "prestart"
        sidecar = false
      }

      artifact {
        source = "verself-artifact://forgejo-runtime"
        destination = "local"
        chown = true
      }

      config {
        command = "/bin/sh"
        args = ["-ec", "export HOME=/var/lib/forgejo\nexport FORGEJO_WORK_DIR=/var/lib/forgejo\nexport PATH=\"$PWD/local/bin:/usr/bin:/bin\"\nlocal/bin/forgejo migrate --config /etc/forgejo/app.ini --work-path /var/lib/forgejo\nlocal/bin/forgejo admin regenerate keys --config /etc/forgejo/app.ini\n"]
      }
    }

    task "server" {
      driver = "raw_exec"
      user = "forgejo"

      artifact {
        source = "verself-artifact://forgejo-runtime"
        destination = "local"
        chown = true
      }

      config {
        command = "local/bin/forgejo"
        args = ["web", "--config", "/etc/forgejo/app.ini"]
      }

      env {
        HOME = "/var/lib/forgejo"
        FORGEJO_WORK_DIR = "/var/lib/forgejo"
        PATH = "$${NOMAD_TASK_DIR}/local/bin:/usr/bin:/bin"
        OTEL_EXPORTER_OTLP_ENDPOINT = "http://127.0.0.1:4317"
        OTEL_RESOURCE_ATTRIBUTES = "verself.supervisor=nomad"
        OTEL_SERVICE_NAME = "forgejo"
        VERSELF_SUPERVISOR = "nomad"
      }

      resources {
        cpu = 600
        memory = 768
      }

      service {
        name = "forgejo-http"
        port = "http"
        provider = "nomad"
        address_mode = "auto"
        check {
          name = "forgejo-http-tcp"
          type = "tcp"
          port = "http"
          interval = "2s"
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
      auto_revert = true
    }
  }
}
