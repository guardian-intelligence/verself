job "otelcol" {
  name = "otelcol"
  datacenters = ["dc1"]
  type = "service"

  group "otelcol" {
    count = 1

    network {
      mode = "host"
      port "otlp_grpc" {
        host_network = "loopback"
        static = 4317
        to = 4317
      }
      port "otlp_http" {
        host_network = "loopback"
        static = 4318
        to = 4318
      }
      port "health" {
        host_network = "loopback"
        static = 13133
        to = 13133
      }
    }

    task "clickhouse-spiffe-helper" {
      driver = "raw_exec"
      user = "otelcol"

      lifecycle {
        hook = "prestart"
        sidecar = true
      }

      config {
        command = "/opt/verself/profile/bin/spiffe-helper"
        args = ["-config", "/etc/otelcol/clickhouse-spiffe-helper.conf"]
      }

      resources {
        cpu = 50
        memory = 64
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
        provider = "nomad"
        address_mode = "auto"
      }

      service {
        name = "otelcol-otlp-http"
        port = "otlp_http"
        provider = "nomad"
        address_mode = "auto"
      }
    }
  }
}
