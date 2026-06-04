job "clickhouse-disaster-recovery" {
  name        = "clickhouse-disaster-recovery"
  datacenters = ["*"]
  type        = "batch"

  group "clickhouse-disaster-recovery" {
    count = 1

    restart {
      attempts = 0
      mode     = "fail"
    }

    reschedule {
      attempts = 0
    }

    task "prepare-root" {
      driver = "raw_exec"
      user   = "root"

      lifecycle {
        hook    = "prestart"
        sidecar = false
      }

      config {
        command = "/usr/bin/install"
        args = [
          "-d",
          "-o", "root",
          "-g", "root",
          "-m", "0755",
          "/var/lib/verself",
          "/var/lib/verself/recovery-cache",
          "/var/lib/verself/recovery-cache/clickhouse",
        ]
      }

      resources {
        cpu    = 50
        memory = 32
      }
    }

    task "prepare-clickhouse" {
      driver = "raw_exec"
      user   = "root"

      lifecycle {
        hook    = "prestart"
        sidecar = false
      }

      config {
        command = "/usr/bin/install"
        args = [
          "-d",
          "-o", "clickhouse_operator",
          "-g", "clickhouse_operator",
          "-m", "0700",
          "/var/lib/verself/recovery-cache/clickhouse",
          "/var/lib/verself/recovery-cache/clickhouse/dr/v1/sites/__VERSELF_SITE__/components/clickhouse/mechanisms/native_disk_full/points",
        ]
      }

      resources {
        cpu    = 50
        memory = 32
      }
    }

    task "clickhouse-recovery" {
      driver = "raw_exec"
      user   = "clickhouse_operator"

      artifact {
        source      = "__VERSELF_CLICKHOUSE_RECOVERY_ARTIFACT__"
        destination = "local"
        chown       = true
      }

      config {
        command = "local/bin/clickhouse-recovery"
        args = [
          "--action=__VERSELF_CLICKHOUSE_RECOVERY_ACTION__",
          "--site=__VERSELF_SITE__",
          "--database=__VERSELF_CLICKHOUSE_RECOVERY_DATABASE__",
          "--point=__VERSELF_CLICKHOUSE_RECOVERY_POINT__",
          "--backup-disk=verself_recovery_backups",
          "--cache-root=/var/lib/verself/recovery-cache/clickhouse",
          "--operator-config=/etc/clickhouse-client/operator.xml",
        ]
      }

      env {
        OTEL_EXPORTER_OTLP_ENDPOINT = "http://127.0.0.1:4317"
        OTEL_RESOURCE_ATTRIBUTES    = "verself.supervisor=nomad"
        OTEL_SERVICE_NAME           = "clickhouse-recovery"
        VERSELF_SUPERVISOR          = "nomad"
      }

      resources {
        cpu    = 500
        memory = 256
      }
    }
  }
}
