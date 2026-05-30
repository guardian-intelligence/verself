job "clickhouse-host-install" {
  name = "clickhouse-host-install"
  datacenters = ["dc1"]
  type = "batch"

  group "clickhouse-host-install" {
    count = 1

    restart {
      attempts = 0
      mode = "fail"
    }

    reschedule {
      attempts = 0
    }

    task "install" {
      driver = "raw_exec"
      user = "root"

      config {
        command = "$${VERSELF_CLICKHOUSE_HOST_INSTALL}/opt/verself/profile/bin/clickhouse-host-install"
        args = ["--artifact-opt", "$${VERSELF_CLICKHOUSE_HOST_INSTALL}/opt"]
      }

      env {
        OTEL_EXPORTER_OTLP_ENDPOINT = "http://127.0.0.1:4317"
        OTEL_RESOURCE_ATTRIBUTES = "verself.supervisor=nomad"
        OTEL_SERVICE_NAME = "clickhouse-host-install"
        VERSELF_CLICKHOUSE_HOST_INSTALL = "verself-artifact://clickhouse-host-install"
        VERSELF_SUPERVISOR = "nomad"
      }

      resources {
        cpu = 100
        memory = 128
      }
    }
  }
}
