job "tigerbeetle" {
  name = "tigerbeetle"
  datacenters = ["*"]
  type = "service"

  group "tigerbeetle" {
    count = 1

    network {
      mode = "host"
      port "client" {
        host_network = "loopback"
        static = 3320
        to = 3320
      }
    }

    task "server" {
      driver = "raw_exec"
      user = "root"

      artifact {
        source = "verself-artifact://tigerbeetle-runtime"
        destination = "local"
      }

      config {
        command = "/bin/sh"
        args = ["-ec", "getent group tigerbeetle >/dev/null || groupadd --system tigerbeetle\nid -u tigerbeetle >/dev/null 2>&1 || useradd --system --gid tigerbeetle --home-dir /var/lib/tigerbeetle --shell /usr/sbin/nologin --no-create-home tigerbeetle\ninstall -d -o tigerbeetle -g tigerbeetle -m 0700 /var/lib/tigerbeetle\nif [ ! -e /var/lib/tigerbeetle/data.tigerbeetle ]; then\n  /usr/sbin/runuser -u tigerbeetle -- local/bin/tigerbeetle format --cluster=0 --replica=0 --replica-count=1 /var/lib/tigerbeetle/data.tigerbeetle\nfi\n/usr/sbin/setcap cap_ipc_lock+ep local/bin/tigerbeetle\nexec /usr/sbin/runuser -u tigerbeetle --preserve-environment -- local/bin/tigerbeetle start \\\n  --addresses=127.0.0.1:3320 \\\n  --experimental \\\n  --statsd=127.0.0.1:8125 \\\n  /var/lib/tigerbeetle/data.tigerbeetle\n"]
      }

      env {
        TB_LOG = "info"
        TB_STATSSD = "127.0.0.1:8125"
        OTEL_EXPORTER_OTLP_ENDPOINT = "http://127.0.0.1:4317"
        OTEL_RESOURCE_ATTRIBUTES = "verself.supervisor=nomad"
        OTEL_SERVICE_NAME = "tigerbeetle"
        VERSELF_SUPERVISOR = "nomad"
      }

      resources {
        cpu = 1000
        memory = 8192
      }

      service {
        name = "tigerbeetle-client"
        port = "client"
        provider = "nomad"
        address_mode = "auto"
      }
    }
  }
}
