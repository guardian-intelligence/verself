job "substrate-control-plane" {
  name = "substrate-control-plane"
  datacenters = ["*"]
  type = "batch"

  parameterized {
    payload = "required"
    meta_required = ["deploy_run_key", "site", "sha"]
  }

  group "substrate-control-plane" {
    count = 1

    task "apply" {
      driver = "raw_exec"
      user = "root"

      vault {
        role = "substrate-control-plane"
      }

      identity {
        name = "vault_default"
        aud  = ["vault.io"]
        ttl  = "1h"
      }

      artifact {
        source = "verself-artifact://substrate-control-plane-apply"
        destination = "local"
        chown = true
      }

      artifact {
        source = "verself-artifact://postgresql-runtime"
        destination = "local/postgresql"
        chown = true
      }

      dispatch_payload {
        file = "bundle.json"
      }

      config {
        command = "local/bin/substrate-control-plane-apply"
        args = ["--bundle=$${NOMAD_TASK_DIR}/bundle.json"]
      }

      env {
        BAO_ADDR = "https://127.0.0.1:8200"
        BAO_CACERT = "/etc/openbao/tls/cert.pem"
        OTEL_EXPORTER_OTLP_ENDPOINT = "http://127.0.0.1:4317"
        OTEL_RESOURCE_ATTRIBUTES = "verself.supervisor=nomad"
        OTEL_SERVICE_NAME = "substrate-control-plane-apply"
        VERSELF_POSTGRESQL_RUNTIME = "$${NOMAD_TASK_DIR}/postgresql/opt/verself/postgresql"
        VERSELF_SUPERVISOR = "nomad"
      }

      resources {
        cpu = 100
        memory = 128
      }

      restart {
        attempts = 12
        delay = "10s"
        interval = "5m"
        mode = "fail"
      }
    }
  }
}
