job "garage-1" {
  name = "garage-1"
  datacenters = ["dc1"]
  type = "service"

  group "garage-1" {
    count = 1

    network {
      mode = "host"
      port "s3" {
        host_network = "loopback"
        static = 3910
        to = 3910
      }
      port "rpc" {
        host_network = "loopback"
        static = 3911
        to = 3911
      }
      port "admin" {
        host_network = "loopback"
        static = 3913
        to = 3913
      }
    }

    task "server" {
      driver = "raw_exec"
      user = "garage"

      config {
        command = "/opt/verself/profile/bin/garage"
        args = ["-c", "/etc/garage/garage-1.toml", "server"]
      }

      env {
        OTEL_EXPORTER_OTLP_ENDPOINT = "http://127.0.0.1:4317"
        OTEL_RESOURCE_ATTRIBUTES = "verself.supervisor=nomad"
        OTEL_SERVICE_NAME = "garage"
        VERSELF_SUPERVISOR = "nomad"
      }

      resources {
        cpu = 500
        memory = 512
      }

      restart {
        attempts = 3
        delay = "15s"
        interval = "300s"
        mode = "delay"
      }

      service {
        name = "garage-s3"
        port = "s3"
        provider = "nomad"
        address_mode = "auto"
        check {
          name = "garage-s3-tcp"
          type = "tcp"
          port = "s3"
          interval = "1s"
          timeout = "3s"
        }
      }

      service {
        name = "garage-admin"
        port = "admin"
        provider = "nomad"
        address_mode = "auto"
        check {
          name = "garage-admin-tcp"
          type = "tcp"
          port = "admin"
          interval = "1s"
          timeout = "3s"
        }
      }
    }

    update {
      max_parallel = 1
      health_check = "checks"
      min_healthy_time = "3s"
      healthy_deadline = "60s"
      progress_deadline = "90s"
      auto_revert = true
    }
  }
}
