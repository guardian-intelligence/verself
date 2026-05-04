job "company" {
  name = "company"
  datacenters = ["dc1"]
  type = "service"
  group "company" {
    count = 2
    network {
      mode = "host"
      port "http" {
        host_network = "loopback"
      }
    }
    task "company" {
      driver = "raw_exec"
      user = "company"
      kill_signal = "SIGTERM"
      kill_timeout = "30s"
      artifact {
        source = "verself-artifact://company"
        destination = "local"
        chown = true
      }
      artifact {
        source = "verself-artifact://nodejs-runtime"
        destination = "local"
        chown = true
      }
      config {
        command = "local/bin/company"
      }
      env {
        BASE_URL = "https://guardianintelligence.org"
        COMPANY_DOMAIN = "guardianintelligence.org"
        HOME = "/var/lib/company"
        HOST = "127.0.0.1"
        NODE_ENV = "production"
        OTEL_EXPORTER_OTLP_ENDPOINT = "http://127.0.0.1:4318"
        OTEL_RESOURCE_ATTRIBUTES = "verself.supervisor=nomad"
        OTEL_SERVICE_NAME = "company"
        PORT = "$${NOMAD_PORT_http}"
        PRODUCT_BASE_URL = "https://verself.sh"
        SITE_ORIGIN = "https://guardianintelligence.org"
        VERSELF_DOMAIN = "verself.sh"
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
        name = "company-http"
        port = "http"
        provider = "nomad"
        address_mode = "auto"
        check {
          name = "company-http-http"
          type = "http"
          path = "/"
          port = "http"
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
