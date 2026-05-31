job "grafana" {
  name = "grafana"
  datacenters = ["*"]
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
        command = "/bin/sh"
        args = ["-ec", "set -a\n. /etc/credstore/grafana/grafana.env\nset +a\nexport GF_PATHS_PLUGINS=\"$VERSELF_GRAFANA_RUNTIME/plugins\"\nexec \"$VERSELF_GRAFANA_RUNTIME/grafana/bin/grafana\" server --homepath \"$VERSELF_GRAFANA_RUNTIME/grafana\" --config /etc/grafana/grafana.ini\n"]
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
        command = "/bin/sh"
        args = ["-ec", "set -a\n. /etc/credstore/grafana/grafana.env\nset +a\nexport GF_PATHS_PLUGINS=\"$VERSELF_GRAFANA_RUNTIME/plugins\"\nlast_status=1\nfor attempt in $(seq 1 30); do\n  if \"$VERSELF_GRAFANA_RUNTIME/grafana/bin/grafana\" cli --homepath \"$VERSELF_GRAFANA_RUNTIME/grafana\" --config /etc/grafana/grafana.ini admin reset-admin-password \"$GF_SECURITY_ADMIN_PASSWORD\"; then\n    exit 0\n  fi\n  last_status=$?\n  sleep 1\ndone\nexit \"$last_status\"\n"]
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
