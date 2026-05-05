job "clickhouse-migrations" {
  name = "clickhouse-migrations"
  datacenters = ["dc1"]
  type = "batch"

  group "clickhouse-migrations" {
    count = 1

    restart {
      attempts = 0
      mode = "fail"
    }

    task "apply" {
      driver = "raw_exec"
      user = "clickhouse_operator"

      artifact {
        source = "verself-artifact://clickhouse-migrations"
      }

      config {
        command = "/bin/sh"
        args = ["-ec", "for migration in local/migrations/[0-9][0-9][0-9]_*.up.sql; do\n  /opt/verself/profile/bin/clickhouse-client \\\n    --config-file /etc/clickhouse-client/operator.xml \\\n    --user clickhouse_operator \\\n    --database verself \\\n    --multiquery \\\n    --queries-file \"$${migration}\"\ndone\n"]
      }

      env {
        OTEL_EXPORTER_OTLP_ENDPOINT = "http://127.0.0.1:4317"
        OTEL_RESOURCE_ATTRIBUTES = "verself.supervisor=nomad"
        OTEL_SERVICE_NAME = "clickhouse-migrations"
        VERSELF_SUPERVISOR = "nomad"
      }

      resources {
        cpu = 100
        memory = 128
      }
    }
  }
}
