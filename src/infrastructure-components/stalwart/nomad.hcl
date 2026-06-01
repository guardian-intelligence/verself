job "stalwart" {
  name = "stalwart"
  datacenters = ["*"]
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
      vault {
        role = "stalwart-runtime"
      }
      identity {
        name = "vault_default"
        aud  = ["vault.io"]
        ttl  = "1h"
      }

      artifact {
        source = "verself-artifact://stalwart-runtime"
        destination = "local"
      }

      config {
        command = "/bin/sh"
        args = ["-ec", "getent group stalwart >/dev/null || groupadd --system stalwart\nid -u stalwart >/dev/null 2>&1 || useradd --system --gid stalwart --home-dir /var/lib/stalwart --shell /usr/sbin/nologin --no-create-home stalwart\ninstall -d -o stalwart -g stalwart -m 0750 /var/lib/stalwart\ninstall -o stalwart -g stalwart -m 0644 local/share/stalwart/webadmin.zip /var/lib/stalwart/webadmin.zip\ninstall -o stalwart -g stalwart -m 0644 local/share/stalwart/spam-filter.toml /var/lib/stalwart/spam-filter.toml\nadmin_hash=\"$${NOMAD_TASK_DIR}/admin-password.hash\"\nsalt=$(openssl rand -hex 8)\nopenssl passwd -6 -stdin -salt \"$salt\" < \"$${NOMAD_SECRETS_DIR}/admin-password\" | tr -d '\\n' > \"$admin_hash\"\nchown stalwart:stalwart \"$admin_hash\"\nchmod 0600 \"$admin_hash\"\nawk -v hash=\"$(cat \"$admin_hash\")\" '{ gsub(\"__STALWART_ADMIN_SECRET_HASH__\", hash); print }' \"$${NOMAD_TASK_DIR}/config.template.toml\" > \"$${NOMAD_TASK_DIR}/config.toml\"\nchown stalwart:stalwart \"$${NOMAD_TASK_DIR}/config.toml\"\nchmod 0600 \"$${NOMAD_TASK_DIR}/config.toml\"\n/usr/sbin/setcap cap_net_bind_service+ep local/bin/stalwart\nexec /usr/bin/setpriv --reuid=stalwart --regid=stalwart --init-groups local/bin/stalwart --config \"$${NOMAD_TASK_DIR}/config.toml\"\n"]
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
        destination = "secrets/admin-password"
        perms = "0600"
        data = <<-EOT
{{- with secret "kv-runtime/data/secret/org/stalwart.admin_password" -}}{{ .Data.data.value }}{{- end -}}
EOT
      }

      template {
        change_mode = "restart"
        destination = "local/config.template.toml"
        data = <<-EOT
[server]
hostname = "__VERSELF_STALWART_DOMAIN__"
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
url = "'__VERSELF_STALWART_PUBLIC_BASE_URL__'"
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
user = "verself-admin"
# Stalwart 0.15.5 accepts hashed fallback secrets inline; this auth field does
# not reliably resolve file interpolation.
secret = "__STALWART_ADMIN_SECRET_HASH__"

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
      vault {
        role = "stalwart-runtime"
      }
      identity {
        name = "vault_default"
        aud  = ["vault.io"]
        ttl  = "1h"
      }

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
        args = ["-ec", "test -n \"$STALWART_ADMIN_PASSWORD\" || { echo 'missing Stalwart OpenBao admin password' >&2; exit 1; }\ncredentials=\"verself-admin:$STALWART_ADMIN_PASSWORD\"\nstalwart_cli() {\n  output=$(CREDENTIALS=\"$credentials\" local/bin/stalwart-cli -u http://127.0.0.1:8090 \"$@\" 2>&1)\n  status=$?\n  if [ \"$status\" -ne 0 ] || printf '%s' \"$output\" | grep -Eq 'Authentication failed|Request failed'; then\n    printf '%s\\n' \"$output\" >&2\n    return 1\n  fi\n}\nlast_status=1\nfor attempt in $(seq 1 30); do\n  if stalwart_cli server add-config session.rcpt.relay false && \\\n     stalwart_cli server add-config asn.type disabled && \\\n     stalwart_cli server add-config metrics.open-telemetry.transport grpc && \\\n     stalwart_cli server add-config metrics.open-telemetry.endpoint http://127.0.0.1:4317 && \\\n     stalwart_cli server add-config metrics.open-telemetry.interval 30s && \\\n     stalwart_cli server reload-config; then\n    exit 0\n  fi\n  last_status=$?\n  sleep 1\ndone\nexit \"$last_status\"\n"]
      }

      resources {
        cpu = 100
        memory = 64
      }

      template {
        change_mode = "restart"
        destination = "secrets/admin.env"
        perms = "0600"
        data = <<-EOT
STALWART_ADMIN_PASSWORD={{ with secret "kv-runtime/data/secret/org/stalwart.admin_password" }}{{ .Data.data.value | toJSON }}{{ end }}
EOT
        env = true
      }
    }
  }
}
