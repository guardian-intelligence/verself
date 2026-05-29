job "otelcol" {
  name = "otelcol"
  datacenters = ["dc1"]
  type = "service"

  group "otelcol" {
    count = 1

    meta {
      verself_group_kind = "service"
    }

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

      artifact {
        source = "verself-artifact://otelcol-config"
        destination = "local"
        chown = true
      }

      artifact {
        source = "verself-artifact://otelcol-runtime"
        destination = "local"
        chown = true
      }

      config {
        command = "local/bin/spiffe-helper"
        args = ["-config", "local/config/clickhouse-spiffe-helper.conf"]
      }

      resources {
        cpu = 50
        memory = 64
      }
    }

    task "collector" {
      driver = "raw_exec"
      user = "otelcol"

      artifact {
        source = "verself-artifact://otelcol-config"
        destination = "local"
        chown = true
      }

      artifact {
        source = "verself-artifact://otelcol-runtime"
        destination = "local"
        chown = true
      }

      config {
        command = "local/bin/otelcol-contrib"
        args = ["--config", "local/config/config.yaml"]
      }

      env {
        OTEL_RESOURCE_ATTRIBUTES = "verself.supervisor=nomad"
        OTEL_SERVICE_NAME = "otelcol"
        VERSELF_SUPERVISOR = "nomad"
      }

      resources {
        cpu = 600
        memory = 2048
      }

      service {
        name = "otelcol-otlp-grpc"
        port = "otlp_grpc"
        provider = "nomad"
        address_mode = "auto"
        check {
          name = "otelcol-otlp-grpc-tcp"
          type = "tcp"
          port = "otlp_grpc"
          interval = "2s"
          timeout = "3s"
        }
      }

      service {
        name = "otelcol-otlp-http"
        port = "otlp_http"
        provider = "nomad"
        address_mode = "auto"
        check {
          name = "otelcol-otlp-http-tcp"
          type = "tcp"
          port = "otlp_http"
          interval = "2s"
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
      auto_revert = true
    }
  }
}
