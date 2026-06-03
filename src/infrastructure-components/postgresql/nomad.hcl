job "postgresql" {
  name = "postgresql"
  datacenters = ["*"]
  type = "service"

  group "postgresql" {
    count = 1

    network {
      mode = "host"
      port "postgres" {
        host_network = "loopback"
        static = 5432
        to = 5432
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
        command = "/bin/bash"
        args = ["-euo", "pipefail", "-c", <<EOH
runtime="$(realpath "$${VERSELF_POSTGRESQL_RUNTIME}/opt/verself/postgresql")"
export LD_LIBRARY_PATH="$runtime/usr/lib/x86_64-linux-gnu:$runtime/usr/lib/postgresql/16/lib"

getent group postgres >/dev/null || groupadd --system postgres
id -u postgres >/dev/null 2>&1 || useradd --system --gid postgres --home-dir /var/lib/postgresql --shell /bin/bash --create-home postgres
install -d -o postgres -g postgres -m 0700 /var/lib/postgresql/16/verself /etc/postgresql/verself
install -d -o postgres -g adm -m 2755 /var/log/postgresql
install -d -o postgres -g postgres -m 0755 /var/run/postgresql

cat >/etc/postgresql/verself/postgresql.conf <<PGCONF
listen_addresses = '127.0.0.1'
port = 5432
max_connections = 300
superuser_reserved_connections = 10
data_directory = '/var/lib/postgresql/16/verself'
hba_file = '/etc/postgresql/verself/pg_hba.conf'
ident_file = '/etc/postgresql/verself/pg_ident.conf'
dynamic_library_path = '$runtime/usr/lib/postgresql/16/lib'
logging_collector = on
log_directory = '/var/log/postgresql'
log_filename = 'postgresql-%Y-%m-%d.log'
log_file_mode = 0640
log_rotation_age = 1d
log_rotation_size = 100MB
log_min_messages = warning
log_line_prefix = '%m [%p] %q%u@%d '
password_encryption = scram-sha-256
shared_buffers = 256MB
work_mem = 4MB
maintenance_work_mem = 64MB
effective_cache_size = 512MB
wal_level = logical
track_commit_timestamp = on
max_wal_size = 1GB
min_wal_size = 80MB
unix_socket_directories = '/var/run/postgresql'
PGCONF

cat >/etc/postgresql/verself/pg_hba.conf <<PGHBA
local   all       all                peer map=verself_services
host    all       all   127.0.0.1/32 scram-sha-256
host    all       all   ::1/128      scram-sha-256
PGHBA

# substrate-control-plane owns service peer mappings; Postgres only seeds bootstrap auth.
if [ ! -e /etc/postgresql/verself/pg_ident.conf ]; then
  cat >/etc/postgresql/verself/pg_ident.conf <<PGIDENT
verself_services      postgres            postgres
PGIDENT
fi

chown postgres:postgres /etc/postgresql/verself/postgresql.conf /etc/postgresql/verself/pg_hba.conf /etc/postgresql/verself/pg_ident.conf
chmod 0600 /etc/postgresql/verself/postgresql.conf /etc/postgresql/verself/pg_hba.conf /etc/postgresql/verself/pg_ident.conf

if [ ! -s /var/lib/postgresql/16/verself/PG_VERSION ]; then
  pwfile="$(mktemp /run/postgresql/initdb-password.XXXXXX)"
  trap 'rm -f "$pwfile"' EXIT
  openssl rand -base64 48 >"$pwfile"
  chown postgres:postgres "$pwfile"
  chmod 0600 "$pwfile"
  runuser -u postgres --preserve-environment -- "$runtime/usr/lib/postgresql/16/bin/initdb" \
    --pgdata=/var/lib/postgresql/16/verself \
    --auth-local=peer \
    --auth-host=scram-sha-256 \
    --encoding=UTF8 \
    --locale=C.UTF-8 \
    -L "$runtime/usr/share/postgresql/16" \
    --pwfile="$pwfile"
fi
EOH
        ]
      }

      env {
        LD_LIBRARY_PATH = "$${VERSELF_POSTGRESQL_RUNTIME}/opt/verself/postgresql/usr/lib/x86_64-linux-gnu:$${VERSELF_POSTGRESQL_RUNTIME}/opt/verself/postgresql/usr/lib/postgresql/16/lib"
        VERSELF_POSTGRESQL_RUNTIME = "verself-artifact://postgresql-runtime"
      }

      resources {
        cpu = 100
        memory = 128
      }
    }

    task "server" {
      driver = "raw_exec"
      user = "postgres"

      config {
        command = "$${VERSELF_POSTGRESQL_RUNTIME}/opt/verself/postgresql/usr/lib/postgresql/16/bin/postgres"
        args = ["-D", "/var/lib/postgresql/16/verself", "-c", "config_file=/etc/postgresql/verself/postgresql.conf"]
      }

      env {
        LD_LIBRARY_PATH = "$${VERSELF_POSTGRESQL_RUNTIME}/opt/verself/postgresql/usr/lib/x86_64-linux-gnu:$${VERSELF_POSTGRESQL_RUNTIME}/opt/verself/postgresql/usr/lib/postgresql/16/lib"
        OTEL_EXPORTER_OTLP_ENDPOINT = "http://127.0.0.1:4317"
        OTEL_RESOURCE_ATTRIBUTES = "verself.supervisor=nomad"
        OTEL_SERVICE_NAME = "postgresql"
        VERSELF_POSTGRESQL_RUNTIME = "verself-artifact://postgresql-runtime"
        VERSELF_SUPERVISOR = "nomad"
      }

      resources {
        cpu = 400
        memory = 1024
      }

      service {
        name = "postgresql"
        port = "postgres"
        provider = "nomad"
        address_mode = "auto"
      }
    }
  }
}
