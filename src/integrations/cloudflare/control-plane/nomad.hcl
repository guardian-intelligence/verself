job "cloudflare-integration-recovery" {
  name = "cloudflare-integration-recovery"
  datacenters = ["*"]
  type = "batch"

  group "cloudflare-integration-recovery" {
    count = 1

    task "recover" {
      driver = "raw_exec"
      user = "root"

      vault {
        env  = false
        role = "cloudflare-integration-recovery-runtime"
      }

      identity {
        name = "vault_default"
        aud  = ["vault.io"]
        ttl  = "1h"
      }

      artifact {
        source = "verself-artifact://cloudflare-control-plane-runtime"
        destination = "local"
      }

      config {
        command = "local/bin/cloudflare-control-plane"
        args = [
          "--action=recover",
          "--recovery-config=local/recovery/__VERSELF_SITE__.cloudflare-recovery.yml",
          "--timeout=10m",
        ]
      }

      env {
        BAO_ADDR = "https://127.0.0.1:8200"
        BAO_CACERT = "/etc/verself/openbao/ca.pem"
        OTEL_EXPORTER_OTLP_ENDPOINT = "http://127.0.0.1:4317"
        OTEL_RESOURCE_ATTRIBUTES = "verself.supervisor=nomad"
        OTEL_SERVICE_NAME = "cloudflare-integration-recovery"
        VERSELF_SITE = "__VERSELF_SITE__"
        VERSELF_SUPERVISOR = "nomad"
      }

      resources {
        cpu = 150
        memory = 128
      }

      restart {
        attempts = 20
        delay = "15s"
        interval = "10m"
        mode = "fail"
      }
    }
  }
}
