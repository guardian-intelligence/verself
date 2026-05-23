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

      artifact {
        source = "verself-artifact://clickhouse-runtime"
        destination = "local"
      }

      config {
        command = "/bin/sh"
        args = ["-euc", "applied=0\nfor migration in local/migrations/[0-9][0-9][0-9]_*.up.sql; do\n  if [ ! -f \"$migration\" ]; then\n    echo \"clickhouse-migrations: no migration files matched local/migrations/[0-9][0-9][0-9]_*.up.sql\" >&2\n    exit 66\n  fi\n  applied=$((applied + 1))\n  echo \"clickhouse-migrations: applying $migration\" >&2\n  local/bin/clickhouse-client \\\n    --config-file /etc/clickhouse-client/operator.xml \\\n    --user clickhouse_operator \\\n    --database verself \\\n    --multiquery \\\n    --queries-file \"$migration\"\ndone\necho \"clickhouse-migrations: applied $applied migration files\" >&2\n"]
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
