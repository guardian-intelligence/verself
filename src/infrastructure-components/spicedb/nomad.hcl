job "spicedb" {
  name = "spicedb"
  datacenters = ["*"]
  type = "service"
  group "spicedb" {
    count = 1
    network {
      mode = "host"
      port "grpc" {
        host_network = "loopback"
      }
      port "metrics" {
        host_network = "loopback"
      }
    }
    task "spicedb-migrate" {
      driver = "raw_exec"
      user = "spicedb"
      artifact {
        source = "verself-artifact://spicedb-runtime"
        destination = "local"
        chown = true
      }
      lifecycle {
        hook = "prestart"
        sidecar = false
      }
      config {
        args = ["datastore", "migrate", "head", "--datastore-engine", "postgres", "--datastore-conn-uri", "postgres://spicedb@/spicedb?host=/var/run/postgresql&sslmode=disable&application_name=spicedb", "--log-format", "json", "--skip-release-check"]
        command = "local/bin/spicedb"
      }
      env {
        HOME = "/tmp"
        OTEL_EXPORTER_OTLP_ENDPOINT = "http://127.0.0.1:4317"
        OTEL_RESOURCE_ATTRIBUTES = "verself.supervisor=nomad"
        OTEL_SERVICE_NAME = "spicedb-migration"
        VERSELF_SUPERVISOR = "nomad"
      }
      resources {
        cpu = 100
        memory = 256
      }
    }
    task "spicedb" {
      driver = "raw_exec"
      user = "spicedb"
      kill_signal = "SIGTERM"
      kill_timeout = "30s"
      vault {
        role = "spicedb-runtime"
      }
      identity {
        name = "vault_default"
        aud = ["vault.io"]
        ttl = "1h"
      }
      artifact {
        source = "verself-artifact://spicedb-runtime"
        destination = "local"
        chown = true
      }
      config {
        args = ["-ec", "exec local/bin/spicedb serve \\\n  --datastore-engine=postgres \\\n  --datastore-conn-uri='postgres://spicedb@/spicedb?host=/var/run/postgresql&sslmode=disable&application_name=spicedb' \\\n  --datastore-conn-pool-read-max-open=8 \\\n  --datastore-conn-pool-read-min-open=1 \\\n  --datastore-conn-pool-write-max-open=4 \\\n  --datastore-conn-pool-write-min-open=1 \\\n  --grpc-addr=\"127.0.0.1:$${NOMAD_PORT_grpc}\" \\\n  --metrics-addr=\"127.0.0.1:$${NOMAD_PORT_metrics}\" \\\n  --http-enabled=false \\\n  --telemetry-endpoint= \\\n  --skip-release-check \\\n  --log-format=json\n"]
        command = "/bin/sh"
      }
      env {
        OTEL_EXPORTER_OTLP_ENDPOINT = "http://127.0.0.1:4317"
        OTEL_RESOURCE_ATTRIBUTES = "verself.supervisor=nomad"
        OTEL_SERVICE_NAME = "spicedb"
        VERSELF_SUPERVISOR = "nomad"
      }
      resources {
        cpu = 500
        memory = 1024
      }
      template {
        destination = "secrets/spicedb.env"
        perms = "0600"
        env = true
        change_mode = "restart"
        data = <<EOH
SPICEDB_GRPC_PRESHARED_KEY={{ with secret "kv-runtime/data/secret/org/iam-service.spicedb.grpc_preshared_key" }}{{ .Data.data.value | toJSON }}{{ end }}
EOH
      }
      restart {
        attempts = 3
        delay = "15s"
        interval = "300s"
        mode = "delay"
      }
      service {
        name = "spicedb-grpc"
        port = "grpc"
        provider = "nomad"
        address_mode = "auto"
        check {
          name = "spicedb-grpc-tcp"
          type = "tcp"
          port = "grpc"
          interval = "1s"
          timeout = "3s"
        }
      }
      service {
        name = "spicedb-metrics"
        port = "metrics"
        provider = "nomad"
        address_mode = "auto"
        check {
          name = "spicedb-metrics-http"
          type = "http"
          path = "/metrics"
          port = "metrics"
          interval = "1s"
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
      canary = 1
      auto_revert = true
      auto_promote = true
    }
  }
}
