job "stalwart" {
  name = "stalwart"
  datacenters = ["dc1"]
  type = "service"

  group "stalwart" {
    count = 1

    network {
      mode = "host"
      # Stalwart binds SMTP publicly; Nomad service discovery stays in-node.
      port "smtp" {
        host_network = "loopback"
        static = 25
        to = 25
      }
      port "http" {
        host_network = "loopback"
        static = 8090
        to = 8090
      }
    }

    task "server" {
      driver = "raw_exec"
      user = "stalwart"

      config {
        command = "/opt/verself/profile/bin/stalwart"
        args = ["--config", "/etc/stalwart/config.toml"]
      }

      env {
        OTEL_EXPORTER_OTLP_ENDPOINT = "http://127.0.0.1:4317"
        OTEL_RESOURCE_ATTRIBUTES = "verself.supervisor=nomad"
        OTEL_SERVICE_NAME = "stalwart"
        VERSELF_SUPERVISOR = "nomad"
      }

      resources {
        cpu = 700
        memory = 1024
      }

      service {
        name = "stalwart-http"
        port = "http"
        provider = "nomad"
        address_mode = "auto"
      }

      service {
        name = "stalwart-smtp"
        port = "smtp"
        provider = "nomad"
        address_mode = "auto"
      }
    }
  }
}
