job "garage" {
  name = "garage"
  datacenters = ["*"]
  type = "service"

  group "garage-0" {
    count = 1

    network {
      mode = "host"
      port "s3" {
        host_network = "loopback"
        static = 3900
        to = 3900
      }
      port "rpc" {
        host_network = "loopback"
        static = 3901
        to = 3901
      }
      port "admin" {
        host_network = "loopback"
        static = 3903
        to = 3903
      }
    }

    task "server" {
      driver = "raw_exec"
      user = "root"

      config {
        command = "/bin/bash"
        args = ["-euo", "pipefail", "-c", <<EOH
getent group garage >/dev/null || groupadd --system garage
id -u garage >/dev/null 2>&1 || useradd --system --gid garage --home-dir /var/lib/garage-0 --shell /usr/sbin/nologin --no-create-home garage
install -d -o root -g garage -m 0750 /etc/garage
install -d -o garage -g garage -m 0750 /var/lib/garage-0 /var/lib/garage-0/data /var/lib/garage-0/meta /var/lib/garage-0/snapshots
[ -s /etc/garage/rpc-secret ] || openssl rand -hex 32 >/etc/garage/rpc-secret
[ -s /etc/garage/admin-token ] || openssl rand -base64 48 | tr -d '\n' >/etc/garage/admin-token
[ -s /etc/garage/metrics-token ] || openssl rand -base64 48 | tr -d '\n' >/etc/garage/metrics-token
chown root:garage /etc/garage/rpc-secret /etc/garage/admin-token /etc/garage/metrics-token
chmod 0640 /etc/garage/rpc-secret /etc/garage/admin-token /etc/garage/metrics-token
cat >/etc/garage/garage-0.toml <<GARAGECONF
replication_factor = 3
consistency_mode = "consistent"
metadata_dir = "/var/lib/garage-0/meta"
data_dir = "/var/lib/garage-0/data"
metadata_snapshots_dir = "/var/lib/garage-0/snapshots"
metadata_fsync = true
data_fsync = true
db_engine = "lmdb"
block_size = "1M"
block_ram_buffer_max = "64MiB"
lmdb_map_size = "10G"
compression_level = 1
rpc_secret = "$(cat /etc/garage/rpc-secret)"
rpc_bind_addr = "127.0.0.1:3901"
rpc_public_addr = "127.0.0.1:3901"
allow_world_readable_secrets = false
[s3_api]
api_bind_addr = "127.0.0.1:3900"
s3_region = "garage"
[admin]
api_bind_addr = "127.0.0.1:3903"
metrics_token = "$(cat /etc/garage/metrics-token)"
metrics_require_token = true
admin_token = "$(cat /etc/garage/admin-token)"
GARAGECONF
chown root:garage /etc/garage/garage-0.toml
chmod 0640 /etc/garage/garage-0.toml
pkill -u garage -f 'garage-0.toml server' || true
exec /usr/sbin/runuser -u garage --preserve-environment -- "$${VERSELF_GARAGE_RUNTIME}/bin/garage" -c /etc/garage/garage-0.toml server
EOH
        ]
      }

      env {
        OTEL_EXPORTER_OTLP_ENDPOINT = "http://127.0.0.1:4317"
        OTEL_RESOURCE_ATTRIBUTES = "verself.supervisor=nomad"
        OTEL_SERVICE_NAME = "garage"
        VERSELF_GARAGE_RUNTIME = "verself-artifact://garage-runtime"
        VERSELF_SUPERVISOR = "nomad"
      }

      resources {
        cpu = 500
        memory = 512
      }

      restart {
        attempts = 3
        delay = "15s"
        interval = "300s"
        mode = "delay"
      }

      service {
        name = "garage-s3"
        port = "s3"
        provider = "nomad"
        address_mode = "auto"
        check {
          name = "garage-s3-tcp"
          type = "tcp"
          port = "s3"
          interval = "1s"
          timeout = "3s"
        }
      }

      service {
        name = "garage-admin"
        port = "admin"
        provider = "nomad"
        address_mode = "auto"
        check {
          name = "garage-admin-tcp"
          type = "tcp"
          port = "admin"
          interval = "1s"
          timeout = "3s"
        }
      }
    }

    update {
      max_parallel = 1
      health_check = "checks"
      min_healthy_time = "3s"
      healthy_deadline = "60s"
      progress_deadline = "90s"
      auto_revert = true
    }
  }
}
