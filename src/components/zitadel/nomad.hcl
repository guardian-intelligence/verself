job "zitadel" {
  name = "zitadel"
  datacenters = ["dc1"]
  type = "service"

  group "zitadel" {
    count = 1

    network {
      port "http" {
        static = 8085
        to = 8085
      }
    }

    task "server" {
      driver = "raw_exec"
      user = "zitadel"

      config {
        command = "/opt/verself/profile/bin/zitadel"
        args = ["start", "--masterkeyFile", "/etc/credstore/zitadel/masterkey", "--config", "/etc/zitadel/config.yaml"]
      }

      env {
        HOME = "/var/lib/zitadel"
        OTEL_EXPORTER_OTLP_ENDPOINT = "http://127.0.0.1:4317"
        OTEL_RESOURCE_ATTRIBUTES = "verself.supervisor=nomad"
        OTEL_SERVICE_NAME = "zitadel"
        VERSELF_SUPERVISOR = "nomad"
      }

      resources {
        cpu = 700
        memory = 1024
      }

      service {
        name = "zitadel-http"
        port = "http"
        provider = "nomad"
        address_mode = "auto"
      }
    }
  }
}
