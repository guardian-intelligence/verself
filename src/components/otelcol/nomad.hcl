job "otelcol" {
  name = "otelcol"
  datacenters = ["dc1"]
  type = "service"

  group "otelcol" {
    count = 1

    network {
      port "otlp_grpc" {
        static = 4317
        to = 4317
      }
      port "otlp_http" {
        static = 4318
        to = 4318
      }
      port "health" {
        static = 13133
        to = 13133
      }
    }

    task "collector" {
      driver = "raw_exec"
      user = "otelcol"

      config {
        command = "/opt/verself/profile/bin/otelcol-contrib"
        args = ["--config", "/etc/otelcol/config.yaml"]
      }

      env {
        OTEL_RESOURCE_ATTRIBUTES = "verself.supervisor=nomad"
        OTEL_SERVICE_NAME = "otelcol"
        VERSELF_SUPERVISOR = "nomad"
      }

      resources {
        cpu = 600
        memory = 1024
      }

      service {
        name = "otelcol-otlp-grpc"
        port = "otlp_grpc"
        address_mode = "auto"
      }

      service {
        name = "otelcol-otlp-http"
        port = "otlp_http"
        address_mode = "auto"
      }
    }
  }
}
