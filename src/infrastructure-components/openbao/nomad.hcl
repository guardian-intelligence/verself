job "openbao" {
  name = "openbao"
  datacenters = ["dc1"]
  type = "service"

  group "openbao" {
    count = 1

    network {
      mode = "host"
      port "api" {
        host_network = "loopback"
        static = 8200
        to = 8200
      }
      port "cluster" {
        host_network = "loopback"
        static = 8201
        to = 8201
      }
    }

    task "server" {
      driver = "raw_exec"
      user = "openbao"

      artifact {
        source = "verself-artifact://openbao-runtime"
        destination = "local"
        chown = true
      }

      config {
        command = "local/bin/bao"
        args = ["server", "-config=/etc/openbao/openbao.hcl"]
      }

      env {
        BAO_ADDR = "https://127.0.0.1:8200"
        HOME = "/var/lib/openbao"
        OTEL_EXPORTER_OTLP_ENDPOINT = "http://127.0.0.1:4317"
        OTEL_RESOURCE_ATTRIBUTES = "verself.supervisor=nomad"
        OTEL_SERVICE_NAME = "openbao"
        VERSELF_SUPERVISOR = "nomad"
      }

      resources {
        cpu = 500
        memory = 512
      }

      service {
        name = "openbao-api"
        port = "api"
        provider = "nomad"
        address_mode = "auto"
      }
    }

    task "unseal" {
      driver = "raw_exec"
      user = "root"

      artifact {
        source = "verself-artifact://openbao-runtime"
        destination = "local"
      }

      lifecycle {
        hook = "poststart"
        sidecar = false
      }

      config {
        command = "/bin/sh"
        args = ["-ec", "for i in $(seq 1 45); do local/bin/bao status -format=json >/tmp/openbao-status.json 2>/dev/null && break; sleep 1; done\nsealed=$(sed -n 's/.*\"sealed\":[[:space:]]*\\(true\\|false\\).*/\\1/p' /tmp/openbao-status.json | head -n1)\nif [ -z \"$sealed\" ]; then\n  echo \"openbao status did not return a sealed field\" >&2\n  exit 1\nfi\nif [ \"$sealed\" = \"true\" ]; then\n  local/bin/bao operator unseal \"$(tr -d '\\n' </etc/credstore/openbao/unseal-key-1)\" >/dev/null\n  local/bin/bao operator unseal \"$(tr -d '\\n' </etc/credstore/openbao/unseal-key-2)\" >/dev/null\nfi\n"]
      }

      env {
        BAO_ADDR = "https://127.0.0.1:8200"
        BAO_CACERT = "/etc/openbao/tls/cert.pem"
      }

      resources {
        cpu = 50
        memory = 64
      }
    }
  }
}
