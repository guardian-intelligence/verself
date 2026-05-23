job "zot" {
  name = "zot"
  datacenters = ["dc1"]
  type = "service"

  group "zot" {
    count = 1

    network {
      mode = "host"
      port "http" {
        host_network = "loopback"
        static = 5080
        to = 5080
      }
    }

    task "server" {
      driver = "raw_exec"
      user = "root"

      artifact {
        source = "verself-artifact://zot-runtime"
        destination = "local"
      }

      config {
        command = "/bin/sh"
        args = ["-ec", "getent group zot >/dev/null || groupadd --system zot\nid -u zot >/dev/null 2>&1 || useradd --system --gid zot --home-dir /var/lib/zot --shell /usr/sbin/nologin --no-create-home zot\ninstall -d -o zot -g zot -m 0750 /var/lib/zot\nif [ ! -s /etc/zot/htpasswd ] && [ -s /etc/zot/publisher-password ]; then\n  local/bin/zot-htpasswd --username artifact-publisher --password-file /etc/zot/publisher-password >/tmp/zot-htpasswd\n  install -o root -g zot -m 0640 /tmp/zot-htpasswd /etc/zot/htpasswd\nfi\nlocal/bin/zot verify /etc/zot/config.json\nexec /usr/sbin/runuser -u zot --preserve-environment -- local/bin/zot serve /etc/zot/config.json\n"]
      }

      env {
        OTEL_EXPORTER_OTLP_ENDPOINT = "http://127.0.0.1:4317"
        OTEL_RESOURCE_ATTRIBUTES = "verself.supervisor=nomad"
        OTEL_SERVICE_NAME = "zot"
        VERSELF_SUPERVISOR = "nomad"
      }

      resources {
        cpu = 400
        memory = 512
      }

      service {
        name = "zot-http"
        port = "http"
        provider = "nomad"
        address_mode = "auto"
      }
    }
  }
}
