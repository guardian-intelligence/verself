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

      artifact {
        source = "verself-artifact://auth-control-plane-apply"
        destination = "local"
      }

      config {
        command = "local/bin/auth-control-plane-apply"
        args = [
          "--admin-pat-file=$${NOMAD_SECRETS_DIR}/admin-pat",
          "--github-login-client-secret-file=$${NOMAD_SECRETS_DIR}/github-login-client-secret",
          "--openbao-token-file=$${NOMAD_SECRETS_DIR}/vault_token",
        ]
      }

      env {
        AUTH_CONTROL_PLANE_IAM_SERVICE_DOMAIN = "__VERSELF_IAM_SERVICE_DOMAIN__"
        AUTH_CONTROL_PLANE_VERSELF_DOMAIN = "__VERSELF_PRODUCT_DOMAIN__"
        AUTH_CONTROL_PLANE_ZITADEL_BASE_URL = "http://127.0.0.1:8085"
        AUTH_CONTROL_PLANE_ZITADEL_HOST = "__VERSELF_PRODUCT_DOMAIN__"
        AUTH_CONTROL_PLANE_GITHUB_LOGIN_CLIENT_ID = "__VERSELF_GITHUB_OAUTH_CLIENT_ID__"
        BAO_ADDR = "https://127.0.0.1:8200"
        BAO_CACERT = "/etc/verself/openbao/ca.pem"
        OTEL_EXPORTER_OTLP_ENDPOINT = "http://127.0.0.1:4317"
        OTEL_RESOURCE_ATTRIBUTES = "verself.supervisor=nomad"
        OTEL_SERVICE_NAME = "auth-control-plane-apply"
        VERSELF_SUPERVISOR = "nomad"
      }

      template {
        change_mode = "restart"
        destination = "secrets/admin-pat"
        perms = "0600"
        data = <<-EOT
{{ with secret "kv-runtime/data/secret/org/auth-control-plane.zitadel.admin_token" }}{{ .Data.data.value }}{{ end }}
EOT
      }

      template {
        change_mode = "restart"
        destination = "secrets/github-login-client-secret"
        perms = "0600"
        data = <<-EOT
{{ with secret "kv-runtime/data/secret/org/auth-control-plane.github_login.oauth_client_secret" }}{{ .Data.data.value }}{{ end }}
EOT
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
