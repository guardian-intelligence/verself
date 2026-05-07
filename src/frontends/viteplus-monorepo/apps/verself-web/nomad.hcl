job "verself-web" {
  name = "verself-web"
  datacenters = ["dc1"]
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
        ELECTRIC_BASE_URL = "http://127.0.0.1:3010"
        ELECTRIC_NOTIFICATIONS_BASE_URL = "http://127.0.0.1:3012"
        HOME = "/var/lib/verself-web"
        HOST = "127.0.0.1"
        NODE_ENV = "production"
        OTEL_EXPORTER_OTLP_ENDPOINT = "http://127.0.0.1:4318"
        OTEL_RESOURCE_ATTRIBUTES = "verself.supervisor=nomad"
        OTEL_SERVICE_NAME = "verself-web"
        PORT = "$${NOMAD_PORT_http}"
        PRODUCT_BASE_URL = "https://verself.sh"
        VERSELF_CRED_ELECTRIC_API_SECRET = "/etc/credstore/verself-web/electric-api-secret"
        VERSELF_CRED_ELECTRIC_NOTIFICATIONS_API_SECRET = "/etc/credstore/verself-web/electric-notifications-api-secret"
        VERSELF_CRED_BILLING_SERVICE_AUTH_AUDIENCE = "/etc/credstore/verself-web/billing-service-auth-audience"
        VERSELF_CRED_GOVERNANCE_SERVICE_AUTH_AUDIENCE = "/etc/credstore/verself-web/governance-service-auth-audience"
        VERSELF_CRED_IAM_SERVICE_AUTH_AUDIENCE = "/etc/credstore/verself-web/iam-service-auth-audience"
        VERSELF_CRED_NOTIFICATIONS_SERVICE_AUTH_AUDIENCE = "/etc/credstore/verself-web/notifications-service-auth-audience"
        VERSELF_CRED_PROFILE_SERVICE_AUTH_AUDIENCE = "/etc/credstore/verself-web/profile-service-auth-audience"
        VERSELF_CRED_PROJECTS_SERVICE_AUTH_AUDIENCE = "/etc/credstore/verself-web/projects-service-auth-audience"
        VERSELF_CRED_SANDBOX_RENTAL_SERVICE_AUTH_AUDIENCE = "/etc/credstore/verself-web/sandbox-rental-auth-audience"
        VERSELF_CRED_SOURCE_CODE_HOSTING_SERVICE_AUTH_AUDIENCE = "/etc/credstore/verself-web/source-code-hosting-service-auth-audience"
        VERSELF_DOMAIN = "verself.sh"
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
          path = "/"
          port = "http"
          interval = "1s"
          timeout = "3s"
        }
      }
      template {
        change_mode = "restart"
        destination = "secrets/upstreams.env"
        data = <<-EOT
BILLING_SERVICE_BASE_URL=http://{{- with nomadService "billing-service-public-http" }}{{ with index . 0 }}{{ .Address }}:{{ .Port }}{{ end }}{{- else }}127.0.0.1:1{{- end }}
GOVERNANCE_SERVICE_BASE_URL=http://{{- with nomadService "governance-service-public-http" }}{{ with index . 0 }}{{ .Address }}:{{ .Port }}{{ end }}{{- else }}127.0.0.1:1{{- end }}
IAM_SERVICE_BASE_URL=http://{{- with nomadService "iam-service-public-http" }}{{ with index . 0 }}{{ .Address }}:{{ .Port }}{{ end }}{{- else }}127.0.0.1:1{{- end }}
NOTIFICATIONS_SERVICE_BASE_URL=http://{{- with nomadService "notifications-service-public-http" }}{{ with index . 0 }}{{ .Address }}:{{ .Port }}{{ end }}{{- else }}127.0.0.1:1{{- end }}
PROFILE_SERVICE_BASE_URL=http://{{- with nomadService "profile-service-public-http" }}{{ with index . 0 }}{{ .Address }}:{{ .Port }}{{ end }}{{- else }}127.0.0.1:1{{- end }}
PROJECTS_SERVICE_BASE_URL=http://{{- with nomadService "projects-service-public-http" }}{{ with index . 0 }}{{ .Address }}:{{ .Port }}{{ end }}{{- else }}127.0.0.1:1{{- end }}
SANDBOX_RENTAL_SERVICE_BASE_URL=http://{{- with nomadService "sandbox-rental-public-http" }}{{ with index . 0 }}{{ .Address }}:{{ .Port }}{{ end }}{{- else }}127.0.0.1:1{{- end }}
SOURCE_CODE_HOSTING_SERVICE_BASE_URL=http://{{- with nomadService "source-code-hosting-service-public-http" }}{{ with index . 0 }}{{ .Address }}:{{ .Port }}{{ end }}{{- else }}127.0.0.1:1{{- end }}
EOT
        env = true
      }
    }
    update {
      max_parallel = 1
      health_check = "checks"
      min_healthy_time = "3s"
      healthy_deadline = "300s"
      progress_deadline = "600s"
      canary = 1
      auto_revert = true
      auto_promote = true
    }
  }
}
