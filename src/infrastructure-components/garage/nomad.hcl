job "garage" {
  name = "garage"
  datacenters = ["*"]
  type = "service"

  group "garage-0" {
    count = 1

    network {
      mode = "host"
      port "s3" {
        host_network = "loopback"
        static = 3900
        to = 3900
      }
      port "rpc" {
        host_network = "loopback"
        static = 3901
        to = 3901
      }
      port "admin" {
        host_network = "loopback"
        static = 3903
        to = 3903
      }
    }

    task "server" {
      driver = "raw_exec"
      user = "root"

      config {
        command = "/bin/sh"
        args = ["-ec", "getent group garage >/dev/null || groupadd --system garage\nid -u garage >/dev/null 2>&1 || useradd --system --gid garage --home-dir /var/lib/garage-0 --shell /usr/sbin/nologin --no-create-home garage\ninstall -d -o garage -g garage -m 0750 /var/lib/garage-0 /var/lib/garage-0/data /var/lib/garage-0/meta /var/lib/garage-0/snapshots\nexec /usr/sbin/runuser -u garage --preserve-environment -- \"$${VERSELF_GARAGE_RUNTIME}/bin/garage\" -c /etc/garage/garage-0.toml server\n"]
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
