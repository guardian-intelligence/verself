job "postgresql-disaster-recovery" {
  name = "postgresql-disaster-recovery"
  datacenters = ["*"]
  type = "batch"

  group "postgresql-disaster-recovery" {
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
        ]
      }

      resources {
        cpu    = 50
        memory = 32
      }
    }

    task "prepare-postgresql" {
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
          "-o", "postgres",
          "-g", "postgres",
          "-m", "0700",
          "/var/lib/verself/recovery-cache/dr/v1/sites/__VERSELF_SITE__/components/postgresql/mechanisms/pg_dumpall_logical/points",
          "/var/lib/verself/recovery-verify/postgresql",
        ]
      }

      resources {
        cpu    = 50
        memory = 32
      }
    }

    task "postgresql-recovery" {
      driver = "raw_exec"
      user   = "postgres"

      artifact {
        source      = "__VERSELF_POSTGRESQL_RECOVERY_ARTIFACT__"
        destination = "local"
        chown       = true
      }

      artifact {
        source      = "__VERSELF_POSTGRESQL_RUNTIME_ARTIFACT__"
        destination = "local"
        chown       = true
      }

      config {
        command = "local/bin/postgresql-recovery"
        args = [
          "--action=__VERSELF_POSTGRESQL_RECOVERY_ACTION__",
          "--site=__VERSELF_SITE__",
          "--cache-root=/var/lib/verself/recovery-cache",
          "--verify-root=/var/lib/verself/recovery-verify/postgresql",
          "--runtime-root=local/opt/verself/postgresql",
          "--point=__VERSELF_POSTGRESQL_RECOVERY_POINT__",
        ]
      }

      env {
        OTEL_EXPORTER_OTLP_ENDPOINT = "http://127.0.0.1:4317"
        OTEL_RESOURCE_ATTRIBUTES = "verself.supervisor=nomad"
        OTEL_SERVICE_NAME = "postgresql-recovery"
        VERSELF_SUPERVISOR = "nomad"
      }

      resources {
        cpu    = 700
        memory = 1024
      }
    }
  }
}
