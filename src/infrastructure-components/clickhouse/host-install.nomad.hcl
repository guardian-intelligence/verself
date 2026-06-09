job "clickhouse-host-install" {
  name = "clickhouse-host-install"
  datacenters = ["*"]
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
        command = "local/verself-artifacts/clickhouse-host-install/opt/verself/profile/bin/clickhouse-host-apply"
        args = [
          "--artifact-root",
          "local/verself-artifacts/clickhouse-host-install",
          "--spiffe-service-prefix",
          "__VERSELF_SPIFFE_SERVICE_PREFIX__",
        ]
      }

      env {
        OTEL_EXPORTER_OTLP_ENDPOINT = "http://127.0.0.1:4317"
        OTEL_RESOURCE_ATTRIBUTES = "verself.supervisor=nomad"
        OTEL_SERVICE_NAME = "clickhouse-host-install"
        VERSELF_CLICKHOUSE_HOST_INSTALL = "verself-artifact://clickhouse-host-install"
        VERSELF_SPIFFE_SERVICE_PREFIX = "__VERSELF_SPIFFE_SERVICE_PREFIX__"
        VERSELF_SUPERVISOR = "nomad"
      }

      resources {
        cpu = 100
        memory = 128
      }
    }
  }
}
