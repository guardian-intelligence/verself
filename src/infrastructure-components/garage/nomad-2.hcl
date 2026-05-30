job "garage-2" {
  name = "garage-2"
  datacenters = ["dc1"]
  type = "service"

  group "garage-2" {
    count = 1

    network {
      mode = "host"
      port "s3" {
        host_network = "loopback"
        static = 3920
        to = 3920
      }
      port "rpc" {
        host_network = "loopback"
        static = 3921
        to = 3921
      }
      port "admin" {
        host_network = "loopback"
        static = 3923
        to = 3923
      }
    }

    task "server" {
      driver = "raw_exec"
      user = "root"

      config {
        command = "$${VERSELF_GARAGE_RUNTIME}/bin/garage-node-runner"
        args = [
          "--garage-bin",
          "$${VERSELF_GARAGE_RUNTIME}/bin/garage",
          "--instance=2",
          "--nodes=0:3900:3901:3903,1:3910:3911:3913,2:3920:3921:3923",
        ]
      }

      env {
        OTEL_EXPORTER_OTLP_ENDPOINT = "http://127.0.0.1:4317"
        OTEL_RESOURCE_ATTRIBUTES = "verself.supervisor=nomad"
        OTEL_SERVICE_NAME = "garage"
        VERSELF_GARAGE_RUNTIME = "verself-artifact://garage-runtime"
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
