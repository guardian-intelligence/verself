job "nats" {
  name = "nats"
  datacenters = ["*"]
  type = "service"

  group "nats" {
    count = 1

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

    task "nats-setup" {
      driver = "raw_exec"
      user = "root"

      lifecycle {
        hook = "prestart"
        sidecar = false
      }

      config {
        command = "/usr/bin/install"
        args = ["-d", "-o", "nats", "-g", "nats", "-m", "0750", "/var/lib/nats", "/var/lib/nats/jetstream", "/var/lib/nats/spiffe"]
      }

      resources {
        cpu = 50
        memory = 32
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
        args = ["-config", "local/config/nats-spiffe-helper.conf"]
      }

      template {
        change_mode = "restart"
        destination = "local/config/nats-spiffe-helper.conf"
        data = <<-EOT
agent_address = "/run/spire-agent/sockets/agent.sock"
cert_dir = "/var/lib/nats/spiffe"
daemon_mode = true
pid_file_name = "/var/lib/nats/nats.pid"
renew_signal = "SIGHUP"
svid_file_name = "svid.pem"
svid_key_file_name = "svid_key.pem"
svid_bundle_file_name = "bundle.pem"
cert_file_mode = 0640
key_file_mode = 0600
EOT
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
        args = ["-c", "local/config/nats-server.conf"]
      }

      env {
        OTEL_EXPORTER_OTLP_ENDPOINT = "http://127.0.0.1:4317"
        OTEL_RESOURCE_ATTRIBUTES = "verself.supervisor=nomad"
        OTEL_SERVICE_NAME = "nats"
        VERSELF_SUPERVISOR = "nomad"
      }

      template {
        change_mode = "restart"
        destination = "local/config/nats-server.conf"
        data = <<-EOT
server_name: verself-nats
pid_file: "/var/lib/nats/nats.pid"
host: 127.0.0.1
port: 4222
http: 8222

jetstream {
  store_dir: "/var/lib/nats/jetstream"
  max_mem_store: 256Mb
  max_file_store: 4Gb
}

tls {
  cert_file: "/var/lib/nats/spiffe/svid.pem"
  key_file: "/var/lib/nats/spiffe/svid_key.pem"
  ca_file: "/var/lib/nats/spiffe/bundle.pem"
  verify: true
  verify_and_map: true
  timeout: 2
}

authorization {
  users = [
    {
      user: "__VERSELF_SPIFFE_SERVICE_PREFIX__/notifications-service"
      permissions: {
        publish: {
          allow: ["events.>", "$JS.API.>", "$JS.ACK.>"]
        }
        subscribe: {
          allow: ["_INBOX.>"]
        }
      }
    }
  ]
}
EOT
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
      }

      service {
        name = "nats-monitoring"
        port = "monitoring"
        provider = "nomad"
        address_mode = "auto"
      }
    }
  }
}
