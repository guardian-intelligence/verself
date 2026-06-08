variable "email_service_resend_keys_artifact_source" {
  type    = string
  default = ""
}

job "email-service-resend-keys" {
  name = "email-service-resend-keys"
  datacenters = ["*"]
  type = "batch"

  group "email-service-resend-keys" {
    count = 1

    task "apply" {
      driver = "raw_exec"
      user = "email_service"

      vault {
        env  = false
        role = "email-service-resend-keys-runtime"
      }

      identity {
        name = "vault_default"
        aud  = ["vault.io"]
        ttl  = "1h"
      }

      artifact {
        source = var.email_service_resend_keys_artifact_source
        destination = "local"
        chown = true
      }

      config {
        command = "local/bin/email-service"
        args = [
          "resend-keys",
          "apply",
          "--openbao-token-file=$${NOMAD_SECRETS_DIR}/vault_token",
        ]
      }

      env {
        BAO_ADDR = "https://127.0.0.1:8200"
        BAO_CACERT = "/etc/verself/openbao/ca.pem"
        OTEL_EXPORTER_OTLP_ENDPOINT = "http://127.0.0.1:4317"
        OTEL_RESOURCE_ATTRIBUTES = "verself.supervisor=nomad"
        OTEL_SERVICE_NAME = "email-service-resend-keys"
        VERSELF_SITE = "__VERSELF_SITE__"
        VERSELF_SUPERVISOR = "nomad"
      }

      resources {
        cpu = 100
        memory = 128
      }

      restart {
        attempts = 3
        delay = "10s"
        interval = "5m"
        mode = "fail"
      }
    }
  }
}
