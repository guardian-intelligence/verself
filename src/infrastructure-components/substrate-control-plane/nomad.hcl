job "substrate-control-plane" {
  name = "substrate-control-plane"
  datacenters = ["*"]
  type = "batch"

  parameterized {
    # Nomad dispatch payloads cap at 16 KiB; bundle bytes are fetched as an artifact.
    payload = "forbidden"
    meta_required = ["bundle_artifact_sha256", "bundle_source", "control_plane_bundle_sha256", "deploy_run_key", "site", "sha"]
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

      artifact {
        source = "$${NOMAD_META_bundle_source}"
        destination = "local/bundle.json.gz"
        mode = "file"
        options {
          archive = false
          checksum = "sha256:$${NOMAD_META_bundle_artifact_sha256}"
        }
      }

      config {
        command = "local/bin/substrate-control-plane-apply"
        args = ["--bundle=local/bundle.json.gz"]
      }

      env {
        BAO_ADDR = "https://127.0.0.1:8200"
        BAO_CACERT = "/etc/openbao/tls/cert.pem"
        OTEL_EXPORTER_OTLP_ENDPOINT = "http://127.0.0.1:4317"
        OTEL_RESOURCE_ATTRIBUTES = "verself.supervisor=nomad"
        OTEL_SERVICE_NAME = "substrate-control-plane-apply"
        VERSELF_OPENBAO_RUNTIME_SEED_FILE = "/etc/verself/bootstrap/openbao-runtime-seed.json"
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
