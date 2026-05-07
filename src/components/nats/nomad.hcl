job "nats" {
  name = "nats"
  datacenters = ["dc1"]
  type = "service"

  group "nats" {
    count = 1

    network {
      mode = "host"
      port "client" {
        host_network = "loopback"
        static = 4222
        to = 4222
      }
      port "monitoring" {
        host_network = "loopback"
        static = 8222
        to = 8222
      }
    }

    task "server" {
      driver = "raw_exec"
      user = "nats"

      config {
        command = "/opt/verself/profile/bin/nats-server"
        args = ["-c", "/etc/nats/nats-server.conf"]
      }

      env {
        OTEL_EXPORTER_OTLP_ENDPOINT = "http://127.0.0.1:4317"
        OTEL_RESOURCE_ATTRIBUTES = "verself.supervisor=nomad"
        OTEL_SERVICE_NAME = "nats"
        VERSELF_SUPERVISOR = "nomad"
      }

      resources {
        cpu = 300
        memory = 512
      }

      service {
        name = "nats-client"
        port = "client"
        provider = "nomad"
        address_mode = "auto"
      }

      service {
        name = "nats-monitoring"
        port = "monitoring"
        provider = "nomad"
        address_mode = "auto"
      }
    }
  }
}
