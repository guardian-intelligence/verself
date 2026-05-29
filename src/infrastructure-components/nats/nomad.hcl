job "nats" {
  name = "nats"
  datacenters = ["dc1"]
  type = "service"

  group "nats" {
    count = 1

    meta {
      verself_group_kind = "service"
    }

    network {
      mode = "host"
      port "client" {
        host_network = "loopback"
        static = 4222
        to = 4222
      }
      port "monitoring" {
        host_network = "loopback"
        static = 8222
        to = 8222
      }
    }

    task "spiffe-helper" {
      driver = "raw_exec"
      user = "nats"

      artifact {
        source = "verself-artifact://nats-runtime"
        destination = "local"
        chown = true
      }

      lifecycle {
        hook = "prestart"
        sidecar = true
      }

      config {
        command = "local/bin/spiffe-helper"
        args = ["-config", "/etc/nats/nats-spiffe-helper.conf"]
      }

      resources {
        cpu = 50
        memory = 64
      }
    }

    task "server" {
      driver = "raw_exec"
      user = "nats"

      artifact {
        source = "verself-artifact://nats-runtime"
        destination = "local"
        chown = true
      }

      config {
        command = "local/bin/nats-server"
        args = ["-c", "/etc/nats/nats-server.conf"]
      }

      env {
        OTEL_EXPORTER_OTLP_ENDPOINT = "http://127.0.0.1:4317"
        OTEL_RESOURCE_ATTRIBUTES = "verself.supervisor=nomad"
        OTEL_SERVICE_NAME = "nats"
        VERSELF_SUPERVISOR = "nomad"
      }

      resources {
        cpu = 300
        memory = 512
      }

      service {
        name = "nats-client"
        port = "client"
        provider = "nomad"
        address_mode = "auto"
        check {
          name = "nats-client-tcp"
          type = "tcp"
          port = "client"
          interval = "2s"
          timeout = "3s"
        }
      }

      service {
        name = "nats-monitoring"
        port = "monitoring"
        provider = "nomad"
        address_mode = "auto"
        check {
          name = "nats-monitoring-tcp"
          type = "tcp"
          port = "monitoring"
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
