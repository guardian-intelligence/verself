job "postgresql" {
  name = "postgresql"
  datacenters = ["dc1"]
  type = "service"

  group "postgresql" {
    count = 1

    meta {
      verself_group_kind = "service"
    }

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
        command = "$${VERSELF_POSTGRESQL_RUNTIME}/opt/verself/postgresql/usr/lib/postgresql/16/bin/postgres"
        args = ["-D", "/var/lib/postgresql/16/verself", "-c", "config_file=/etc/postgresql/verself/postgresql.conf"]
      }

      env {
        LD_LIBRARY_PATH = "$${VERSELF_POSTGRESQL_RUNTIME}/opt/verself/postgresql/usr/lib/x86_64-linux-gnu:$${VERSELF_POSTGRESQL_RUNTIME}/opt/verself/postgresql/usr/lib/postgresql/16/lib"
        OTEL_EXPORTER_OTLP_ENDPOINT = "http://127.0.0.1:4317"
        OTEL_RESOURCE_ATTRIBUTES = "verself.supervisor=nomad"
        OTEL_SERVICE_NAME = "postgresql"
        VERSELF_POSTGRESQL_RUNTIME = "verself-artifact://postgresql-runtime"
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
        check {
          name = "postgresql-tcp"
          type = "tcp"
          port = "postgres"
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
