job "credential-store-object-storage" {
  name = "credential-store-object-storage"
  datacenters = ["dc1"]
  type = "batch"

  group "credential-store-object-storage" {
    count = 1

    restart {
      attempts = 0
      mode = "fail"
    }

    reschedule {
      attempts = 0
    }

    task "apply" {
      driver = "raw_exec"
      user = "root"

      config {
        command = "$${VERSELF_CREDENTIAL_STORE_RUNTIME}/opt/verself/credential-store/bin/credential-store-apply"
        args = ["--profile", "object-storage"]
      }

      env {
        OTEL_EXPORTER_OTLP_ENDPOINT = "http://127.0.0.1:4317"
        OTEL_RESOURCE_ATTRIBUTES = "verself.supervisor=nomad"
        OTEL_SERVICE_NAME = "credential-store-object-storage"
        VERSELF_CREDENTIAL_STORE_RUNTIME = "verself-artifact://credential-store-runtime"
        VERSELF_SUPERVISOR = "nomad"
      }

      resources {
        cpu = 50
        memory = 128
      }
    }
  }
}
