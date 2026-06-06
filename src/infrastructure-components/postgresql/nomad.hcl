variable "guardian_repo_root" {
  type    = string
  default = "/home/ubuntu/.local/state/guardian/repo/current"
}

variable "postgresql_resource_name" {
  type    = string
  default = "postgresql"
}

job "postgresql" {
  name        = "postgresql"
  datacenters = ["*"]
  type        = "service"

  group "postgresql" {
    count = 1

    network {
      mode = "host"
      port "postgres" {
        host_network = "loopback"
        static       = 5432
        to           = 5432
      }
    }

    task "setup" {
      driver = "raw_exec"
      user   = "root"

      lifecycle {
        hook    = "prestart"
        sidecar = false
      }

      config {
        command = "/usr/bin/python3"
        args = ["-c", <<-PY
import fcntl
import grp
import hashlib
import json
import os
import pathlib
import pwd
import re
import shutil
import subprocess
import tarfile

repo_root = pathlib.Path("${var.guardian_repo_root}")
resource_name = "${var.postgresql_resource_name}"
doc_path = repo_root / "workspace/.guardian/fly/document.json"
projected_doc_path = pathlib.Path("/run/verself/recovery/postgresql/document.json")
postgres_major = "16"
identifier_pattern = re.compile(r"^[A-Za-z_][A-Za-z0-9_]*$")

def run(args, **kwargs):
    subprocess.run(args, check=True, **kwargs)

def load_resource():
    try:
        doc = json.loads(doc_path.read_text(encoding="utf-8"))
    except Exception as exc:
        raise SystemExit(f"load Guardian resource graph {doc_path}: {exc}") from exc
    matches = [
        resource for resource in doc.get("resources", [])
        if resource.get("apiVersion") == "postgresql.guardianintelligence.org/v1alpha1"
        and resource.get("kind") == "PostgreSQLCluster"
        and (resource.get("metadata") or {}).get("name") == resource_name
    ]
    if len(matches) != 1:
        raise SystemExit(f"expected exactly one PostgreSQLCluster resource named {resource_name!r}, found {len(matches)}")
    return doc, matches[0].get("spec") or {}

def required_path(spec, key):
    value = spec.get(key)
    if not isinstance(value, str) or not value.startswith("/"):
        raise SystemExit(f"PostgreSQLCluster.spec.{key} must be an absolute path")
    return pathlib.Path(value)

def required_repo_path(spec, key):
    value = spec.get(key)
    if not isinstance(value, str) or value.startswith("/") or ".." in pathlib.PurePosixPath(value).parts:
        raise SystemExit(f"PostgreSQLCluster.spec.{key} must be a repo-relative path")
    return repo_root / value

def required_int(spec, key):
    value = spec.get(key)
    if not isinstance(value, int) or value <= 0:
        raise SystemExit(f"PostgreSQLCluster.spec.{key} must be a positive integer")
    return value

def check_identifier(value, field):
    if not isinstance(value, str) or not identifier_pattern.match(value):
        raise SystemExit(f"{field} must be a PostgreSQL identifier")
    return value

def ensure_group(name):
    try:
        grp.getgrnam(name)
    except KeyError:
        run(["/usr/sbin/groupadd", "--system", name])

def ensure_user(name):
    try:
        pwd.getpwnam(name)
        return
    except KeyError:
        pass
    run([
        "/usr/sbin/useradd",
        "--system",
        "--gid", "postgres",
        "--home-dir", "/var/lib/postgresql",
        "--shell", "/bin/bash",
        "--create-home",
        name,
    ])

def mkdir(path, uid, gid, mode):
    path.mkdir(parents=True, exist_ok=True)
    os.chown(path, uid, gid)
    os.chmod(path, mode)

def sha256_file(path):
    h = hashlib.sha256()
    with path.open("rb") as f:
        for chunk in iter(lambda: f.read(1024 * 1024), b""):
            h.update(chunk)
    return h.hexdigest()

def safe_extract_tar(path, dest):
    dest_resolved = dest.resolve()
    with tarfile.open(path, "r") as tf:
        for member in tf.getmembers():
            target = (dest / member.name).resolve()
            if target != dest_resolved and not str(target).startswith(str(dest_resolved) + os.sep):
                raise SystemExit(f"unsafe PostgreSQL runtime artifact member: {member.name}")
            if member.issym() or member.islnk():
                raise SystemExit(f"PostgreSQL runtime artifact links are not allowed: {member.name}")
        tf.extractall(dest)

def install_runtime(artifact, runtime_root, postgres_gid):
    if not artifact.is_file():
        raise SystemExit(f"missing PostgreSQL runtime artifact: {artifact}")
    mkdir(runtime_root, 0, postgres_gid, 0o755)
    mkdir(runtime_root / "releases", 0, postgres_gid, 0o755)
    mkdir(runtime_root / "tmp", 0, postgres_gid, 0o755)
    lock_path = runtime_root / "install.lock"
    with lock_path.open("w") as lock:
        fcntl.flock(lock, fcntl.LOCK_EX)
        release = runtime_root / "releases" / ("sha256-" + sha256_file(artifact))
        tmp = runtime_root / "tmp" / (release.name + "." + str(os.getpid()))
        if not (release / "usr/lib/postgresql" / postgres_major / "bin/postgres").is_file():
            shutil.rmtree(tmp, ignore_errors=True)
            tmp.mkdir(parents=True, mode=0o755)
            safe_extract_tar(artifact, tmp)
            extracted = tmp / "opt/verself/postgresql"
            if not (extracted / "usr/lib/postgresql" / postgres_major / "bin/postgres").is_file():
                raise SystemExit(f"PostgreSQL runtime artifact missing postgres binary under {extracted}")
            shutil.rmtree(release, ignore_errors=True)
            os.replace(extracted, release)
            shutil.rmtree(tmp, ignore_errors=True)
        else:
            shutil.rmtree(tmp, ignore_errors=True)
        next_link = runtime_root / "current.next"
        current_link = runtime_root / "current"
        try:
            next_link.unlink()
        except FileNotFoundError:
            pass
        os.symlink(release, next_link)
        if current_link.exists() and not current_link.is_symlink():
            shutil.rmtree(current_link)
        os.replace(next_link, current_link)

def runtime_env(runtime_root):
    current = runtime_root / "current"
    return {
        **os.environ,
        "HOME": "/var/lib/postgresql",
        "LD_LIBRARY_PATH": str(current / "usr/lib/x86_64-linux-gnu") + ":" + str(current / "usr/lib/postgresql" / postgres_major / "lib"),
    }

def write_config(spec, runtime_root, data_dir, config_dir, log_dir, socket_dir):
    listen_address = spec.get("listenAddress")
    if not isinstance(listen_address, str) or not listen_address:
        raise SystemExit("PostgreSQLCluster.spec.listenAddress is required")
    port = required_int(spec, "port")
    max_connections = required_int(spec, "maxConnections")
    superuser_reserved = spec.get("superuserReservedConnections")
    if not isinstance(superuser_reserved, int) or superuser_reserved < 0 or superuser_reserved >= max_connections:
        raise SystemExit("PostgreSQLCluster.spec.superuserReservedConnections must be >= 0 and less than maxConnections")
    pg_lib = runtime_root / "current/usr/lib/postgresql" / postgres_major / "lib"
    postgresql_conf = f"""listen_addresses = '{listen_address}'
port = {port}
max_connections = {max_connections}
superuser_reserved_connections = {superuser_reserved}
data_directory = '{data_dir}'
hba_file = '{config_dir / "pg_hba.conf"}'
ident_file = '{config_dir / "pg_ident.conf"}'
dynamic_library_path = '{pg_lib}'
logging_collector = on
log_directory = '{log_dir}'
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
unix_socket_directories = '{socket_dir}'
"""
    pg_hba = """local   all       all                peer map=verself_services
host    all       all   127.0.0.1/32 scram-sha-256
host    all       all   ::1/128      scram-sha-256
"""
    mappings = [{"systemUser": "postgres", "postgresUser": "postgres"}]
    mappings.extend(spec.get("peerMappings") or [])
    seen = set()
    pg_ident_lines = ["verself_services      postgres                 postgres"]
    for mapping in sorted(mappings, key=lambda item: (item.get("systemUser", ""), item.get("postgresUser", ""))):
        system_user = check_identifier(mapping.get("systemUser"), "PostgreSQLCluster.spec.peerMappings[].systemUser")
        postgres_user = check_identifier(mapping.get("postgresUser"), "PostgreSQLCluster.spec.peerMappings[].postgresUser")
        key = (system_user, postgres_user)
        if key in seen:
            continue
        seen.add(key)
        if key == ("postgres", "postgres"):
            continue
        pg_ident_lines.append(f"verself_services      {system_user:<24} {postgres_user}")
    files = {
        config_dir / "postgresql.conf": postgresql_conf,
        config_dir / "pg_hba.conf": pg_hba,
        config_dir / "pg_ident.conf": "\n".join(pg_ident_lines) + "\n",
    }
    for path, content in files.items():
        tmp = path.with_name(path.name + "." + str(os.getpid()) + ".tmp")
        tmp.write_text(content, encoding="utf-8")
        os.chown(tmp, pwd.getpwnam("postgres").pw_uid, pwd.getpwnam("postgres").pw_gid)
        os.chmod(tmp, 0o600)
        os.replace(tmp, path)

def project_document(doc, postgres_uid, postgres_gid):
    pathlib.Path("/run/verself").mkdir(parents=True, exist_ok=True)
    os.chown("/run/verself", 0, 0)
    os.chmod("/run/verself", 0o755)
    pathlib.Path("/run/verself/recovery").mkdir(parents=True, exist_ok=True)
    os.chown("/run/verself/recovery", 0, 0)
    os.chmod("/run/verself/recovery", 0o711)
    projected_doc_path.parent.mkdir(parents=True, exist_ok=True)
    os.chown(projected_doc_path.parent, postgres_uid, postgres_gid)
    os.chmod(projected_doc_path.parent, 0o750)
    tmp_doc_path = projected_doc_path.with_name(projected_doc_path.name + "." + str(os.getpid()) + ".tmp")
    tmp_doc_path.write_text(json.dumps(doc, sort_keys=True, separators=(",", ":")) + "\n", encoding="utf-8")
    os.chown(tmp_doc_path, postgres_uid, postgres_gid)
    os.chmod(tmp_doc_path, 0o640)
    os.replace(tmp_doc_path, projected_doc_path)

doc, spec = load_resource()
ensure_group("postgres")
ensure_user("postgres")
postgres = pwd.getpwnam("postgres")
artifact = required_repo_path(spec, "runtimeArtifact")
runtime_root = required_path(spec, "runtimeRoot")
data_dir = required_path(spec, "dataDir")
config_dir = required_path(spec, "configDir")
log_dir = required_path(spec, "logDir")
socket_dir = required_path(spec, "socketDir")
report_path = required_path(spec, "reportPath")
if report_path.parent != projected_doc_path.parent:
    raise SystemExit("PostgreSQLCluster.spec.reportPath must be under /run/verself/recovery/postgresql")

mkdir(data_dir, postgres.pw_uid, postgres.pw_gid, 0o700)
mkdir(config_dir, postgres.pw_uid, postgres.pw_gid, 0o700)
mkdir(log_dir, postgres.pw_uid, grp.getgrnam("adm").gr_gid, 0o2755)
mkdir(socket_dir, postgres.pw_uid, postgres.pw_gid, 0o755)
install_runtime(artifact, runtime_root, postgres.pw_gid)
write_config(spec, runtime_root, data_dir, config_dir, log_dir, socket_dir)
project_document(doc, postgres.pw_uid, postgres.pw_gid)

if not (data_dir / "PG_VERSION").is_file():
    pwfile = socket_dir / ("initdb-password." + str(os.getpid()))
    pwfile.write_text(subprocess.check_output(["/usr/bin/openssl", "rand", "-base64", "48"], text=True), encoding="utf-8")
    os.chown(pwfile, postgres.pw_uid, postgres.pw_gid)
    os.chmod(pwfile, 0o600)
    try:
        run([
            "/usr/sbin/runuser",
            "-u", "postgres",
            "--",
            "/usr/bin/env",
            "HOME=/var/lib/postgresql",
            "LD_LIBRARY_PATH=" + runtime_env(runtime_root)["LD_LIBRARY_PATH"],
            str(runtime_root / "current/usr/lib/postgresql" / postgres_major / "bin/initdb"),
            "--pgdata=" + str(data_dir),
            "--auth-local=peer",
            "--auth-host=scram-sha-256",
            "--encoding=UTF8",
            "--locale=C.UTF-8",
            "-L", str(runtime_root / "current/usr/share/postgresql" / postgres_major),
            "--pwfile=" + str(pwfile),
        ])
    finally:
        try:
            pwfile.unlink()
        except FileNotFoundError:
            pass
PY
        ]
      }

      resources {
        cpu    = 100
        memory = 256
      }
    }

    task "server" {
      driver = "raw_exec"
      user   = "postgres"

      config {
        command = "/usr/bin/python3"
        args = ["-c", <<-PY
import json
import os
import pathlib

resource_name = "${var.postgresql_resource_name}"
doc_path = pathlib.Path("/run/verself/recovery/postgresql/document.json")
postgres_major = "16"

doc = json.loads(doc_path.read_text(encoding="utf-8"))
resources = [
    resource for resource in doc.get("resources", [])
    if resource.get("apiVersion") == "postgresql.guardianintelligence.org/v1alpha1"
    and resource.get("kind") == "PostgreSQLCluster"
    and (resource.get("metadata") or {}).get("name") == resource_name
]
if len(resources) != 1:
    raise SystemExit(f"expected exactly one PostgreSQLCluster resource named {resource_name!r}, found {len(resources)}")
spec = resources[0].get("spec") or {}
runtime_root = pathlib.Path(spec["runtimeRoot"])
current = runtime_root / "current"
os.environ["HOME"] = "/var/lib/postgresql"
os.environ["LD_LIBRARY_PATH"] = str(current / "usr/lib/x86_64-linux-gnu") + ":" + str(current / "usr/lib/postgresql" / postgres_major / "lib")
postgres = current / "usr/lib/postgresql" / postgres_major / "bin/postgres"
argv = [
    str(postgres),
    "-D", spec["dataDir"],
    "-c", "config_file=" + spec["configDir"] + "/postgresql.conf",
]
os.execv(str(postgres), argv)
PY
        ]
      }

      resources {
        cpu    = 400
        memory = 1024
      }

      service {
        name         = "postgresql"
        port         = "postgres"
        provider     = "nomad"
        address_mode = "auto"

        check {
          type     = "tcp"
          port     = "postgres"
          interval = "10s"
          timeout  = "2s"
        }
      }
    }

    task "reconcile" {
      driver = "raw_exec"
      user   = "postgres"

      lifecycle {
        hook    = "poststart"
        sidecar = true
      }

      config {
        command = "/usr/bin/python3"
        args = ["-c", <<-PY
import json
import os
import pathlib
import re
import subprocess
import time

resource_name = "${var.postgresql_resource_name}"
doc_path = pathlib.Path("/run/verself/recovery/postgresql/document.json")
postgres_major = "16"
identifier_pattern = re.compile(r"^[A-Za-z_][A-Za-z0-9_]*$")

def load_spec():
    doc = json.loads(doc_path.read_text(encoding="utf-8"))
    resources = [
        resource for resource in doc.get("resources", [])
        if resource.get("apiVersion") == "postgresql.guardianintelligence.org/v1alpha1"
        and resource.get("kind") == "PostgreSQLCluster"
        and (resource.get("metadata") or {}).get("name") == resource_name
    ]
    if len(resources) != 1:
        raise RuntimeError(f"expected exactly one PostgreSQLCluster resource named {resource_name!r}, found {len(resources)}")
    return resources[0].get("spec") or {}

def check_identifier(value, field):
    if not isinstance(value, str) or not identifier_pattern.match(value):
        raise RuntimeError(f"{field} must be a PostgreSQL identifier")
    return value

def quote_ident(value):
    check_identifier(value, "identifier")
    return '"' + value.replace('"', '""') + '"'

def command_env(spec):
    runtime_root = pathlib.Path(spec["runtimeRoot"])
    current = runtime_root / "current"
    return {
        **os.environ,
        "HOME": "/var/lib/postgresql",
        "LD_LIBRARY_PATH": str(current / "usr/lib/x86_64-linux-gnu") + ":" + str(current / "usr/lib/postgresql" / postgres_major / "lib"),
    }

def psql(spec, *args, input_text=None):
    runtime_root = pathlib.Path(spec["runtimeRoot"])
    command = [
        str(runtime_root / "current/usr/lib/postgresql" / postgres_major / "bin/psql"),
        "-h", spec["socketDir"],
        "-p", str(spec["port"]),
        "-d", "postgres",
        "-v", "ON_ERROR_STOP=1",
    ]
    command.extend(args)
    return subprocess.run(command, check=True, text=True, input=input_text, capture_output=True, env=command_env(spec))

def wait_for_ready(spec):
    deadline = time.monotonic() + 60
    last_error = None
    while time.monotonic() < deadline:
        try:
            psql(spec, "-c", "select 1;")
            return
        except subprocess.CalledProcessError as exc:
            last_error = (exc.stderr or exc.stdout or str(exc)).strip()
            time.sleep(1)
    raise RuntimeError("PostgreSQL did not become ready: " + str(last_error))

def role_exists(spec, name):
    result = psql(spec, "-A", "-t", "-c", "select 1 from pg_roles where rolname = " + "'" + name.replace("'", "''") + "';")
    return result.stdout.strip() == "1"

def database_exists(spec, name):
    result = psql(spec, "-A", "-t", "-c", "select 1 from pg_database where datname = " + "'" + name.replace("'", "''") + "';")
    return result.stdout.strip() == "1"

def reconcile_role(spec, role):
    name = check_identifier(role["name"], "PostgreSQLCluster.spec.roles[].name")
    login = bool(role.get("login", True))
    if not role_exists(spec, name):
        psql(spec, "-c", "create role " + quote_ident(name) + (" login;" if login else " nologin;"))
    else:
        psql(spec, "-c", "alter role " + quote_ident(name) + (" login;" if login else " nologin;"))
    for parent in role.get("memberOf") or []:
        parent = check_identifier(parent, "PostgreSQLCluster.spec.roles[].memberOf[]")
        psql(spec, "-c", "grant " + quote_ident(parent) + " to " + quote_ident(name) + ";")
    return name

def reconcile_once():
    spec = load_spec()
    report_path = pathlib.Path(spec["reportPath"])
    wait_for_ready(spec)
    psql(spec, "-c", "select pg_reload_conf();")
    roles = {check_identifier(database["owner"], "PostgreSQLCluster.spec.databases[].owner") for database in spec.get("databases") or []}
    roles.add("postgres")
    for mapping in spec.get("peerMappings") or []:
        roles.add(check_identifier(mapping["postgresUser"], "PostgreSQLCluster.spec.peerMappings[].postgresUser"))
    for role in spec.get("roles") or []:
        roles.add(reconcile_role(spec, role))
    for role in sorted(roles):
        if not role_exists(spec, role):
            psql(spec, "-c", "create role " + quote_ident(role) + " login;")
    for database in spec.get("databases") or []:
        name = check_identifier(database["name"], "PostgreSQLCluster.spec.databases[].name")
        owner = check_identifier(database["owner"], "PostgreSQLCluster.spec.databases[].owner")
        if not database_exists(spec, name):
            psql(spec, "-c", "create database " + quote_ident(name) + " owner " + quote_ident(owner) + ";")
    report = {
        "component": "postgresql",
        "resource": resource_name,
        "status": "healthy",
        "socket_dir": spec["socketDir"],
        "port": spec["port"],
        "databases": sorted(database["name"] for database in spec.get("databases") or []),
        "roles": sorted(roles),
        "checked_at_unix": int(time.time()),
    }
    tmp = report_path.with_name(report_path.name + "." + str(os.getpid()) + ".tmp")
    tmp.write_text(json.dumps(report, sort_keys=True) + "\n", encoding="utf-8")
    os.chmod(tmp, 0o640)
    os.replace(tmp, report_path)

while True:
    reconcile_once()
    time.sleep(30)
PY
        ]
      }

      resources {
        cpu    = 50
        memory = 128
      }
    }
  }
}
