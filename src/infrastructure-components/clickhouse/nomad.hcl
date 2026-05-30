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
        args = ["-euc", <<-EOT
client=/opt/verself/profile/bin/clickhouse-client
test -x "$client"
trust_domain="$(cat /etc/verself/spiffe-trust-domain)"
test -n "$trust_domain"
"$client" \
  --config-file /etc/clickhouse-client/operator.xml \
  --user clickhouse_operator \
  --query 'CREATE DATABASE IF NOT EXISTS verself'
applied=0
for migration in local/migrations/[0-9][0-9][0-9]_*.up.sql; do
  if [ ! -f "$migration" ]; then
    continue
  fi
  applied=$((applied + 1))
  echo "clickhouse-migrations: applying $migration" >&2
  rendered="$(mktemp)"
  sed "s#spiffe://spiffe.verself.sh/#spiffe://$trust_domain/#g" "$migration" > "$rendered"
  "$client" \
    --config-file /etc/clickhouse-client/operator.xml \
    --user clickhouse_operator \
    --database verself \
    --multiquery \
    --queries-file "$rendered"
  rm -f "$rendered"
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
      }

      resources {
        cpu = 100
        memory = 128
      }
    }
  }
}
