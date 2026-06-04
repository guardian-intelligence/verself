job "openbao" {
  name = "openbao"
  datacenters = ["*"]
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
import shutil
import subprocess

def run(args):
    subprocess.run(args, check=True)

def ensure_group(name):
    try:
        import grp
        grp.getgrnam(name)
    except KeyError:
        run(["/usr/sbin/groupadd", "--system", name])

def ensure_user(name):
    try:
        pwd.getpwnam(name)
    except KeyError:
        run([
            "/usr/sbin/useradd",
            "--system",
            "--gid", name,
            "--home-dir", "/var/lib/openbao",
            "--shell", "/usr/sbin/nologin",
            "--no-create-home",
            name,
        ])

ensure_group("openbao")
ensure_user("openbao")
openbao = pwd.getpwnam("openbao")

def mkdir(path, uid, gid, mode):
    pathlib.Path(path).mkdir(parents=True, exist_ok=True)
    os.chown(path, uid, gid)
    os.chmod(path, mode)

mkdir("/etc/openbao", 0, openbao.pw_gid, 0o750)
mkdir("/etc/openbao/tls", 0, openbao.pw_gid, 0o750)
mkdir("/var/lib/verself", 0, 0, 0o755)
mkdir("/var/lib/verself/bootstrap", 0, 0, 0o700)
mkdir("/var/lib/verself/bootstrap/openbao", 0, 0, 0o700)
mkdir("/run/verself/bootstrap", 0, 0, 0o700)
mkdir("/var/lib/openbao", openbao.pw_uid, openbao.pw_gid, 0o700)
mkdir("/var/lib/openbao/raft", openbao.pw_uid, openbao.pw_gid, 0o700)
mkdir("/var/log/openbao", openbao.pw_uid, openbao.pw_gid, 0o700)

cert = pathlib.Path("/etc/openbao/tls/cert.pem")
key = pathlib.Path("/etc/openbao/tls/key.pem")
if not cert.exists() or not key.exists():
    tmp = pathlib.Path("/etc/openbao/tls/.next")
    shutil.rmtree(tmp, ignore_errors=True)
    tmp.mkdir(mode=0o700)
    run([
        "/usr/bin/openssl", "req", "-x509", "-newkey", "ec",
        "-pkeyopt", "ec_paramgen_curve:prime256v1",
        "-days", "3650", "-nodes",
        "-keyout", str(tmp / "key.pem"),
        "-out", str(tmp / "cert.pem"),
        "-subj", "/CN=127.0.0.1",
        "-addext", "subjectAltName=IP:127.0.0.1,DNS:localhost",
    ])
    os.replace(tmp / "key.pem", key)
    os.replace(tmp / "cert.pem", cert)
    shutil.rmtree(tmp, ignore_errors=True)
for path, mode in ((cert, 0o640), (key, 0o640)):
    os.chown(path, 0, openbao.pw_gid)
    os.chmod(path, mode)

public_ca_dir = pathlib.Path("/etc/verself/openbao")
public_ca_dir.mkdir(parents=True, exist_ok=True)
public_ca = public_ca_dir / "ca.pem"
shutil.copyfile(cert, public_ca)
os.chown(public_ca_dir, 0, 0)
os.chmod(public_ca_dir, 0o755)
os.chown(public_ca, 0, 0)
os.chmod(public_ca, 0o644)

hosts = pathlib.Path("/etc/openbao/hosts")
hosts.write_text("127.0.0.1 localhost\\n::1 localhost ip6-localhost ip6-loopback\\n", encoding="utf-8")
os.chown(hosts, 0, openbao.pw_gid)
os.chmod(hosts, 0o640)

config = pathlib.Path("/etc/openbao/openbao.hcl")
config.write_text("""ui = false
disable_mlock = true

api_addr = "https://127.0.0.1:8200"
cluster_addr = "https://127.0.0.1:8201"

storage "raft" {
  path = "/var/lib/openbao/raft"
  node_id = "verself-single-node"
}

listener "tcp" {
  address = "127.0.0.1:8200"
  cluster_address = "127.0.0.1:8201"
  tls_cert_file = "/etc/openbao/tls/cert.pem"
  tls_key_file = "/etc/openbao/tls/key.pem"
  tls_min_version = "tls13"

  telemetry {
    unauthenticated_metrics_access = true
  }
}

telemetry {
  prometheus_retention_time = "1m"
  disable_hostname = true
}

audit "file" "verself" {
  description = "Verself forensic backstop audit log"
  options {
    file_path = "/var/log/openbao/audit.log"
    mode = "0600"
  }
}
""", encoding="utf-8")
os.chown(config, 0, openbao.pw_gid)
os.chmod(config, 0o640)
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

      restart {
        attempts = 0
        mode = "fail"
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
        memory = 4096
      }

      service {
        name = "openbao-api"
        port = "api"
        provider = "nomad"
        address_mode = "auto"
      }
    }

    task "bootstrap" {
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
        command = "local/bin/openbao-bootstrap"
        args = [
          "--bao=local/bin/bao",
          "--state-dir=/var/lib/verself/bootstrap/openbao",
          "--site-root-token-file=/run/verself/bootstrap/openbao-site-root.token",
          "--addr=https://127.0.0.1:8200",
          "--ca-cert=/etc/openbao/tls/cert.pem",
        ]
      }

      env {
        BAO_ADDR = "https://127.0.0.1:8200"
        BAO_CACERT = "/etc/openbao/tls/cert.pem"
      }

      resources {
        cpu = 50
        # OpenBao returns unseal shares only once; keep enough headroom to wrap every share before exit.
        memory = 256
      }
    }
  }
}
