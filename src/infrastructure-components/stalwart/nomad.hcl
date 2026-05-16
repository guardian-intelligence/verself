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
      user = "stalwart"

      config {
        command = "/opt/verself/profile/bin/stalwart"
        args = ["--config", "$${NOMAD_TASK_DIR}/config.toml"]
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

      # Stalwart configuration. Owned here so changes ride `aspect deploy`:
      # the rendered body is part of the job spec digest, so a config edit
      # resubmits the job and restarts Stalwart. Bootstrap/host concerns
      # (OS user, cap_net_bind_service, PG database, TLS material, DNS,
      # credentials, post-start Settings API + domain) remain in the
      # stalwart Ansible role. Receive-only: no outbound relay.
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

# session.*, remote.*, queue.*, metrics.* are pushed via the Settings API
# after startup by the stalwart Ansible role (tasks/settings.yml); Stalwart
# v0.15+ enforces the local/database split. http.* keys are pinned locally
# via config.local-keys because JMAP session URL generation parses them at
# startup. https://stalw.art/docs/configuration/overview/

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
  }
}
