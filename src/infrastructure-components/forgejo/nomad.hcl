job "forgejo" {
  name = "forgejo"
  datacenters = ["dc1"]
  type = "service"

  group "forgejo" {
    count = 1

    network {
      mode = "host"
      port "http" {
        host_network = "loopback"
        static = 3000
        to = 3000
      }
    }

    task "server" {
      driver = "raw_exec"
      user = "forgejo"

      config {
        command = "/opt/verself/profile/bin/forgejo"
        args = ["web", "--config", "/etc/forgejo/app.ini"]
      }

      env {
        FORGEJO_WORK_DIR = "/var/lib/forgejo"
        PATH = "/opt/verself/profile/bin:/usr/bin:/bin"
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
      }
    }
  }
}
