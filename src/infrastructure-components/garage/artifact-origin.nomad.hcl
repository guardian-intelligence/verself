job "garage-artifact-origin" {
  name = "garage-artifact-origin"
  datacenters = ["dc1"]
  type = "service"

  group "artifact-origin" {
    count = 1

    network {
      mode = "host"
      port "https" {
        host_network = "loopback"
        static = 9443
        to = 9443
      }
    }

    task "origin" {
      driver = "raw_exec"
      user = "root"

      config {
        command = "$${VERSELF_GARAGE_RUNTIME}/bin/garage-artifact-origin"
        args = [
          "--garage-bin",
          "$${VERSELF_GARAGE_RUNTIME}/bin/garage",
          "--nodes=0:3900:3901:3903,1:3910:3911:3913,2:3920:3921:3923",
          "--bucket=nomad-artifacts",
          "--reader-env=/etc/nomad/nomad-artifacts.env",
          "--reader-access-env=AWS_ACCESS_KEY_ID",
          "--reader-secret-env=AWS_SECRET_ACCESS_KEY",
          "--publisher-env=/etc/garage/nomad-artifacts/publisher.env",
          "--publisher-access-env=VERSELF_NOMAD_ARTIFACTS_S3_ACCESS_KEY_ID",
          "--publisher-secret-env=VERSELF_NOMAD_ARTIFACTS_S3_SECRET_ACCESS_KEY",
          "--origin-hostname=artifacts.internal.verself.local",
          "--ca-bundle=/etc/verself/pki/nomad-artifacts-ca.pem",
          "--listen=127.0.0.1:9443",
          "--target=http://127.0.0.1:3900",
        ]
      }

      env {
        OTEL_EXPORTER_OTLP_ENDPOINT = "http://127.0.0.1:4317"
        OTEL_RESOURCE_ATTRIBUTES = "verself.supervisor=nomad"
        OTEL_SERVICE_NAME = "garage-artifact-origin"
        VERSELF_GARAGE_RUNTIME = "verself-artifact://garage-runtime"
        VERSELF_SUPERVISOR = "nomad"
      }

      resources {
        cpu = 200
        memory = 256
      }

      restart {
        attempts = 3
        delay = "15s"
        interval = "300s"
        mode = "delay"
      }

      service {
        name = "artifact-origin"
        port = "https"
        provider = "nomad"
        address_mode = "auto"
        check {
          name = "artifact-origin-tcp"
          type = "tcp"
          port = "https"
          interval = "1s"
          timeout = "3s"
        }
      }
    }

    update {
      max_parallel = 1
      health_check = "checks"
      min_healthy_time = "3s"
      healthy_deadline = "120s"
      progress_deadline = "180s"
      auto_revert = true
    }
  }
}
