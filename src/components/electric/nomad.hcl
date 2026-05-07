job "electric" {
  name = "electric"
  datacenters = ["dc1"]
  type = "service"

  group "electric-default" {
    count = 1
    network {
      mode = "host"
      port "http" {
        host_network = "loopback"
      }
    }
    task "electric-default" {
      driver = "raw_exec"
      user = "root"
      kill_signal = "SIGTERM"
      kill_timeout = "45s"
      artifact {
        source = "verself-artifact://electric-nomad-runner"
        destination = "local"
        chown = true
      }
      config {
        command = "local/bin/electric-nomad-runner"
        args = [
          "--ctr=/opt/verself/profile/bin/ctr",
          "--image=docker.io/electricsql/electric:1.5.0",
          "--env-file=/etc/credstore/electric/runtime.env",
          "--storage-dir=/var/lib/electric",
          "--instance-id=electric",
          "--replication-stream-id=default",
          "--db-pool-size=15",
        ]
      }
      env {
        OTEL_EXPORTER_OTLP_ENDPOINT = "http://127.0.0.1:4317"
        OTEL_RESOURCE_ATTRIBUTES = "verself.supervisor=nomad"
        OTEL_SERVICE_NAME = "electric"
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
        name = "electric-http"
        port = "http"
        provider = "nomad"
        address_mode = "auto"
        check {
          name = "electric-http-tcp"
          type = "tcp"
          port = "http"
          interval = "1s"
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

  group "electric-notifications" {
    count = 1
    network {
      mode = "host"
      port "http" {
        host_network = "loopback"
      }
    }
    task "electric-notifications" {
      driver = "raw_exec"
      user = "root"
      kill_signal = "SIGTERM"
      kill_timeout = "45s"
      artifact {
        source = "verself-artifact://electric-nomad-runner"
        destination = "local"
        chown = true
      }
      config {
        command = "local/bin/electric-nomad-runner"
        args = [
          "--ctr=/opt/verself/profile/bin/ctr",
          "--image=docker.io/electricsql/electric:1.5.0",
          "--env-file=/etc/credstore/electric-notifications/runtime.env",
          "--storage-dir=/var/lib/electric-notifications",
          "--instance-id=electric-notifications",
          "--replication-stream-id=notifications",
          "--db-pool-size=8",
        ]
      }
      env {
        OTEL_EXPORTER_OTLP_ENDPOINT = "http://127.0.0.1:4317"
        OTEL_RESOURCE_ATTRIBUTES = "verself.supervisor=nomad"
        OTEL_SERVICE_NAME = "electric-notifications"
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
        name = "electric-notifications-http"
        port = "http"
        provider = "nomad"
        address_mode = "auto"
        check {
          name = "electric-notifications-http-tcp"
          type = "tcp"
          port = "http"
          interval = "1s"
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
