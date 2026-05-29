job "auth-control-plane" {
  name = "auth-control-plane"
  datacenters = ["dc1"]
  type = "batch"

  group "auth-control-plane" {
    count = 1

    meta {
      verself_group_kind = "batch"
    }

    task "auth-control-plane" {
      driver = "raw_exec"
      user = "root"

      artifact {
        source = "verself-artifact://auth-control-plane-apply"
        destination = "local"
        chown = true
      }

      config {
        command = "local/bin/auth-control-plane-apply"
      }

      env {
        AUTH_CONTROL_PLANE_ADMIN_PAT_PATH = "/etc/zitadel/admin.pat"
        AUTH_CONTROL_PLANE_IAM_CREDSTORE_DIR = "/etc/credstore/iam-service"
        AUTH_CONTROL_PLANE_IAM_CREDSTORE_GROUP = "iam_service"
        AUTH_CONTROL_PLANE_VERSELF_DOMAIN = "verself.sh"
        AUTH_CONTROL_PLANE_ZITADEL_BASE_URL = "http://127.0.0.1:8085"
        AUTH_CONTROL_PLANE_ZITADEL_HOST = "verself.sh"
        # Sign in with GitHub. Files are rendered from SOPS by the Zitadel
        # Ansible role; empty/absent files skip GitHub IdP provisioning.
        AUTH_CONTROL_PLANE_GITHUB_LOGIN_CLIENT_ID_PATH = "/etc/credstore/zitadel/github-login-client-id"
        AUTH_CONTROL_PLANE_GITHUB_LOGIN_CLIENT_SECRET_PATH = "/etc/credstore/zitadel/github-login-client-secret"
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
        attempts = 2
        delay = "5s"
        interval = "60s"
        mode = "fail"
      }
    }
  }
}
