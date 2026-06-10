job "otelcol" {
  name = "otelcol"
  datacenters = ["*"]
  type = "service"

  group "otelcol" {
    count = 1

    network {
      mode = "host"
      port "otlp_grpc" {
        host_network = "loopback"
        static = 4317
        to = 4317
      }
      port "otlp_http" {
        host_network = "loopback"
        static = 4318
        to = 4318
      }
      port "health" {
        host_network = "loopback"
        static = 13133
        to = 13133
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
import os
import pathlib
import pwd

otelcol = pwd.getpwnam("otelcol")

def mkdir(path, mode):
    pathlib.Path(path).mkdir(parents=True, exist_ok=True)
    os.chown(path, otelcol.pw_uid, otelcol.pw_gid)
    os.chmod(path, mode)

mkdir("/var/lib/otelcol", 0o750)
mkdir("/var/lib/otelcol/storage", 0o750)
mkdir("/var/lib/otelcol/clickhouse-spiffe", 0o700)

ready = pathlib.Path("/var/lib/otelcol/clickhouse-spiffe/.ready")
ready.write_text("ok\n", encoding="utf-8")
os.chown(ready, otelcol.pw_uid, otelcol.pw_gid)
PY
        ]
      }

      resources {
        cpu = 50
        memory = 64
      }
    }

    task "clickhouse-spiffe-helper" {
      driver = "raw_exec"
      user = "otelcol"

      lifecycle {
        hook = "prestart"
        sidecar = true
      }

      artifact {
        source = "verself-artifact://otelcol-config"
        destination = "local"
        chown = true
      }

      artifact {
        source = "verself-artifact://otelcol-runtime"
        destination = "local"
        chown = true
      }

      config {
        # The setup prestart races this sidecar; the helper only retries its
        # cert dump on SVID rotation, so wait for setup's marker first
        # (wait-files-exec only accepts non-empty regular files).
        command = "local/bin/wait-files-exec"
        args = [
          "--timeout=30s",
          "/var/lib/otelcol/clickhouse-spiffe/.ready",
          "--",
          "local/bin/spiffe-helper",
          "-config",
          "local/config/clickhouse-spiffe-helper.conf",
        ]
      }

      resources {
        cpu = 50
        memory = 64
      }
    }

    task "collector" {
      driver = "raw_exec"
      user = "otelcol"

      artifact {
        source = "verself-artifact://otelcol-config"
        destination = "local"
        chown = true
      }

      artifact {
        source = "verself-artifact://otelcol-runtime"
        destination = "local"
        chown = true
      }

      config {
        command = "local/bin/wait-files-exec"
        args = [
          "--timeout=30s",
          "/var/lib/otelcol/clickhouse-spiffe/svid.pem",
          "/var/lib/otelcol/clickhouse-spiffe/svid_key.pem",
          "/var/lib/otelcol/clickhouse-spiffe/bundle.pem",
          "--",
          "local/bin/otelcol-contrib",
          "--config",
          "local/config/config.yaml",
        ]
      }

      env {
        OTEL_RESOURCE_ATTRIBUTES = "verself.supervisor=nomad"
        OTEL_SERVICE_NAME = "otelcol"
        VERSELF_SUPERVISOR = "nomad"
      }

      resources {
        cpu = 600
        memory = 2048
      }

      service {
        name = "otelcol-otlp-grpc"
        port = "otlp_grpc"
        provider = "nomad"
        address_mode = "auto"
      }

      service {
        name = "otelcol-otlp-http"
        port = "otlp_http"
        provider = "nomad"
        address_mode = "auto"
      }
    }
  }
}
