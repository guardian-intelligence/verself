job "nomad-observer" {
  name = "nomad-observer"
  datacenters = ["*"]
  type = "service"

  group "observer" {
    count = 1

    task "observer" {
      driver = "raw_exec"
      # Runs as a dedicated identity (not nobody): it presents an x509-SVID to
      # write the fleet projection to ClickHouse over mTLS, and is a member of
      # the spire_workload group for agent-socket access.
      user = "nomad_observer"
      kill_signal = "SIGTERM"
      kill_timeout = "15s"

      artifact {
        source = "verself-artifact://nomad-observer"
        destination = "local"
        chown = true
      }

      config {
        command = "local/bin/nomad-observer"
      }

      env {
        NOMAD_ADDR = "http://127.0.0.1:4646"
        NOMAD_NAMESPACE = "default"
        NOMAD_OBSERVER_CAPTURE_WORKERS = "4"
        NOMAD_OBSERVER_CAPTURE_QUEUE_SIZE = "128"
        # Log capture uses Nomad streaming endpoints; keep it manual until the reader is bounded.
        NOMAD_OBSERVER_STDERR_TAIL_BYTES = "0"
        NOMAD_OBSERVER_STDOUT_TAIL_BYTES = "0"
        OTEL_EXPORTER_OTLP_ENDPOINT = "http://127.0.0.1:4317"
        OTEL_SERVICE_NAME = "nomad-observer"
        SPIFFE_ENDPOINT_SOCKET = "unix:///run/spire-agent/sockets/agent.sock"
        VERSELF_CLICKHOUSE_ADDRESS = "127.0.0.1:9440"
        VERSELF_CLICKHOUSE_USER = "nomad_observer"
        VERSELF_CRED_CLICKHOUSE_CA_CERT = "/etc/verself/clickhouse/server-ca.pem"
        VERSELF_SUPERVISOR = "nomad"
      }

      resources {
        cpu = 100
        memory = 128
      }

      restart {
        attempts = 5
        delay = "10s"
        interval = "300s"
        mode = "delay"
      }
    }

    update {
      max_parallel = 1
      health_check = "task_states"
      min_healthy_time = "5s"
      healthy_deadline = "120s"
      progress_deadline = "300s"
      auto_revert = true
    }
  }
}
