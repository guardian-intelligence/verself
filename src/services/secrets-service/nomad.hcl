job "secrets-service" {
  name = "secrets-service"
  datacenters = ["dc1"]
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
        SECRETS_OPENBAO_ADDR = "https://127.0.0.1:8200"
        SECRETS_OPENBAO_JWT_PREFIX = "jwt"
        SECRETS_OPENBAO_KV_PREFIX = "kv"
        SECRETS_OPENBAO_SPIFFE_JWT_PREFIX = "spiffe-jwt"
        SECRETS_OPENBAO_TRANSIT_PREFIX = "transit"
        SECRETS_OPENBAO_WORKLOAD_AUDIENCE = "openbao"
        SECRETS_RUNTIME_SECRET_NAMESPACE = "runtime"
        SPIFFE_ENDPOINT_SOCKET = "unix:///run/spire-agent/sockets/agent.sock"
        VERSELF_AUTH_ISSUER_URL = "https://verself.sh"
        VERSELF_CRED_AUTH_AUDIENCE = "/etc/credstore/secrets-service/auth-audience"
        VERSELF_CRED_OPENBAO_CA_CERT = "/etc/credstore/secrets-service/openbao-ca-cert"
        VERSELF_INSTALLATION_ID = "inst_5NZSEA08R8P3HN566DNH8D301M"
        VERSELF_INTERNAL_LISTEN_ADDR = "127.0.0.1:$${NOMAD_PORT_internal_https}"
        VERSELF_LISTEN_ADDR = "127.0.0.1:$${NOMAD_PORT_public_http}"
        VERSELF_SUPERVISOR = "nomad"
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
