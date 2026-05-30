job "forgejo" {
  name = "forgejo"
  datacenters = ["dc1"]
  type = "service"

  group "forgejo" {
    count = 1

    network {
      mode = "host"
      port "http" {
        host_network = "loopback"
        static = 3000
        to = 3000
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
        source = "verself-artifact://forgejo-runtime"
        destination = "local"
        chown = true
      }

      config {
        command = "local/bin/forgejo-node-runner"
        args = ["prepare"]
      }
    }

    task "server" {
      driver = "raw_exec"
      user = "forgejo"

      artifact {
        source = "verself-artifact://forgejo-runtime"
        destination = "local"
        chown = true
      }

      config {
        command = "local/bin/forgejo-node-runner"
        args = ["serve"]
      }

      env {
        HOME = "/var/lib/forgejo"
        FORGEJO_WORK_DIR = "/var/lib/forgejo"
        PATH = "$${NOMAD_TASK_DIR}/local/bin:/usr/bin:/bin"
        OTEL_EXPORTER_OTLP_ENDPOINT = "http://127.0.0.1:4317"
        OTEL_RESOURCE_ATTRIBUTES = "verself.supervisor=nomad"
        OTEL_SERVICE_NAME = "forgejo"
        VERSELF_SUPERVISOR = "nomad"
      }

      resources {
        cpu = 600
        memory = 768
      }

      service {
        name = "forgejo-http"
        port = "http"
        provider = "nomad"
        address_mode = "auto"
      }
    }
  }
}
