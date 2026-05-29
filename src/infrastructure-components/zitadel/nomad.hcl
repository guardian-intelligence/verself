job "zitadel" {
  name = "zitadel"
  datacenters = ["dc1"]
  type = "service"

  group "zitadel" {
    count = 1

    meta {
      verself_group_kind = "service"
      verself_allow_lifecycle_bootstrap = "true"
    }

    network {
      mode = "host"
      port "http" {
        host_network = "loopback"
        static = 8085
        to = 8085
      }
    }

    task "setup" {
      driver = "raw_exec"
      user = "root"
      lifecycle {
        hook = "prestart"
        sidecar = false
      }

      artifact {
        source = "verself-artifact://zitadel-setup-apply"
        destination = "local"
        chown = true
      }

      artifact {
        source = "verself-artifact://zitadel-runtime"
        destination = "local"
        chown = true
      }

      config {
        command = "local/bin/zitadel-setup-apply"
      }

      env {
        HOME = "/var/lib/zitadel"
        OTEL_EXPORTER_OTLP_ENDPOINT = "http://127.0.0.1:4317"
        OTEL_RESOURCE_ATTRIBUTES = "verself.supervisor=nomad"
        OTEL_SERVICE_NAME = "zitadel-setup-apply"
        VERSELF_ZITADEL_BIN = "local/bin/zitadel"
        VERSELF_ZITADEL_EXTERNAL_DOMAIN = "verself.sh"
        VERSELF_SUPERVISOR = "nomad"
      }

      resources {
        cpu = 200
        memory = 256
      }
    }

    task "server" {
      driver = "raw_exec"
      user = "zitadel"

      artifact {
        source = "verself-artifact://zitadel-runtime"
        destination = "local"
        chown = true
      }

      config {
        command = "local/bin/zitadel"
        args = ["start", "--masterkeyFile", "/etc/credstore/zitadel/masterkey", "--config", "/etc/zitadel/config.yaml"]
      }

      env {
        HOME = "/var/lib/zitadel"
        OTEL_EXPORTER_OTLP_ENDPOINT = "http://127.0.0.1:4317"
        OTEL_RESOURCE_ATTRIBUTES = "verself.supervisor=nomad"
        OTEL_SERVICE_NAME = "zitadel"
        VERSELF_ZITADEL_EXTERNAL_DOMAIN = "verself.sh"
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
        check {
          name = "zitadel-http-tcp"
          type = "tcp"
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
