job "stalwart" {
  name = "stalwart"
  datacenters = ["dc1"]
  type = "service"

  group "stalwart" {
    count = 1

    network {
      mode = "host"
      # Stalwart binds SMTP publicly; Nomad service discovery stays in-node.
      port "smtp" {
        host_network = "loopback"
        static = 25
        to = 25
      }
      port "http" {
        host_network = "loopback"
        static = 8090
        to = 8090
      }
    }

    task "server" {
      driver = "raw_exec"
      user = "root"

      artifact {
        source = "verself-artifact://stalwart-runtime"
        destination = "local"
      }

      config {
        command = "/bin/sh"
        args = ["-ec", "getent group stalwart >/dev/null || groupadd --system stalwart\nid -u stalwart >/dev/null 2>&1 || useradd --system --gid stalwart --home-dir /var/lib/stalwart --shell /usr/sbin/nologin --no-create-home stalwart\ninstall -d -o stalwart -g stalwart -m 0750 /var/lib/stalwart\ninstall -o stalwart -g stalwart -m 0644 local/share/stalwart/webadmin.zip /var/lib/stalwart/webadmin.zip\ninstall -o stalwart -g stalwart -m 0644 local/share/stalwart/spam-filter.toml /var/lib/stalwart/spam-filter.toml\n/usr/sbin/setcap cap_net_bind_service+ep local/bin/stalwart\nexec /usr/sbin/runuser -u stalwart --preserve-environment -- local/bin/stalwart --config \"$${NOMAD_TASK_DIR}/config.toml\"\n"]
      }

      env {
        OTEL_EXPORTER_OTLP_ENDPOINT = "http://127.0.0.1:4317"
        OTEL_RESOURCE_ATTRIBUTES = "verself.supervisor=nomad"
        OTEL_SERVICE_NAME = "stalwart"
        VERSELF_SUPERVISOR = "nomad"
      }

      resources {
        cpu = 700
        memory = 1024
      }

      # Stalwart configuration. Owned here so changes ride `aspect deploy`.
      # Receive-only: no outbound relay.
      template {
        change_mode = "restart"
        destination = "local/config.toml"
        data = <<-EOT
[server]
hostname = "mail.verself.sh"
max-connections = 256

[server.listener."smtp"]
bind = ["0.0.0.0:25"]
protocol = "smtp"
tls.implicit = false

[server.listener."http"]
bind = ["127.0.0.1:8090"]
protocol = "http"
tls.implicit = false

# HAProxy's lego renewal unit writes this material from the product
# wildcard certificate and reloads Stalwart after successful validation.
[certificate."default"]
cert = "%%{file:/etc/stalwart/certs/cert.pem}%"
private-key = "%%{file:/etc/stalwart/certs/key.pem}%"
default = true

[config]
local-keys = [
    "store.*",
    "directory.*",
    "tracer.*",
    "!server.blocked-ip.*",
    "!server.allowed-ip.*",
    "server.*",
    "authentication.fallback-admin.*",
    "cluster.*",
    "config.local-keys.*",
    "storage.data",
    "storage.blob",
    "storage.lookup",
    "storage.fts",
    "storage.directory",
    "certificate.*",
    "http.*",
    "webadmin.*",
    "spam-filter.resource",
]

[http]
url = "'https://mail.verself.sh'"
use-x-forwarded = true
allowed-endpoint.0.if = "starts_with(url_path, '/api') && remote_ip != '127.0.0.1'"
allowed-endpoint.0.then = "404"
allowed-endpoint.1.else = "200"

[webadmin]
path = "/var/lib/stalwart"
resource = "file:///var/lib/stalwart/webadmin.zip"

[spam-filter]
resource = "file:///var/lib/stalwart/spam-filter.toml"

[store."postgresql"]
type = "postgresql"
# Unix-socket peer auth. Stalwart runs as the `stalwart` OS user; pg_ident.conf
# maps that to the `stalwart` PG role. No password is sent; the role has no
# password set, so TCP/SCRAM auth against this role is rejected by PostgreSQL.
host = "/var/run/postgresql"
database = "stalwart"
user = "stalwart"
timeout = "15s"

[store."postgresql".pool]
max-connections = 10

[storage]
data = "postgresql"
blob = "postgresql"
fts = "postgresql"
lookup = "postgresql"
directory = "internal"

[directory."internal"]
type = "internal"
store = "postgresql"

# Fallback admin for Management API bootstrap.
[authentication.fallback-admin]
user = "admin"
secret = "%%{file:/etc/credstore/stalwart/admin-password}%"

[tracer."otel"]
type = "open-telemetry"
transport = "grpc"
endpoint = "http://127.0.0.1:4317"
level = "info"
enable = true
enable.log-exporter = true
enable.span-exporter = true
EOT
      }

      service {
        name = "stalwart-http"
        port = "http"
        provider = "nomad"
        address_mode = "auto"
      }

      service {
        name = "stalwart-smtp"
        port = "smtp"
        provider = "nomad"
        address_mode = "auto"
      }
    }

    task "settings" {
      driver = "raw_exec"
      user = "stalwart"

      lifecycle {
        hook = "poststart"
        sidecar = false
      }

      artifact {
        source = "verself-artifact://stalwart-runtime"
        destination = "local"
        chown = true
      }

      config {
        command = "/bin/sh"
        args = ["-ec", "credentials=\"admin:$(tr -d '\\n' </etc/credstore/stalwart/admin-password)\"\nlast_status=1\nfor attempt in $(seq 1 30); do\n  if CREDENTIALS=\"$credentials\" local/bin/stalwart-cli -u http://127.0.0.1:8090 server add-config session.rcpt.relay false && \\\n     CREDENTIALS=\"$credentials\" local/bin/stalwart-cli -u http://127.0.0.1:8090 server add-config asn.type disabled && \\\n     CREDENTIALS=\"$credentials\" local/bin/stalwart-cli -u http://127.0.0.1:8090 server add-config metrics.open-telemetry.transport grpc && \\\n     CREDENTIALS=\"$credentials\" local/bin/stalwart-cli -u http://127.0.0.1:8090 server add-config metrics.open-telemetry.endpoint http://127.0.0.1:4317 && \\\n     CREDENTIALS=\"$credentials\" local/bin/stalwart-cli -u http://127.0.0.1:8090 server add-config metrics.open-telemetry.interval 30s && \\\n     CREDENTIALS=\"$credentials\" local/bin/stalwart-cli -u http://127.0.0.1:8090 server reload-config; then\n    exit 0\n  fi\n  last_status=$?\n  sleep 1\ndone\nexit \"$last_status\"\n"]
      }

      resources {
        cpu = 100
        memory = 64
      }
    }
  }
}
