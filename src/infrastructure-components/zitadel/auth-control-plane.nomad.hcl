variable "guardian_repo_root" {
  type    = string
  default = "/home/ubuntu/.local/state/guardian/repo/current"
}

job "auth-control-plane" {
  name = "auth-control-plane"
  datacenters = ["*"]
  type = "batch"

  group "auth-control-plane" {
    count = 1

    task "auth-control-plane" {
      driver = "raw_exec"
      user = "root"

      vault {
        env  = false
        role = "auth-control-plane-runtime"
      }

      identity {
        name = "vault_default"
        aud  = ["vault.io"]
        ttl  = "1h"
      }

      config {
        command = "${var.guardian_repo_root}/bazel-bin/src/infrastructure-components/zitadel/cmd/auth-control-plane-apply/auth-control-plane-apply_/auth-control-plane-apply"
        args = [
          "--resource-graph=${var.guardian_repo_root}/workspace/.guardian/fly/document.json",
          "--resource-name=auth-control-plane",
          "--openbao-token-file=$${NOMAD_SECRETS_DIR}/vault_token",
        ]
      }

      env {
        OTEL_EXPORTER_OTLP_ENDPOINT = "http://127.0.0.1:4317"
        OTEL_RESOURCE_ATTRIBUTES = "verself.supervisor=nomad"
        OTEL_SERVICE_NAME = "auth-control-plane-apply"
        VERSELF_SUPERVISOR = "nomad"
      }

      resources {
        cpu = 100
        memory = 128
      }

      restart {
        attempts = 12
        delay = "10s"
        interval = "10m"
        mode = "fail"
      }
    }
  }
}
