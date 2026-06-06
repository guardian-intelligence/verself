variable "guardian_repo_root" {
  type    = string
  default = "/home/ubuntu/.local/state/guardian/repo/current"
}

job "zitadel" {
  name = "zitadel"
  datacenters = ["*"]
  type = "service"

  group "zitadel" {
    count = 1

    network {
      mode = "host"
      port "http" {
        host_network = "loopback"
        static = 8085
        to = 8085
      }
    }

    task "setup" {
      driver = "raw_exec"
      user = "root"
      lifecycle {
        hook = "prestart"
        sidecar = false
      }

      vault {
        env  = false
        role = "zitadel-runtime"
      }

      identity {
        name = "vault_default"
        aud  = ["vault.io"]
        ttl  = "1h"
      }

      config {
        command = "${var.guardian_repo_root}/bazel-bin/src/infrastructure-components/zitadel/cmd/zitadel-setup-apply/zitadel-setup-apply_/zitadel-setup-apply"
        args = [
          "--repo-root=${var.guardian_repo_root}",
          "--resource-graph=${var.guardian_repo_root}/workspace/.guardian/fly/document.json",
          "--resource-name=zitadel",
          "--openbao-token-file=$${NOMAD_SECRETS_DIR}/vault_token",
        ]
      }

      env {
        OTEL_EXPORTER_OTLP_ENDPOINT = "http://127.0.0.1:4317"
        OTEL_RESOURCE_ATTRIBUTES = "verself.supervisor=nomad"
        OTEL_SERVICE_NAME = "zitadel-setup-apply"
        VERSELF_SUPERVISOR = "nomad"
      }

      resources {
        cpu = 200
        memory = 256
      }
    }

    task "server" {
      driver = "raw_exec"
      user = "root"

      vault {
        env  = false
        role = "zitadel-runtime"
      }

      identity {
        name = "vault_default"
        aud  = ["vault.io"]
        ttl  = "1h"
      }

      config {
        command = "${var.guardian_repo_root}/bazel-bin/src/infrastructure-components/zitadel/cmd/zitadel-setup-apply/zitadel-setup-apply_/zitadel-setup-apply"
        args = [
          "--mode=start",
          "--repo-root=${var.guardian_repo_root}",
          "--resource-graph=${var.guardian_repo_root}/workspace/.guardian/fly/document.json",
          "--resource-name=zitadel",
          "--openbao-token-file=$${NOMAD_SECRETS_DIR}/vault_token",
        ]
      }

      env {
        OTEL_EXPORTER_OTLP_ENDPOINT = "http://127.0.0.1:4317"
        OTEL_RESOURCE_ATTRIBUTES = "verself.supervisor=nomad"
        OTEL_SERVICE_NAME = "zitadel"
        VERSELF_SUPERVISOR = "nomad"
      }

      resources {
        cpu = 700
        memory = 1024
      }

      service {
        name = "zitadel-http"
        port = "http"
        provider = "nomad"
        address_mode = "auto"
      }
    }
  }
}
