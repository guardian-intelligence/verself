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

    task "clickhouse-spiffe-helper" {
      driver = "raw_exec"
      user = "grafana"

      lifecycle {
        hook = "prestart"
        sidecar = true
      }

      config {
        command = "/opt/verself/profile/bin/spiffe-helper"
        args = ["-config", "/etc/grafana/clickhouse-spiffe-helper.conf"]
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
        command = "/bin/sh"
        args = ["-ec", "set -a\n. /etc/credstore/grafana/grafana.env\nset +a\nexec /opt/verself/profile/bin/grafana server --homepath /opt/verself/grafana --config /etc/grafana/grafana.ini\n"]
      }

      env {
        HOME = "/var/lib/grafana"
        PATH = "/opt/verself/profile/bin:/usr/bin:/bin"
        OTEL_EXPORTER_OTLP_ENDPOINT = "http://127.0.0.1:4317"
        OTEL_RESOURCE_ATTRIBUTES = "verself.supervisor=nomad"
        OTEL_SERVICE_NAME = "grafana"
        VERSELF_SUPERVISOR = "nomad"
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
  }
}
