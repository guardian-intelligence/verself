job "verdaccio" {
  name = "verdaccio"
  datacenters = ["dc1"]
  type = "service"

  group "verdaccio" {
    count = 1

    network {
      mode = "host"
      port "http" {
        host_network = "loopback"
        static = 4873
        to = 4873
      }
    }

    task "prepare-storage" {
      driver = "raw_exec"
      user = "root"
      lifecycle {
        hook = "prestart"
        sidecar = false
      }

      config {
        command = "/bin/sh"
        args = ["-ec", "install -d -o nobody -g nogroup -m 0755 /var/lib/verdaccio /var/lib/verdaccio/storage\nif [ ! -e /var/lib/verdaccio/htpasswd ]; then\n  : > /var/lib/verdaccio/htpasswd\nfi\nchmod 0640 /var/lib/verdaccio/htpasswd\nif [ ! -e /var/lib/verdaccio/.nomad-owner-nobody-v1 ]; then\n  chown -R nobody:nogroup /var/lib/verdaccio\n  : > /var/lib/verdaccio/.nomad-owner-nobody-v1\n  chown nobody:nogroup /var/lib/verdaccio/.nomad-owner-nobody-v1\nelse\n  chown nobody:nogroup /var/lib/verdaccio/htpasswd\nfi\n"]
      }

      resources {
        cpu = 100
        memory = 64
      }
    }

    task "server" {
      driver = "raw_exec"
      user = "nobody"
      kill_signal = "SIGTERM"
      kill_timeout = "30s"

      artifact {
        source = "verself-artifact://verdaccio-runtime"
        destination = "local"
        chown = true
      }

      config {
        command = "/bin/sh"
        args = ["-ec", "if [ ! -d local/lib/node_modules/verdaccio ]; then\n  tar -xf local/lib/node_modules.tar -C local/lib\nfi\nexport PATH=\"$PWD/local/bin:/usr/bin:/bin\"\nexec local/bin/verdaccio --config local/etc/verdaccio/config.yaml\n"]
      }

      env {
        HOME = "/var/lib/verdaccio"
        OTEL_EXPORTER_OTLP_ENDPOINT = "http://127.0.0.1:4317"
        OTEL_RESOURCE_ATTRIBUTES = "verself.supervisor=nomad"
        OTEL_SERVICE_NAME = "verdaccio"
        VERSELF_SUPERVISOR = "nomad"
      }

      resources {
        cpu = 500
        memory = 768
      }

      service {
        name = "verdaccio-http"
        port = "http"
        provider = "nomad"
        address_mode = "auto"
        check {
          name = "verdaccio-http-ping"
          type = "http"
          path = "/-/ping"
          port = "http"
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
