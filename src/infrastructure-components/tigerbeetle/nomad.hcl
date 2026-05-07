job "tigerbeetle" {
  name = "tigerbeetle"
  datacenters = ["dc1"]
  type = "service"

  group "tigerbeetle" {
    count = 1

    network {
      mode = "host"
      port "client" {
        host_network = "loopback"
        static = 3320
        to = 3320
      }
    }

    task "server" {
      driver = "raw_exec"
      user = "tigerbeetle"

      config {
        command = "/opt/verself/profile/bin/tigerbeetle"
        args = [
          "start",
          "--addresses=127.0.0.1:3320",
          "--experimental",
          "--statsd=127.0.0.1:8125",
          "/var/lib/tigerbeetle/data.tigerbeetle",
        ]
      }

      env {
        TB_LOG = "info"
        TB_STATSSD = "127.0.0.1:8125"
        OTEL_EXPORTER_OTLP_ENDPOINT = "http://127.0.0.1:4317"
        OTEL_RESOURCE_ATTRIBUTES = "verself.supervisor=nomad"
        OTEL_SERVICE_NAME = "tigerbeetle"
        VERSELF_SUPERVISOR = "nomad"
      }

      resources {
        cpu = 1000
        memory = 8192
      }

      service {
        name = "tigerbeetle-client"
        port = "client"
        provider = "nomad"
        address_mode = "auto"
      }
    }
  }
}
