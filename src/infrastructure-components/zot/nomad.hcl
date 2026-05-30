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
        command = "local/bin/zot-node-runner"
        args = ["--zot-bin", "local/bin/zot", "--zot-htpasswd-bin", "local/bin/zot-htpasswd"]
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
