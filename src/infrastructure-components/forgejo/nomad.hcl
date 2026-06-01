job "forgejo" {
  name = "forgejo"
  datacenters = ["*"]
  type = "service"

  group "forgejo" {
    count = 1

    network {
      mode = "host"
      port "http" {
        host_network = "loopback"
        static = 3000
        to = 3000
      }
    }

    task "setup" {
      driver = "raw_exec"
      user = "root"
      vault {
        role = "forgejo-runtime"
      }
      identity {
        name = "vault_default"
        aud  = ["vault.io"]
        ttl  = "1h"
      }

      lifecycle {
        hook = "prestart"
        sidecar = false
      }

      artifact {
        source = "verself-artifact://forgejo-runtime"
        destination = "local"
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
            "--home-dir", "/var/lib/forgejo",
            "--shell", "/bin/bash",
            "--create-home",
            name,
        ], check=True)
        return pwd.getpwnam(name)

group = ensure_group("forgejo")
user = ensure_user("forgejo", group)
for raw, mode in [
    ("/var/lib/forgejo", 0o750),
    ("/var/lib/forgejo/data", 0o750),
    ("/var/lib/forgejo/log", 0o750),
    ("/var/lib/forgejo/repositories", 0o750),
]:
    path = pathlib.Path(raw)
    path.mkdir(parents=True, exist_ok=True)
    os.chown(path, user.pw_uid, group.gr_gid)
    os.chmod(path, mode)

cwd = pathlib.Path.cwd()
config = cwd / "local" / "app.ini"
subprocess.run(["/usr/bin/install", "-o", "forgejo", "-g", "forgejo", "-m", "0600", "secrets/app.ini", str(config)], check=True)
env = os.environ.copy()
env["HOME"] = "/var/lib/forgejo"
env["FORGEJO_WORK_DIR"] = "/var/lib/forgejo"
env["PATH"] = f"{cwd / 'local' / 'bin'}:/usr/bin:/bin"
forgejo = str(cwd / "local" / "bin" / "forgejo")
for args in [
    ["migrate", "--config", str(config), "--work-path", "/var/lib/forgejo"],
    ["admin", "regenerate", "keys", "--config", str(config)],
]:
    subprocess.run(["/usr/sbin/runuser", "-u", "forgejo", "--preserve-environment", "--", forgejo] + args, check=True, env=env)
PY
        ]
      }

      template {
        change_mode = "restart"
        destination = "secrets/app.ini"
        perms = "0600"
        data = <<-EOT
APP_NAME = Verself
RUN_MODE = prod
RUN_USER = forgejo
WORK_PATH = /var/lib/forgejo

[server]
DOMAIN = __VERSELF_FORGEJO_DOMAIN__
ROOT_URL = https://__VERSELF_FORGEJO_DOMAIN__/
HTTP_PORT = 3000
HTTP_ADDR = 127.0.0.1
SSH_DOMAIN = __VERSELF_FORGEJO_DOMAIN__
DISABLE_SSH = true
START_SSH_SERVER = false
LFS_START_SERVER = true
OFFLINE_MODE = true

[database]
DB_TYPE = sqlite3
PATH = /var/lib/forgejo/data/forgejo.db

[repository]
ROOT = /var/lib/forgejo/repositories
DEFAULT_BRANCH = main

[lfs]
PATH = /var/lib/forgejo/data/lfs

[log]
MODE = console
LEVEL = info
ROOT_PATH = /var/lib/forgejo/log

[security]
INSTALL_LOCK = true
SECRET_KEY = {{ with secret "kv-runtime/data/secret/org/forgejo.secret_key" }}{{ .Data.data.value }}{{ end }}
INTERNAL_TOKEN = {{ with secret "kv-runtime/data/secret/org/forgejo.internal_token" }}{{ .Data.data.value }}{{ end }}

[service]
DISABLE_REGISTRATION = true
ALLOW_ONLY_EXTERNAL_REGISTRATION = false
REQUIRE_SIGNIN_VIEW = true
ENABLE_BASIC_AUTHENTICATION = false
ENABLE_INTERNAL_SIGNIN = false

[oauth2]
ENABLED = false

[openid]
ENABLE_OPENID_SIGNIN = false
ENABLE_OPENID_SIGNUP = false

[actions]
ENABLED = true
EOT
      }
    }

    task "server" {
      driver = "raw_exec"
      user = "root"
      vault {
        role = "forgejo-runtime"
      }
      identity {
        name = "vault_default"
        aud  = ["vault.io"]
        ttl  = "1h"
      }

      artifact {
        source = "verself-artifact://forgejo-runtime"
        destination = "local"
      }

      config {
        command = "/bin/sh"
        args = ["-ec", "install -o forgejo -g forgejo -m 0600 secrets/app.ini local/app.ini\nexport HOME=/var/lib/forgejo\nexport FORGEJO_WORK_DIR=/var/lib/forgejo\nexport PATH=\"$PWD/local/bin:/usr/bin:/bin\"\nexec /usr/sbin/runuser -u forgejo --preserve-environment -- \"$PWD/local/bin/forgejo\" web --config \"$PWD/local/app.ini\"\n"]
      }

      env {
        HOME = "/var/lib/forgejo"
        FORGEJO_WORK_DIR = "/var/lib/forgejo"
        PATH = "$${NOMAD_TASK_DIR}/local/bin:/usr/bin:/bin"
        OTEL_EXPORTER_OTLP_ENDPOINT = "http://127.0.0.1:4317"
        OTEL_RESOURCE_ATTRIBUTES = "verself.supervisor=nomad"
        OTEL_SERVICE_NAME = "forgejo"
        VERSELF_SUPERVISOR = "nomad"
      }

      template {
        change_mode = "restart"
        destination = "secrets/app.ini"
        perms = "0600"
        data = <<-EOT
APP_NAME = Verself
RUN_MODE = prod
RUN_USER = forgejo
WORK_PATH = /var/lib/forgejo

[server]
DOMAIN = __VERSELF_FORGEJO_DOMAIN__
ROOT_URL = https://__VERSELF_FORGEJO_DOMAIN__/
HTTP_PORT = 3000
HTTP_ADDR = 127.0.0.1
SSH_DOMAIN = __VERSELF_FORGEJO_DOMAIN__
DISABLE_SSH = true
START_SSH_SERVER = false
LFS_START_SERVER = true
OFFLINE_MODE = true

[database]
DB_TYPE = sqlite3
PATH = /var/lib/forgejo/data/forgejo.db

[repository]
ROOT = /var/lib/forgejo/repositories
DEFAULT_BRANCH = main

[lfs]
PATH = /var/lib/forgejo/data/lfs

[log]
MODE = console
LEVEL = info
ROOT_PATH = /var/lib/forgejo/log

[security]
INSTALL_LOCK = true
SECRET_KEY = {{ with secret "kv-runtime/data/secret/org/forgejo.secret_key" }}{{ .Data.data.value }}{{ end }}
INTERNAL_TOKEN = {{ with secret "kv-runtime/data/secret/org/forgejo.internal_token" }}{{ .Data.data.value }}{{ end }}

[service]
DISABLE_REGISTRATION = true
ALLOW_ONLY_EXTERNAL_REGISTRATION = false
REQUIRE_SIGNIN_VIEW = true
ENABLE_BASIC_AUTHENTICATION = false
ENABLE_INTERNAL_SIGNIN = false

[oauth2]
ENABLED = false

[openid]
ENABLE_OPENID_SIGNIN = false
ENABLE_OPENID_SIGNUP = false

[actions]
ENABLED = true
EOT
      }

      resources {
        cpu = 600
        memory = 768
      }

      service {
        name = "forgejo-http"
        port = "http"
        provider = "nomad"
        address_mode = "auto"
      }
    }

    task "automation-token" {
      driver = "raw_exec"
      user = "root"
      vault {
        role = "forgejo-runtime"
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
        source = "verself-artifact://forgejo-runtime"
        destination = "local"
      }

      config {
        command = "/usr/bin/python3"
        args = ["-c", <<-PY
import hashlib
import json
import os
import pathlib
import ssl
import subprocess
import time
import urllib.error
import urllib.request

cwd = pathlib.Path.cwd()
forgejo = str(cwd / "local" / "bin" / "forgejo")
sqlite = str(cwd / "local" / "bin" / "sqlite3")
work_dir = "/var/lib/forgejo"
config = str(cwd / "local" / "app.ini")
db_path = "/var/lib/forgejo/data/forgejo.db"
username = "forgejo-automation"
secret_name = "source-code-hosting-service.forgejo.automation_token"

subprocess.run(["/usr/bin/install", "-o", "forgejo", "-g", "forgejo", "-m", "0600", "secrets/app.ini", config], check=True)

env = os.environ.copy()
env["HOME"] = work_dir
env["FORGEJO_WORK_DIR"] = work_dir
env["PATH"] = f"{cwd / 'local' / 'bin'}:/usr/bin:/bin"

def run_as_forgejo(args):
    return subprocess.run(
        ["/usr/sbin/runuser", "-u", "forgejo", "--preserve-environment", "--"] + args,
        check=True,
        text=True,
        capture_output=True,
        env=env,
    )

def forgejo_run(args):
    return run_as_forgejo([forgejo] + args)

def sqlite_value(sql):
    return run_as_forgejo([sqlite, db_path, sql]).stdout.strip()

def sqlite_exec(sql):
    run_as_forgejo([sqlite, db_path, sql])

def wait_ready():
    last_error = None
    for _ in range(60):
        try:
            forgejo_run(["admin", "user", "list", "--config", config])
            if pathlib.Path(db_path).exists():
                return
        except Exception as exc:
            last_error = exc
        time.sleep(1)
    raise SystemExit(f"forgejo automation token setup could not read Forgejo state: {last_error}")

def user_exists():
    return sqlite_value("SELECT 1 FROM user WHERE lower_name = 'forgejo-automation';") == "1"

def ensure_user():
    if not user_exists():
        forgejo_run([
            "admin", "user", "create",
            "--config", config,
            "--username", username,
            "--email", "forgejo-automation@__VERSELF_PRODUCT_DOMAIN__",
            "--fullname", "Forgejo Automation",
            "--random-password",
            "--random-password-length", "32",
        ])
    sqlite_exec("UPDATE user SET is_admin = 1 WHERE lower_name = 'forgejo-automation';")

def bao_client():
    token = os.environ.get("BAO_TOKEN") or os.environ.get("VAULT_TOKEN")
    if not token:
        raise SystemExit("OpenBao workload token is required")
    addr = (os.environ.get("BAO_ADDR") or os.environ.get("VAULT_ADDR") or "https://127.0.0.1:8200").rstrip("/")
    ca = os.environ.get("BAO_CACERT") or os.environ.get("VAULT_CACERT") or "/etc/verself/openbao/ca.pem"
    ctx = ssl.create_default_context(cafile=ca)
    return addr, token, ctx

def bao(method, path, body=None, expected=(200, 204)):
    addr, token, ctx = bao_client()
    data = None
    headers = {"X-Vault-Token": token}
    if body is not None:
        data = json.dumps(body).encode("utf-8")
        headers["Content-Type"] = "application/json"
    req = urllib.request.Request(
        addr + "/v1/" + path,
        data=data,
        headers=headers,
        method=method,
    )
    try:
        with urllib.request.urlopen(req, context=ctx, timeout=10) as resp:
            raw = resp.read()
            if resp.status not in expected:
                raise RuntimeError(f"openbao {method} {path} status {resp.status}: {raw[:512]!r}")
            return resp.status, json.loads(raw.decode("utf-8")) if raw else {}
    except urllib.error.HTTPError as exc:
        raw = exc.read()
        if exc.code in expected:
            return exc.code, json.loads(raw.decode("utf-8")) if raw else {}
        raise RuntimeError(f"openbao {method} {path} status {exc.code}: {raw[:512]!r}") from exc

def read_secret():
    status, response = bao("GET", "kv-runtime/data/secret/org/" + secret_name, expected=(200, 404))
    if status == 404:
        return ""
    return str(response.get("data", {}).get("data", {}).get("value", "")).strip()

def write_secret(value):
    bao("POST", "kv-runtime/data/secret/org/" + secret_name, {"data": {"value": value}})

wait_ready()
ensure_user()

existing = read_secret()
if existing:
    print("forgejo automation token already present sha256=" + hashlib.sha256(existing.encode("utf-8")).hexdigest())
    raise SystemExit(0)

token_name = "verself-automation-" + str(int(time.time()))
token = forgejo_run([
    "admin", "user", "generate-access-token",
    "--config", config,
    "--username", username,
    "--token-name", token_name,
    "--raw",
    "--scopes", "all",
]).stdout.strip()
if not token:
    raise SystemExit("Forgejo generated an empty automation token")
write_secret(token)
print("forgejo automation token written sha256=" + hashlib.sha256(token.encode("utf-8")).hexdigest())
PY
        ]
      }

      env {
        BAO_ADDR = "https://127.0.0.1:8200"
        BAO_CACERT = "/etc/verself/openbao/ca.pem"
      }

      template {
        change_mode = "restart"
        destination = "secrets/app.ini"
        perms = "0600"
        data = <<-EOT
APP_NAME = Verself
RUN_MODE = prod
RUN_USER = forgejo
WORK_PATH = /var/lib/forgejo

[server]
DOMAIN = __VERSELF_FORGEJO_DOMAIN__
ROOT_URL = https://__VERSELF_FORGEJO_DOMAIN__/
HTTP_PORT = 3000
HTTP_ADDR = 127.0.0.1
SSH_DOMAIN = __VERSELF_FORGEJO_DOMAIN__
DISABLE_SSH = true
START_SSH_SERVER = false
LFS_START_SERVER = true
OFFLINE_MODE = true

[database]
DB_TYPE = sqlite3
PATH = /var/lib/forgejo/data/forgejo.db

[repository]
ROOT = /var/lib/forgejo/repositories
DEFAULT_BRANCH = main

[lfs]
PATH = /var/lib/forgejo/data/lfs

[log]
MODE = console
LEVEL = info
ROOT_PATH = /var/lib/forgejo/log

[security]
INSTALL_LOCK = true
SECRET_KEY = {{ with secret "kv-runtime/data/secret/org/forgejo.secret_key" }}{{ .Data.data.value }}{{ end }}
INTERNAL_TOKEN = {{ with secret "kv-runtime/data/secret/org/forgejo.internal_token" }}{{ .Data.data.value }}{{ end }}

[service]
DISABLE_REGISTRATION = true
ALLOW_ONLY_EXTERNAL_REGISTRATION = false
REQUIRE_SIGNIN_VIEW = true
ENABLE_BASIC_AUTHENTICATION = false
ENABLE_INTERNAL_SIGNIN = false

[oauth2]
ENABLED = false

[openid]
ENABLE_OPENID_SIGNIN = false
ENABLE_OPENID_SIGNUP = false

[actions]
ENABLED = true
EOT
      }

      resources {
        cpu = 100
        memory = 128
      }
    }
  }
}
