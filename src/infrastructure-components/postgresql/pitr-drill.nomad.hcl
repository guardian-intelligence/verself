job "postgresql-pitr-drill" {
  name = "postgresql-pitr-drill"
  datacenters = ["*"]
  type = "batch"

  group "postgresql-pitr-drill" {
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
          "/var/lib/verself/recovery-drill",
        ]
      }

      resources {
        cpu    = 50
        memory = 32
      }
    }

    task "postgresql-pitr-drill" {
      driver = "raw_exec"
      user   = "root"

      artifact {
        source      = "__VERSELF_POSTGRESQL_PITR_DRILL_ARTIFACT__"
        destination = "local"
        chown       = false
      }

      artifact {
        source      = "__VERSELF_POSTGRESQL_RECOVERY_RUNTIME_ARTIFACT__"
        destination = "local"
        chown       = false
      }

      config {
        command = "local/bin/postgresql-pitr-drill"
        args = [
          "--site=__VERSELF_SITE__",
          "--drills=__VERSELF_POSTGRESQL_PITR_DRILLS__",
          "--runtime-root=local/opt/verself/postgresql-recovery",
          "--drill-root=/var/lib/verself/recovery-drill/postgresql",
          "--cache-root=/var/lib/verself/recovery-cache",
          "--writer-seconds=__VERSELF_POSTGRESQL_PITR_WRITER_SECONDS__",
          "--writer-interval=__VERSELF_POSTGRESQL_PITR_WRITER_INTERVAL__",
          "--archive-timeout=__VERSELF_POSTGRESQL_PITR_ARCHIVE_TIMEOUT__",
          "--archive-boundary=__VERSELF_POSTGRESQL_PITR_ARCHIVE_BOUNDARY__",
        ]
      }

      env {
        OTEL_EXPORTER_OTLP_ENDPOINT = "http://127.0.0.1:4317"
        OTEL_RESOURCE_ATTRIBUTES = "verself.supervisor=nomad"
        OTEL_SERVICE_NAME = "postgresql-pitr-drill"
        VERSELF_SUPERVISOR = "nomad"
      }

      resources {
        cpu    = 1000
        memory = 1024
      }
    }
  }
}
