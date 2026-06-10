job "verself-web" {
  name = "verself-web"
  datacenters = ["*"]
  type = "service"
  group "verself-web" {
    count = 2
    network {
      mode = "host"
      port "http" {
        host_network = "loopback"
      }
    }
    task "verself-web" {
      driver = "raw_exec"
      user = "verself-web"
      kill_signal = "SIGTERM"
      kill_timeout = "30s"
      vault {
        role = "verself-web-runtime"
      }
      identity {
        name = "vault_default"
        aud  = ["vault.io"]
        ttl  = "1h"
      }
      artifact {
        source = "verself-artifact://verself-web"
        destination = "local"
        chown = true
      }
      artifact {
        source = "verself-artifact://nodejs-runtime"
        destination = "local"
        chown = true
      }
      config {
        command = "local/bin/verself-web"
      }
      env {
        HOME = "/var/lib/verself-web"
        HOST = "127.0.0.1"
        NODE_ENV = "production"
        OTEL_EXPORTER_OTLP_ENDPOINT = "http://127.0.0.1:4318"
        OTEL_RESOURCE_ATTRIBUTES = "deployment.environment.name=__VERSELF_SITE__,verself.site=__VERSELF_SITE__,verself.supervisor=nomad"
        OTEL_SERVICE_NAME = "verself-web"
        PORT = "$${NOMAD_PORT_http}"
        PRODUCT_BASE_URL = "__VERSELF_PRODUCT_BASE_URL__"
        VERSELF_DOMAIN = "__VERSELF_PRODUCT_DOMAIN__"
        VERSELF_CRED_ELECTRIC_IAM_API_SECRET = "$${NOMAD_SECRETS_DIR}/electric-iam-api-secret"
        VERSELF_SITE = "__VERSELF_SITE__"
        VERSELF_SUPERVISOR = "nomad"
      }
      resources {
        cpu = 500
        memory = 512
      }
      restart {
        attempts = 3
        delay = "15s"
        interval = "300s"
        mode = "delay"
      }
      service {
        name = "verself-web-http"
        port = "http"
        provider = "nomad"
        address_mode = "auto"
        check {
          name = "verself-web-http-http"
          type = "http"
          path = "/readyz"
          port = "http"
          interval = "1s"
          timeout = "3s"
        }
      }
      template {
        change_mode = "restart"
        destination = "secrets/electric-iam-api-secret"
        perms = "0600"
        data = <<-EOT
{{ with secret "kv-runtime/data/secret/org/electric-iam.api_secret" }}{{ .Data.data.value }}{{ end }}
EOT
      }
      template {
        # Env templates only apply at task start, so upstream churn must
        # restart the task; splay staggers the two allocs. A missing service
        # renders an empty URL and the app fails fast at startup.
        change_mode = "restart"
        splay = "30s"
        destination = "secrets/upstreams.env"
        data = <<-EOT
BILLING_SERVICE_BASE_URL=http://{{- with nomadService "billing-public-http" }}{{ with index . 0 }}{{ .Address }}:{{ .Port }}{{ end }}{{- end }}
ELECTRIC_BASE_URL=http://{{- with nomadService "electric-http" }}{{ with index . 0 }}{{ .Address }}:{{ .Port }}{{ end }}{{- end }}
ELECTRIC_IAM_BASE_URL=http://{{- with nomadService "electric-iam-http" }}{{ with index . 0 }}{{ .Address }}:{{ .Port }}{{ end }}{{- end }}
GOVERNANCE_SERVICE_BASE_URL=http://{{- with nomadService "governance-service-public-http" }}{{ with index . 0 }}{{ .Address }}:{{ .Port }}{{ end }}{{- end }}
IAM_SERVICE_BASE_URL=http://{{- with nomadService "iam-service-public-http" }}{{ with index . 0 }}{{ .Address }}:{{ .Port }}{{ end }}{{- end }}
NOTIFICATIONS_SERVICE_BASE_URL=http://{{- with nomadService "notifications-service-public-http" }}{{ with index . 0 }}{{ .Address }}:{{ .Port }}{{ end }}{{- end }}
PROFILE_SERVICE_BASE_URL=http://{{- with nomadService "profile-service-public-http" }}{{ with index . 0 }}{{ .Address }}:{{ .Port }}{{ end }}{{- end }}
PROJECTS_SERVICE_BASE_URL=http://{{- with nomadService "projects-service-public-http" }}{{ with index . 0 }}{{ .Address }}:{{ .Port }}{{ end }}{{- end }}
SANDBOX_RENTAL_SERVICE_BASE_URL=http://{{- with nomadService "sandbox-rental-public-http" }}{{ with index . 0 }}{{ .Address }}:{{ .Port }}{{ end }}{{- end }}
SOURCE_CODE_HOSTING_SERVICE_BASE_URL=http://{{- with nomadService "source-code-hosting-service-public-http" }}{{ with index . 0 }}{{ .Address }}:{{ .Port }}{{ end }}{{- end }}
EOT
        env = true
      }
    }
    update {
      # No canary: it needs +1 alloc of headroom, which deadlocks updates on a
      # memory-reserved single-node site; rolling replacement stays in budget.
      max_parallel = 1
      health_check = "checks"
      min_healthy_time = "3s"
      healthy_deadline = "300s"
      progress_deadline = "600s"
      auto_revert = true
    }
  }
}
