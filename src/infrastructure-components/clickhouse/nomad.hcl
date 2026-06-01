job "clickhouse-migrations" {
  name = "clickhouse-migrations"
  datacenters = ["*"]
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
        args = ["-euc", <<-EOT
client=/opt/verself/profile/bin/clickhouse
test -x "$client"
rendered_dir="$NOMAD_TASK_DIR/rendered-clickhouse-migrations"
rm -rf "$rendered_dir"
mkdir -p "$rendered_dir"
token_prefix="__VERSELF_SPIFFE"
token="$token_prefix""_SERVICE_PREFIX__"
applied=0
for migration in local/migrations/[0-9][0-9][0-9]_*.up.sql; do
  if [ ! -f "$migration" ]; then
    continue
  fi
  rendered="$rendered_dir/$(basename "$migration")"
  sed "s#$token#$VERSELF_SPIFFE_SERVICE_PREFIX#g" "$migration" > "$rendered"
  applied=$((applied + 1))
  echo "clickhouse-migrations: applying $migration" >&2
  "$client" client \
    --config-file /etc/clickhouse-client/operator.xml \
    --user clickhouse_operator \
    --database verself \
    --multiquery \
    --queries-file "$rendered"
done
echo "clickhouse-migrations: applied $applied delta migration files" >&2
EOT
        ]
      }

      env {
        OTEL_EXPORTER_OTLP_ENDPOINT = "http://127.0.0.1:4317"
        OTEL_RESOURCE_ATTRIBUTES = "verself.supervisor=nomad"
        OTEL_SERVICE_NAME = "clickhouse-migrations"
        VERSELF_SUPERVISOR = "nomad"
        VERSELF_SPIFFE_SERVICE_PREFIX = "__VERSELF_SPIFFE_SERVICE_PREFIX__"
      }

      resources {
        cpu = 100
        memory = 128
      }
    }
  }
}
