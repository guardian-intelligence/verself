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
mkdir("/var/lib/openbao", openbao.pw_uid, openbao.pw_gid, 0o700)
mkdir("/var/lib/openbao/raft", openbao.pw_uid, openbao.pw_gid, 0o700)
mkdir("/var/log/openbao", openbao.pw_uid, openbao.pw_gid, 0o700)
mkdir("/etc/credstore/openbao", 0, openbao.pw_gid, 0o750)

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
        memory = 512
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
        command = "/usr/bin/python3"
        args = ["-c", <<-PY
import grp
import json
import os
import pathlib
import subprocess
import sys
import time
import ssl
import urllib.error
import urllib.request

bao = pathlib.Path("local/bin/bao")
credstore = pathlib.Path("/etc/credstore/openbao")
credstore.mkdir(mode=0o750, parents=True, exist_ok=True)
openbao_gid = grp.getgrnam("openbao").gr_gid
os.chown(credstore, 0, openbao_gid)
os.chmod(credstore, 0o750)

def run(args, env=None):
    return subprocess.run([str(bao)] + args, check=True, text=True, capture_output=True, env=env)

def bao_status():
    proc = subprocess.run([str(bao), "status", "-format=json"], check=False, text=True, capture_output=True)
    if proc.stdout.strip():
        return json.loads(proc.stdout)
    raise RuntimeError(f"bao status exited {proc.returncode}: {proc.stderr.strip()}")

status = None
last_error = None
for _ in range(45):
    try:
        status = bao_status()
        break
    except Exception as exc:
        last_error = exc
        time.sleep(1)
if status is None:
    raise SystemExit(f"openbao status did not become readable: {last_error}")

def write_secret(name, value):
    path = credstore / name
    path.write_text(value.strip() + "\n", encoding="utf-8")
    os.chown(path, 0, openbao_gid)
    os.chmod(path, 0o640)

root_token = None
if not status.get("initialized", False):
    init = json.loads(run(["operator", "init", "-key-shares=3", "-key-threshold=2", "-format=json"]).stdout)
    keys = init.get("unseal_keys_b64") or init.get("keys_base64") or []
    if len(keys) < 2 or not init.get("root_token"):
        raise SystemExit("openbao init response did not include root token and at least two unseal keys")
    root_token = init["root_token"]
    for index, key in enumerate(keys, start=1):
        write_secret(f"unseal-key-{index}", key)
    status = bao_status()

if status.get("sealed", True):
    for index in (1, 2):
        path = credstore / f"unseal-key-{index}"
        if not path.exists():
            raise SystemExit(f"{path} is required to unseal initialized OpenBao")
        run(["operator", "unseal", path.read_text(encoding="utf-8").strip()])

if root_token:
    # The operator init root token is only valid inside this bootstrap
    # transaction: create the site-local workload identity path, then revoke it.
    api_ctx = ssl.create_default_context(cafile="/etc/openbao/tls/cert.pem")

    def api(method, path, body=None, expected=(200, 204)):
        data = None
        headers = {"X-Vault-Token": root_token}
        if body is not None:
            data = json.dumps(body).encode("utf-8")
            headers["Content-Type"] = "application/json"
        req = urllib.request.Request(
            "https://127.0.0.1:8200/v1/" + path,
            data=data,
            headers=headers,
            method=method,
        )
        try:
            with urllib.request.urlopen(req, context=api_ctx, timeout=10) as resp:
                raw = resp.read()
                if resp.status not in expected:
                    raise RuntimeError(f"openbao {method} {path} status {resp.status}: {raw[:512]!r}")
                if raw:
                    return json.loads(raw.decode("utf-8"))
                return {}
        except urllib.error.HTTPError as exc:
            raw = exc.read()
            if exc.code in expected:
                if raw:
                    return json.loads(raw.decode("utf-8"))
                return {}
            raise RuntimeError(f"openbao {method} {path} status {exc.code}: {raw[:512]!r}") from exc

    mounts = api("GET", "sys/mounts").get("data", {})
    if "kv-runtime/" not in mounts:
        api("POST", "sys/mounts/kv-runtime", {"type": "kv", "options": {"version": "2"}})

    auth = api("GET", "sys/auth").get("data", {})
    if "jwt-nomad/" not in auth:
        api("POST", "sys/auth/jwt-nomad", {"type": "jwt", "description": "Verself Nomad workload identity auth"})
    api("POST", "auth/jwt-nomad/config", {
        "jwks_url": "http://127.0.0.1:4646/.well-known/jwks.json",
        "jwt_supported_algs": ["RS256", "EdDSA"],
    })

    policy = """
path "sys/policies/acl/*" {
  capabilities = ["create", "update", "read", "list"]
}

path "auth/jwt-nomad/role/*" {
  capabilities = ["create", "update", "read", "list"]
}

path "kv-runtime/data/secret/org/*" {
  capabilities = ["create", "update", "read", "delete"]
}

path "kv-runtime/metadata/secret/org/*" {
  capabilities = ["read", "list", "delete"]
}
""".strip()
    api("POST", "sys/policies/acl/substrate-control-plane", {"policy": policy})
    api("POST", "auth/jwt-nomad/role/substrate-control-plane", {
        "role_type": "jwt",
        "bound_audiences": ["vault.io"],
        "user_claim": "/nomad_job_id",
        "user_claim_json_pointer": True,
        "claim_mappings": {
            "nomad_namespace": "nomad_namespace",
            "nomad_job_id": "nomad_job_id",
            "nomad_task": "nomad_task",
        },
        "bound_claims": {"nomad_job_id": "substrate-control-plane"},
        "token_type": "service",
        "token_policies": ["substrate-control-plane"],
        "token_period": "30m",
        "token_explicit_max_ttl": 0,
    })

    env = os.environ.copy()
    env["BAO_TOKEN"] = root_token
    run(["token", "revoke", "-self"], env=env)
PY
        ]
      }

      env {
        BAO_ADDR = "https://127.0.0.1:8200"
        BAO_CACERT = "/etc/openbao/tls/cert.pem"
      }

      resources {
        cpu = 50
        memory = 64
      }
    }
  }
}
