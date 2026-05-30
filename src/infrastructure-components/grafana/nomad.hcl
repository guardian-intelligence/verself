job "grafana" {
  name = "grafana"
  datacenters = ["dc1"]
  type = "service"

  group "grafana" {
    count = 1

    network {
      mode = "host"
      port "http" {
        host_network = "loopback"
        static = 4300
        to = 4300
      }
    }

    task "prepare" {
      driver = "raw_exec"
      user = "root"

      lifecycle {
        hook = "prestart"
        sidecar = false
      }

      config {
        command = "$${VERSELF_GRAFANA_RUNTIME}/bin/grafana-node-runner"
        args = ["prepare"]
      }

      env {
        VERSELF_GRAFANA_RUNTIME = "verself-artifact://grafana-runtime"
      }

      resources {
        cpu = 100
        memory = 128
      }
    }

    task "clickhouse-spiffe-helper" {
      driver = "raw_exec"
      user = "grafana"

      lifecycle {
        hook = "poststart"
        sidecar = true
      }

      config {
        command = "$${VERSELF_GRAFANA_RUNTIME}/bin/spiffe-helper"
        args = ["-config", "/etc/grafana/clickhouse-spiffe-helper.conf"]
      }

      env {
        VERSELF_GRAFANA_RUNTIME = "verself-artifact://grafana-runtime"
      }

      resources {
        cpu = 50
        memory = 64
      }
    }

    task "server" {
      driver = "raw_exec"
      user = "grafana"

      config {
        command = "$${VERSELF_GRAFANA_RUNTIME}/bin/grafana-node-runner"
        args = ["serve"]
      }

      env {
        HOME = "/var/lib/grafana"
        PATH = "$${VERSELF_GRAFANA_RUNTIME}/grafana/bin:$${VERSELF_GRAFANA_RUNTIME}/bin:/usr/bin:/bin"
        OTEL_EXPORTER_OTLP_ENDPOINT = "http://127.0.0.1:4317"
        OTEL_RESOURCE_ATTRIBUTES = "verself.supervisor=nomad"
        OTEL_SERVICE_NAME = "grafana"
        VERSELF_SUPERVISOR = "nomad"
        VERSELF_GRAFANA_RUNTIME = "verself-artifact://grafana-runtime"
      }

      resources {
        cpu = 600
        memory = 1024
      }

      service {
        name = "grafana-http"
        port = "http"
        provider = "nomad"
        address_mode = "auto"
      }
    }

    task "admin-password" {
      driver = "raw_exec"
      user = "grafana"

      lifecycle {
        hook = "poststart"
        sidecar = false
      }

      config {
        command = "$${VERSELF_GRAFANA_RUNTIME}/bin/grafana-node-runner"
        args = ["reset-admin-password"]
      }

      env {
        HOME = "/var/lib/grafana"
        VERSELF_GRAFANA_RUNTIME = "verself-artifact://grafana-runtime"
      }

      resources {
        cpu = 100
        memory = 128
      }
    }
  }
}
