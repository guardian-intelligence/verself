job "postgresql" {
  name = "postgresql"
  datacenters = ["dc1"]
  type = "service"

  group "postgresql" {
    count = 1

    network {
      mode = "host"
      port "postgres" {
        host_network = "loopback"
        static = 5432
        to = 5432
      }
    }

    task "server" {
      driver = "raw_exec"
      user = "postgres"

      config {
        command = "/opt/verself/postgresql/usr/lib/postgresql/16/bin/postgres"
        args = ["-D", "/var/lib/postgresql/16/verself", "-c", "config_file=/etc/postgresql/verself/postgresql.conf"]
      }

      env {
        LD_LIBRARY_PATH = "/opt/verself/postgresql/usr/lib/x86_64-linux-gnu"
        OTEL_EXPORTER_OTLP_ENDPOINT = "http://127.0.0.1:4317"
        OTEL_RESOURCE_ATTRIBUTES = "verself.supervisor=nomad"
        OTEL_SERVICE_NAME = "postgresql"
        VERSELF_SUPERVISOR = "nomad"
      }

      resources {
        cpu = 400
        memory = 1024
      }

      service {
        name = "postgresql"
        port = "postgres"
        provider = "nomad"
        address_mode = "auto"
      }
    }
  }
}
