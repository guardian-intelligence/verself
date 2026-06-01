job "secrets-service" {
  name = "secrets-service"
  datacenters = ["*"]
  type = "service"
  group "secrets-service" {
    count = 2
    network {
      mode = "host"
      port "internal_https" {
        host_network = "loopback"
      }
      port "public_http" {
        host_network = "loopback"
      }
    }
    task "secrets-service" {
      driver = "raw_exec"
      user = "secrets_service"
      kill_signal = "SIGTERM"
      kill_timeout = "30s"
      shutdown_delay = "5s"
      vault {
        role = "secrets-service-runtime"
      }

      identity {
        name = "vault_default"
        aud  = ["vault.io"]
        ttl  = "1h"
      }

      artifact {
        source = "verself-artifact://secrets-service"
        destination = "local"
        chown = true
      }
      config {
        command = "local/bin/secrets-service"
      }
      env {
        OTEL_EXPORTER_OTLP_ENDPOINT = "http://127.0.0.1:4317"
        OTEL_RESOURCE_ATTRIBUTES = "verself.supervisor=nomad"
        OTEL_SERVICE_NAME = "secrets-service"
        SECRETS_OPENBAO_JWT_PREFIX = "jwt"
        SECRETS_OPENBAO_KV_PREFIX = "kv"
        SECRETS_OPENBAO_SPIFFE_JWT_PREFIX = "spiffe-jwt"
        SECRETS_OPENBAO_TRANSIT_PREFIX = "transit"
        SECRETS_OPENBAO_WORKLOAD_AUDIENCE = "openbao"
        SECRETS_RUNTIME_SECRET_NAMESPACE = "runtime"
        SPIFFE_ENDPOINT_SOCKET = "unix:///run/spire-agent/sockets/agent.sock"
        VERSELF_AUTH_ISSUER_URL = "__VERSELF_AUTH_ISSUER_URL__"
        VERSELF_CRED_OPENBAO_CA_CERT = "/etc/verself/openbao/ca.pem"
        VERSELF_INSTALLATION_ID = "__VERSELF_INSTALLATION_ID__"
        VERSELF_INTERNAL_LISTEN_ADDR = "127.0.0.1:$${NOMAD_PORT_internal_https}"
        VERSELF_LISTEN_ADDR = "127.0.0.1:$${NOMAD_PORT_public_http}"
        VERSELF_SUPERVISOR = "nomad"
      }
      template {
        change_mode = "restart"
        destination = "secrets/auth-audience.env"
        perms = "0600"
        env = true
        data = <<-EOT
VERSELF_CRED_VALUE_AUTH_AUDIENCE={{ with secret "kv-runtime/data/secret/org/iam-service.zitadel.auth_audience" }}{{ .Data.data.value | toJSON }}{{ end }}
EOT
      }
      resources {
        cpu = 500
        memory = 256
      }
      restart {
        attempts = 3
        delay = "15s"
        interval = "300s"
        mode = "delay"
      }
      service {
        name = "secrets-service-internal-https"
        port = "internal_https"
        provider = "nomad"
        address_mode = "auto"
        check {
          name = "secrets-service-tcp-internal_https"
          type = "tcp"
          port = "internal_https"
          interval = "1s"
          timeout = "3s"
        }
      }
      service {
        name = "secrets-service-public-http"
        port = "public_http"
        provider = "nomad"
        address_mode = "auto"
        check {
          name = "secrets-service-http-public_http"
          type = "http"
          path = "/readyz"
          port = "public_http"
          interval = "1s"
          timeout = "3s"
        }
      }
      template {
        change_mode = "restart"
        destination = "secrets/upstreams.env"
        perms = "0600"
        data = <<-EOT
SECRETS_OPENBAO_ADDR=https://{{- with nomadService "openbao-api" }}{{ with index . 0 }}{{ .Address }}:{{ .Port }}{{ end }}{{- else }}127.0.0.1:1{{- end }}
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
