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

    task "server" {
      driver = "raw_exec"
      user = "verdaccio"

      config {
        command = "/opt/verself/verdaccio/bin/verdaccio"
        args = ["--config", "/etc/verdaccio/config.yaml"]
      }

      env {
        HOME = "/var/lib/verdaccio"
        PATH = "/opt/verself/verdaccio/bin:/opt/verself/profile/bin:/usr/bin:/bin"
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
      }
    }
  }
}
