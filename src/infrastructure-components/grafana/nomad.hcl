job "grafana" {
  name = "grafana"
  datacenters = ["*"]
  type = "service"

  group "grafana" {
    count = 1

    network {
      mode = "host"
      port "http" {
        host_network = "loopback"
        static = 4300
        to = 4300
      }
    }

    task "setup" {
      driver = "raw_exec"
      user = "root"

      lifecycle {
        hook = "prestart"
        sidecar = false
      }

      config {
        command = "/usr/bin/python3"
        args = ["-c", <<-PY
import grp
import os
import pathlib
import pwd
import subprocess

def ensure_group(name):
    try:
        return grp.getgrnam(name)
    except KeyError:
        subprocess.run(["/usr/sbin/groupadd", "--system", name], check=True)
        return grp.getgrnam(name)

def ensure_user(name, group):
    try:
        return pwd.getpwnam(name)
    except KeyError:
        subprocess.run([
            "/usr/sbin/useradd",
            "--system",
            "--gid", group.gr_name,
            "--home-dir", "/var/lib/grafana",
            "--shell", "/usr/sbin/nologin",
            "--no-create-home",
            name,
        ], check=True)
        return pwd.getpwnam(name)

group = ensure_group("grafana")
user = ensure_user("grafana", group)
for raw, mode in [
    ("/var/lib/grafana", 0o750),
    ("/var/lib/grafana/provisioning", 0o750),
    ("/var/lib/grafana/provisioning/dashboards", 0o750),
    ("/var/lib/grafana/provisioning/datasources", 0o750),
    ("/var/log/grafana", 0o750),
]:
    path = pathlib.Path(raw)
    path.mkdir(parents=True, exist_ok=True)
    os.chown(path, user.pw_uid, group.gr_gid)
    os.chmod(path, mode)
PY
        ]
      }

      resources {
        cpu = 50
        memory = 64
      }
    }

    task "server" {
      driver = "raw_exec"
      user = "root"
      vault {
        role = "grafana-runtime"
      }
      identity {
        name = "vault_default"
        aud  = ["vault.io"]
        ttl  = "1h"
      }

      config {
        command = "/bin/sh"
        args = ["-ec", "test -n \"$GF_SECURITY_ADMIN_PASSWORD\" || { echo 'missing Grafana OpenBao admin password' >&2; exit 1; }\ntest -n \"$GF_SECURITY_SECRET_KEY\" || { echo 'missing Grafana OpenBao secret key' >&2; exit 1; }\nruntime=\"$PWD/local/runtime\"\nrm -rf \"$runtime\"\nmkdir -p \"$runtime\"\ntar -xf \"$VERSELF_GRAFANA_RUNTIME/grafana-runtime.tar\" -C \"$runtime\"\ntest -d \"$runtime/plugins/grafana-clickhouse-datasource\"\ntest -f \"$runtime/plugins/grafana-clickhouse-datasource/CHANGELOG.md\" || printf 'Bundled from pinned grafana-clickhouse-datasource 4.17.0.\\n' > \"$runtime/plugins/grafana-clickhouse-datasource/CHANGELOG.md\"\nchown -R grafana:grafana \"$runtime\"\nexport GF_PATHS_PLUGINS=\"$runtime/plugins\"\ninstall -o grafana -g grafana -m 0640 secrets/grafana.ini local/grafana.ini\nexec /usr/sbin/runuser -u grafana --preserve-environment -- \"$runtime/grafana/bin/grafana\" server --homepath \"$runtime/grafana\" --config \"$PWD/local/grafana.ini\"\n"]
      }

      env {
        GF_DATABASE_HOST = "/var/run/postgresql"
        GF_DATABASE_NAME = "grafana"
        GF_DATABASE_SSL_MODE = "disable"
        GF_DATABASE_USER = "grafana"
        HOME = "/var/lib/grafana"
        PATH = "$${VERSELF_GRAFANA_RUNTIME}/grafana/bin:$${VERSELF_GRAFANA_RUNTIME}/bin:/usr/bin:/bin"
        OTEL_EXPORTER_OTLP_ENDPOINT = "http://127.0.0.1:4317"
        OTEL_RESOURCE_ATTRIBUTES = "verself.supervisor=nomad"
        OTEL_SERVICE_NAME = "grafana"
        VERSELF_SUPERVISOR = "nomad"
        VERSELF_GRAFANA_RUNTIME = "verself-artifact://grafana-runtime"
      }

      template {
        change_mode = "restart"
        destination = "secrets/runtime.env"
        perms = "0600"
        data = <<-EOT
GF_SECURITY_ADMIN_PASSWORD={{ with secret "kv-runtime/data/secret/org/grafana.admin_password" }}{{ .Data.data.value | toJSON }}{{ end }}
GF_SECURITY_SECRET_KEY={{ with secret "kv-runtime/data/secret/org/grafana.secret_key" }}{{ .Data.data.value | toJSON }}{{ end }}
EOT
        env = true
      }

      template {
        change_mode = "restart"
        destination = "secrets/grafana.ini"
        perms = "0600"
        data = <<-EOT
[paths]
data = /var/lib/grafana
logs = /var/log/grafana
plugins = $__env{GF_PATHS_PLUGINS}
provisioning = /var/lib/grafana/provisioning

[server]
protocol = http
http_addr = 127.0.0.1
http_port = 4300
domain = dashboard.__VERSELF_PRODUCT_DOMAIN__
root_url = https://dashboard.__VERSELF_PRODUCT_DOMAIN__/
serve_from_sub_path = false

[database]
type = postgres
host = $__env{GF_DATABASE_HOST}
name = $__env{GF_DATABASE_NAME}
user = $__env{GF_DATABASE_USER}
ssl_mode = disable

[security]
admin_user = admin
admin_password = $__env{GF_SECURITY_ADMIN_PASSWORD}
secret_key = $__env{GF_SECURITY_SECRET_KEY}
cookie_secure = true
cookie_samesite = strict
disable_gravatar = true
allow_embedding = false

[users]
allow_sign_up = false
auto_assign_org = true
auto_assign_org_role = Viewer

[analytics]
reporting_enabled = false
check_for_updates = false
check_for_plugin_updates = false
feedback_links_enabled = false

[plugins]
plugin_admin_enabled = false
plugin_admin_external_manage_enabled = false
plugin_catalog_url =
preinstall_disabled = true
preinstall_auto_update = false

[auth]
disable_login_form = false
oauth_auto_login = false
signout_redirect_url = https://dashboard.__VERSELF_PRODUCT_DOMAIN__/.pomerium/sign_out

[auth.jwt]
enabled = true
header_name = X-Pomerium-Jwt-Assertion
email_claim = email
username_claim = email
jwk_set_url = https://dashboard.__VERSELF_PRODUCT_DOMAIN__/.well-known/pomerium/jwks.json
cache_ttl = 60m
auto_sign_up = true
url_login = false

[auth.anonymous]
enabled = false

[log]
mode = console
level = info
EOT
      }

      resources {
        cpu = 600
        memory = 1024
      }

      service {
        name = "grafana-http"
        port = "http"
        provider = "nomad"
        address_mode = "auto"
      }
    }

    task "admin-password" {
      driver = "raw_exec"
      user = "root"
      vault {
        role = "grafana-runtime"
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

      config {
        command = "/bin/sh"
        args = ["-ec", "test -n \"$GF_SECURITY_ADMIN_PASSWORD\" || { echo 'missing Grafana OpenBao admin password' >&2; exit 1; }\ntest -n \"$GF_SECURITY_SECRET_KEY\" || { echo 'missing Grafana OpenBao secret key' >&2; exit 1; }\nruntime=\"$PWD/local/runtime\"\nrm -rf \"$runtime\"\nmkdir -p \"$runtime\"\ntar -xf \"$VERSELF_GRAFANA_RUNTIME/grafana-runtime.tar\" -C \"$runtime\"\ntest -d \"$runtime/plugins/grafana-clickhouse-datasource\"\ntest -f \"$runtime/plugins/grafana-clickhouse-datasource/CHANGELOG.md\" || printf 'Bundled from pinned grafana-clickhouse-datasource 4.17.0.\\n' > \"$runtime/plugins/grafana-clickhouse-datasource/CHANGELOG.md\"\nchown -R grafana:grafana \"$runtime\"\nexport GF_PATHS_PLUGINS=\"$runtime/plugins\"\ninstall -o grafana -g grafana -m 0640 secrets/grafana.ini local/grafana.ini\nlast_status=1\nfor attempt in $(seq 1 30); do\n  if /usr/sbin/runuser -u grafana --preserve-environment -- \"$runtime/grafana/bin/grafana\" cli --homepath \"$runtime/grafana\" --config \"$PWD/local/grafana.ini\" admin reset-admin-password \"$GF_SECURITY_ADMIN_PASSWORD\"; then\n    exit 0\n  fi\n  last_status=$?\n  sleep 1\ndone\nexit \"$last_status\"\n"]
      }

      env {
        GF_DATABASE_HOST = "/var/run/postgresql"
        GF_DATABASE_NAME = "grafana"
        GF_DATABASE_SSL_MODE = "disable"
        GF_DATABASE_USER = "grafana"
        HOME = "/var/lib/grafana"
        VERSELF_GRAFANA_RUNTIME = "verself-artifact://grafana-runtime"
      }

      template {
        change_mode = "restart"
        destination = "secrets/runtime.env"
        perms = "0600"
        data = <<-EOT
GF_SECURITY_ADMIN_PASSWORD={{ with secret "kv-runtime/data/secret/org/grafana.admin_password" }}{{ .Data.data.value | toJSON }}{{ end }}
GF_SECURITY_SECRET_KEY={{ with secret "kv-runtime/data/secret/org/grafana.secret_key" }}{{ .Data.data.value | toJSON }}{{ end }}
EOT
        env = true
      }

      template {
        change_mode = "restart"
        destination = "secrets/grafana.ini"
        perms = "0600"
        data = <<-EOT
[paths]
data = /var/lib/grafana
logs = /var/log/grafana
plugins = $__env{GF_PATHS_PLUGINS}
provisioning = /var/lib/grafana/provisioning

[server]
protocol = http
http_addr = 127.0.0.1
http_port = 4300
domain = dashboard.__VERSELF_PRODUCT_DOMAIN__
root_url = https://dashboard.__VERSELF_PRODUCT_DOMAIN__/
serve_from_sub_path = false

[database]
type = postgres
host = $__env{GF_DATABASE_HOST}
name = $__env{GF_DATABASE_NAME}
user = $__env{GF_DATABASE_USER}
ssl_mode = disable

[security]
admin_user = admin
admin_password = $__env{GF_SECURITY_ADMIN_PASSWORD}
secret_key = $__env{GF_SECURITY_SECRET_KEY}
cookie_secure = true
cookie_samesite = strict
disable_gravatar = true
allow_embedding = false

[users]
allow_sign_up = false
auto_assign_org = true
auto_assign_org_role = Viewer

[analytics]
reporting_enabled = false
check_for_updates = false
check_for_plugin_updates = false
feedback_links_enabled = false

[plugins]
plugin_admin_enabled = false
plugin_admin_external_manage_enabled = false
plugin_catalog_url =
preinstall_disabled = true
preinstall_auto_update = false

[auth]
disable_login_form = false
oauth_auto_login = false
signout_redirect_url = https://dashboard.__VERSELF_PRODUCT_DOMAIN__/.pomerium/sign_out

[auth.jwt]
enabled = true
header_name = X-Pomerium-Jwt-Assertion
email_claim = email
username_claim = email
jwk_set_url = https://dashboard.__VERSELF_PRODUCT_DOMAIN__/.well-known/pomerium/jwks.json
cache_ttl = 60m
auto_sign_up = true
url_login = false

[auth.anonymous]
enabled = false

[log]
mode = console
level = info
EOT
      }

      resources {
        cpu = 100
        memory = 128
      }
    }
  }
}
