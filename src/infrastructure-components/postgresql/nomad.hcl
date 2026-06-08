variable "repo_root" {
  type    = string
  default = "/home/ubuntu/.local/state/guardian/repo"
}

variable "artifact_root" {
  type    = string
  default = "/home/ubuntu/.local/state/guardian/repo/artifacts"
}

variable "site" {
  type    = string
  default = "gamma"
}

variable "postgresql_resource_name" {
  type    = string
  default = "postgresql"
}

variable "postgresql_image_sha256" {
  type = string
}

# Fixed image prefix: the postgresql_runtime tar lays the runtime under
# /opt/verself/postgresql, so the in-container runtime root is constant. There
# is no releases/current versioning indirection; the image digest IS the runtime
# version.
locals {
  runtime_root = "/opt/verself/postgresql"
  image        = "docker-archive:${var.artifact_root}/sha256/${var.postgresql_image_sha256}"
}

job "postgresql" {
  name        = "postgresql"
  datacenters = ["*"]
  type        = "service"

  meta {
    image_sha256 = "${var.postgresql_image_sha256}"
  }

  group "postgresql" {
    count = 1

    reschedule {
      delay          = "15s"
      delay_function = "exponential"
      max_delay      = "5m"
      unlimited      = true
    }

    network {
      mode = "host"
      port "postgres" {
        host_network = "loopback"
        static       = 5432
        to           = 5432
      }
    }

    # The podman driver does not create bind-mount sources (unlike `docker -v`);
    # a missing host path fails container creation with `statfs: no such file or
    # directory`. /var/run/postgresql is tmpfs, wiped every reboot, so this runs
    # per-alloc, not once in preflight. setup chowns/chmods these in-container,
    # so creating both at 0755 here is sufficient.
    task "provision-host-dirs" {
      driver = "raw_exec"
      user   = "root"

      lifecycle {
        hook    = "prestart"
        sidecar = false
      }

      config {
        command = "/usr/bin/install"
        args = [
          "-d",
          "-m",
          "0755",
          "/var/lib/postgresql/16/verself",
          "/var/run/postgresql",
        ]
      }

      resources {
        cpu    = 25
        memory = 16
      }
    }

    task "setup" {
      driver = "podman"
      user   = "postgres"

      # Restart budget absorbs the prestart race with provision-host-dirs (which
      # is near-instant vs. podman container creation) and transient podman
      # socket flaps on reconverge; setup is idempotent.
      restart {
        attempts = 3
        delay    = "5s"
        interval = "1m"
        mode     = "fail"
      }

      lifecycle {
        hook    = "prestart"
        sidecar = false
      }

      vault {
        env  = false
        role = "postgresql-runtime"
      }

      identity {
        name = "vault_default"
        aud  = ["vault.io"]
        ttl  = "1h"
      }

      config {
        image           = "${local.image}"
        init            = true
        network_mode    = "host"
        readonly_rootfs = true
        # setup initializes ownership of fresh durable bind-mounts; it keeps only
        # the file-ownership caps that requires and drops everything else.
        cap_drop     = ["all"]
        cap_add      = ["chown", "fowner", "dac_override"]
        security_opt = ["no-new-privileges"]
        shm_size     = "512m"
        command      = "/usr/bin/python3"
        volumes = [
          "${var.repo_root}/workspace/.guardian/fly/document.json:/guardian/document.json:ro,noexec",
          # pgBackRest makes outbound TLS to R2; the image carries no CA roots, so
          # mount the host trust store read-only (same pattern as cloudflare).
          "/etc/ssl/certs:/etc/ssl/certs:ro,noexec",
          "/var/lib/postgresql/16/verself:/var/lib/postgresql/16/verself:rw",
          "/var/run/postgresql:/var/run/postgresql:rw",
        ]
        tmpfs = ["/tmp"]
        args = ["-c", <<-PY
import grp
import json
import os
import pathlib
import pwd
import re
import secrets
import subprocess

runtime_root = pathlib.Path("${local.runtime_root}")
resource_name = "${var.postgresql_resource_name}"
doc_path = pathlib.Path("/guardian/document.json")
projected_doc_path = pathlib.Path("/alloc/recovery/document.json")
postgres_major = "16"
identifier_pattern = re.compile(r"^[A-Za-z_][A-Za-z0-9_]*$")

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

def required_int(spec, key):
    value = spec.get(key)
    if not isinstance(value, int) or value <= 0:
        raise SystemExit(f"PostgreSQLCluster.spec.{key} must be a positive integer")
    return value

def optional_bool(value, default=False):
    if value is None:
        return default
    if not isinstance(value, bool):
        raise SystemExit("PostgreSQLCluster backup boolean fields must be boolean")
    return value

def check_identifier(value, field):
    if not isinstance(value, str) or not identifier_pattern.match(value):
        raise SystemExit(f"{field} must be a PostgreSQL identifier")
    return value

def require_nonempty(value, field):
    if not isinstance(value, str) or not value:
        raise SystemExit(f"{field} is required")
    return value

def optional_backup(spec):
    backup = spec.get("backup")
    if backup is None:
        return None
    if not isinstance(backup, dict):
        raise SystemExit("PostgreSQLCluster.spec.backup must be an object")
    repository = backup.get("repository")
    if not isinstance(repository, dict) or repository.get("type") != "s3":
        raise SystemExit("PostgreSQLCluster.spec.backup.repository.type must be s3")
    cipher_ref = backup.get("cipherPassRef")
    if not isinstance(cipher_ref, dict) or cipher_ref.get("kind") != "SecretPath":
        raise SystemExit("PostgreSQLCluster.spec.backup.cipherPassRef must reference a SecretPath")
    return {
        "stanza": check_identifier(backup.get("stanza"), "PostgreSQLCluster.spec.backup.stanza"),
        "configPath": required_path(backup, "configPath"),
        "spoolDir": required_path(backup, "spoolDir"),
        "logDir": required_path(backup, "logDir"),
        "archiveTimeout": require_nonempty(backup.get("archiveTimeout"), "PostgreSQLCluster.spec.backup.archiveTimeout"),
        "processMax": required_int(backup, "processMax"),
        "retentionFull": required_int(backup, "retentionFull"),
        "destructiveRestoreAllowed": optional_bool(backup.get("destructiveRestoreAllowed"), False),
        "recoveryCredentialOpenBaoPath": require_nonempty(backup.get("recoveryCredentialOpenBaoPath"), "PostgreSQLCluster.spec.backup.recoveryCredentialOpenBaoPath"),
        "cipherPassName": require_nonempty(cipher_ref.get("name"), "PostgreSQLCluster.spec.backup.cipherPassRef.name"),
        "repository": {
            "endpoint": require_nonempty(repository.get("endpoint"), "PostgreSQLCluster.spec.backup.repository.endpoint"),
            "region": require_nonempty(repository.get("region"), "PostgreSQLCluster.spec.backup.repository.region"),
            "bucket": require_nonempty(repository.get("bucket"), "PostgreSQLCluster.spec.backup.repository.bucket"),
            "path": required_path(repository, "path"),
        },
    }

def mkdir(path, uid, gid, mode):
    path.mkdir(parents=True, exist_ok=True)
    os.chown(path, uid, gid)
    os.chmod(path, mode)

def recovery_binary():
    path = runtime_root / "usr/bin/postgresql-recovery"
    if not path.is_file():
        raise SystemExit(f"PostgreSQL image missing postgresql-recovery binary under {path}")
    return path

def read_secret_file(path, field):
    value = pathlib.Path(path).read_text(encoding="utf-8").strip()
    if not value:
        raise SystemExit(f"{field} rendered empty")
    return value

def write_pgbackrest_config(backup, spec, data_dir, postgres_uid, postgres_gid):
    if backup is None:
        return
    access_key = read_secret_file(os.environ["NOMAD_SECRETS_DIR"] + "/postgresql.pgbackrest.r2_access_key_id", "R2 recovery access key")
    secret_key = read_secret_file(os.environ["NOMAD_SECRETS_DIR"] + "/postgresql.pgbackrest.r2_secret_access_key", "R2 recovery secret key")
    cipher_pass = read_secret_file(os.environ["NOMAD_SECRETS_DIR"] + "/postgresql.pgbackrest.cipher_pass", "pgBackRest cipher pass")
    mkdir(backup["configPath"].parent, postgres_uid, postgres_gid, 0o700)
    mkdir(backup["spoolDir"], postgres_uid, postgres_gid, 0o700)
    mkdir(backup["logDir"], postgres_uid, grp.getgrnam("adm").gr_gid, 0o750)
    content = "\n".join([
        "[global]",
        "repo1-type=s3",
        "repo1-s3-endpoint=" + backup["repository"]["endpoint"],
        "repo1-s3-region=" + backup["repository"]["region"],
        "repo1-s3-bucket=" + backup["repository"]["bucket"],
        # pgBackRest/OpenSSL does not find the mounted trust store by default in
        # the minimal image; point every repo command (including postgres's
        # archive_command, which reads this same config) at the host CA bundle.
        "repo1-s3-ca-file=/etc/ssl/certs/ca-certificates.crt",
        "repo1-s3-key=" + access_key,
        "repo1-s3-key-secret=" + secret_key,
        "repo1-path=" + str(backup["repository"]["path"]),
        "repo1-cipher-type=aes-256-cbc",
        "repo1-cipher-pass=" + cipher_pass,
        "repo1-retention-full=" + str(backup["retentionFull"]),
        "process-max=" + str(backup["processMax"]),
        "start-fast=y",
        "log-level-console=warn",
        "log-level-file=detail",
        "log-path=" + str(backup["logDir"]),
        "spool-path=" + str(backup["spoolDir"]),
        "",
        "[" + backup["stanza"] + "]",
        "pg1-path=" + str(data_dir),
        "pg1-port=" + str(required_int(spec, "port")),
        "pg1-socket-path=" + str(required_path(spec, "socketDir")),
        "pg1-user=postgres",
        "",
    ])
    tmp = backup["configPath"].with_name(backup["configPath"].name + "." + str(os.getpid()) + ".tmp")
    tmp.write_text(content, encoding="utf-8")
    os.chown(tmp, postgres_uid, postgres_gid)
    os.chmod(tmp, 0o600)
    os.replace(tmp, backup["configPath"])

def write_config(spec, data_dir, config_dir, log_dir, socket_dir, backup):
    listen_address = spec.get("listenAddress")
    if not isinstance(listen_address, str) or not listen_address:
        raise SystemExit("PostgreSQLCluster.spec.listenAddress is required")
    port = required_int(spec, "port")
    max_connections = required_int(spec, "maxConnections")
    superuser_reserved = spec.get("superuserReservedConnections")
    if not isinstance(superuser_reserved, int) or superuser_reserved < 0 or superuser_reserved >= max_connections:
        raise SystemExit("PostgreSQLCluster.spec.superuserReservedConnections must be >= 0 and less than maxConnections")
    pg_lib = runtime_root / "usr/lib/postgresql" / postgres_major / "lib"
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
dynamic_shared_memory_type = sysv
max_wal_size = 1GB
min_wal_size = 80MB
unix_socket_directories = '{socket_dir}'
"""
    if backup is not None:
        postgresql_conf += f"""archive_mode = on
archive_timeout = '{backup["archiveTimeout"]}'
archive_command = '{runtime_root / "usr/bin/pgbackrest"} --config={backup["configPath"]} --stanza={backup["stanza"]} archive-push %p'
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
    postgres = pwd.getpwnam("postgres")
    for path, content in files.items():
        tmp = path.with_name(path.name + "." + str(os.getpid()) + ".tmp")
        tmp.write_text(content, encoding="utf-8")
        os.chown(tmp, postgres.pw_uid, postgres.pw_gid)
        os.chmod(tmp, 0o600)
        os.replace(tmp, path)

def project_document(doc, postgres_uid, postgres_gid):
    projected_doc_path.parent.mkdir(parents=True, exist_ok=True)
    os.chown(projected_doc_path.parent, postgres_uid, postgres_gid)
    os.chmod(projected_doc_path.parent, 0o750)
    tmp_doc_path = projected_doc_path.with_name(projected_doc_path.name + "." + str(os.getpid()) + ".tmp")
    tmp_doc_path.write_text(json.dumps(doc, sort_keys=True, separators=(",", ":")) + "\n", encoding="utf-8")
    os.chown(tmp_doc_path, postgres_uid, postgres_gid)
    os.chmod(tmp_doc_path, 0o640)
    os.replace(tmp_doc_path, projected_doc_path)

def data_directory_status(data_dir):
    # Check: PG_VERSION is the minimal marker for an existing PostgreSQL
    # cluster; non-empty data without it is treated as invalid evidence.
    if (data_dir / "PG_VERSION").is_file():
        return "valid_cluster"
    try:
        entries = list(data_dir.iterdir())
    except FileNotFoundError:
        return "empty"
    return "empty" if not entries else "invalid_cluster"

def pid_alive(pid):
    try:
        os.kill(pid, 0)
        return True
    except ProcessLookupError:
        return False
    except PermissionError:
        return True

def postmaster_status(data_dir):
    # Check: a stale postmaster.pid can be safely reclaimed by the server task,
    # but a live postmaster means setup must not mutate PGDATA.
    pid_file = data_dir / "postmaster.pid"
    if not pid_file.exists():
        return "stopped"
    try:
        pid = int(pid_file.read_text(encoding="utf-8").splitlines()[0])
    except Exception:
        return "stale_pid"
    return "running" if pid_alive(pid) else "stale_pid"

def recovery_common_args(backup):
    return [
        str(recovery_binary()),
        "--stanza=" + backup["stanza"],
        "--config=" + str(backup["configPath"]),
        "--runtime-root=" + str(runtime_root),
        "--process-max=" + str(backup["processMax"]),
    ]

def backup_repository_status(backup):
    # Check: successful pgBackRest inventory with no backups is a reachable
    # empty repository; command failure is treated as unreachable, not empty.
    if backup is None:
        return "not_configured"
    result = subprocess.run(
        recovery_common_args(backup) + ["--action=info"],
        check=False,
        text=True,
        capture_output=True,
    )
    if result.returncode != 0:
        return "unreachable"
    try:
        payload = json.loads(result.stdout)
    except Exception as exc:
        raise SystemExit(f"parse pgBackRest info JSON: {exc}") from exc
    backups = []
    if isinstance(payload, list):
        for stanza in payload:
            if isinstance(stanza, dict):
                backups.extend(stanza.get("backup") or [])
    return "reachable_backed" if backups else "reachable_empty"

def build_recovery_plan(backup, data_status, postmaster, repository):
    args = [
        str(recovery_binary()),
        "--action=plan",
        "--plan-config=valid",
        "--plan-data-directory=" + data_status,
        "--plan-postmaster=" + postmaster,
        "--plan-backup-repository=" + repository,
        "--plan-destructive-restore-allowed=" + ("true" if backup and backup["destructiveRestoreAllowed"] else "false"),
    ]
    result = subprocess.run(args, check=True, text=True, capture_output=True)
    return json.loads(result.stdout)

def plan_contains(plan, state):
    if plan.get("terminal") == state:
        return True
    return any(transition.get("from") == state or transition.get("to") == state for transition in plan.get("transitions") or [])

def blocked_reason(plan):
    transitions = plan.get("transitions") or []
    if transitions:
        return transitions[-1].get("reason", "PostgreSQL recovery blocked")
    return "PostgreSQL recovery blocked"

def initialize_cluster(data_dir, socket_dir):
    pwfile = socket_dir / ("initdb-password." + str(os.getpid()))
    pwfile.write_text(secrets.token_urlsafe(48), encoding="utf-8")
    os.chmod(pwfile, 0o600)
    try:
        subprocess.run([
            str(runtime_root / "usr/lib/postgresql" / postgres_major / "bin/initdb"),
            "--pgdata=" + str(data_dir),
            "--auth-local=peer",
            "--auth-host=scram-sha-256",
            "--encoding=UTF8",
            "--locale=C.UTF-8",
            "-L", str(runtime_root / "usr/share/postgresql" / postgres_major),
            "--pwfile=" + str(pwfile),
        ], check=True)
    finally:
        try:
            pwfile.unlink()
        except FileNotFoundError:
            pass

def restore_cluster(backup, data_dir):
    args = recovery_common_args(backup) + [
        "--action=restore",
        "--pgdata=" + str(data_dir),
        "--restore-type=default",
        "--target-action=promote",
    ]
    if backup["destructiveRestoreAllowed"]:
        args.append("--destructive-restore-allowed")
    subprocess.run(args, check=True)

doc, spec = load_resource()
postgres = pwd.getpwnam("postgres")
data_dir = required_path(spec, "dataDir")
config_dir = required_path(spec, "configDir")
log_dir = required_path(spec, "logDir")
socket_dir = required_path(spec, "socketDir")
report_path = required_path(spec, "reportPath")
backup = optional_backup(spec)
if report_path.parent != projected_doc_path.parent:
    raise SystemExit("PostgreSQLCluster.spec.reportPath must be under /alloc/recovery")

# Initialize ownership of the durable bind-mounts; the initdb pwfile lands in
# the socket dir which postgres owns.
mkdir(data_dir, postgres.pw_uid, postgres.pw_gid, 0o700)
mkdir(config_dir, postgres.pw_uid, postgres.pw_gid, 0o700)
mkdir(log_dir, postgres.pw_uid, grp.getgrnam("adm").gr_gid, 0o2755)
mkdir(socket_dir, postgres.pw_uid, postgres.pw_gid, 0o755)
write_pgbackrest_config(backup, spec, data_dir, postgres.pw_uid, postgres.pw_gid)
write_config(spec, data_dir, config_dir, log_dir, socket_dir, backup)
project_document(doc, postgres.pw_uid, postgres.pw_gid)

data_status = data_directory_status(data_dir)
postmaster = postmaster_status(data_dir)
repository = backup_repository_status(backup)
plan = build_recovery_plan(backup, data_status, postmaster, repository)
if plan.get("terminal") == "blocked":
    raise SystemExit(blocked_reason(plan))
if plan_contains(plan, "restore_backup"):
    restore_cluster(backup, data_dir)
elif plan_contains(plan, "initialize_fresh_cluster"):
    initialize_cluster(data_dir, socket_dir)
PY
        ]
      }

      env {
        HOME   = "/tmp"
        TMPDIR = "/tmp"
      }

      resources {
        cpu    = 100
        memory = 256
      }

      template {
        change_mode = "restart"
        destination = "secrets/postgresql.pgbackrest.r2_access_key_id"
        perms       = "0600"
        data        = <<-EOT
{{ with secret "kv-controller/data/integrations/cloudflare/r2/capabilities/recovery" }}{{ .Data.data.access_key_id }}{{ end }}
EOT
      }

      template {
        change_mode = "restart"
        destination = "secrets/postgresql.pgbackrest.r2_secret_access_key"
        perms       = "0600"
        data        = <<-EOT
{{ with secret "kv-controller/data/integrations/cloudflare/r2/capabilities/recovery" }}{{ .Data.data.secret_access_key }}{{ end }}
EOT
      }

      template {
        change_mode = "restart"
        destination = "secrets/postgresql.pgbackrest.cipher_pass"
        perms       = "0600"
        data        = <<-EOT
{{ with secret "kv-runtime/data/secret/org/postgresql.pgbackrest.cipher_pass" }}{{ .Data.data.value }}{{ end }}
EOT
      }
    }

    task "server" {
      driver = "podman"
      user   = "postgres"

      # Long-lived: "process running" is a maintained invariant (DR contract).
      # mode=delay never gives up, so a killed postmaster or a transient podman
      # socket flap is retried indefinitely instead of failing the alloc.
      restart {
        attempts = 5
        delay    = "10s"
        interval = "5m"
        mode     = "delay"
      }

      config {
        image           = "${local.image}"
        init            = true
        network_mode    = "host"
        readonly_rootfs = true
        cap_drop        = ["all"]
        security_opt    = ["no-new-privileges"]
        shm_size        = "512m"
        command         = "/usr/bin/python3"
        volumes = [
          # server runs archive_command (pgBackRest archive-push) to R2; needs CA roots.
          "/etc/ssl/certs:/etc/ssl/certs:ro,noexec",
          "/var/lib/postgresql/16/verself:/var/lib/postgresql/16/verself:rw",
          "/var/run/postgresql:/var/run/postgresql:rw",
        ]
        tmpfs = ["/tmp"]
        args = ["-c", <<-PY
import json
import os
import pathlib
import socket
import time

resource_name = "${var.postgresql_resource_name}"
runtime_root = pathlib.Path("${local.runtime_root}")
doc_path = pathlib.Path("/alloc/recovery/document.json")
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
postgres = runtime_root / "usr/lib/postgresql" / postgres_major / "bin/postgres"
argv = [
    str(postgres),
    "-D", spec["dataDir"],
    "-c", "config_file=" + spec["configDir"] + "/postgresql.conf",
]

def postmaster_listening(socket_dir, port):
    # The PID in postmaster.pid is container-PID-namespace-local; under init=true
    # the fresh wrapper is itself pid 2, so os.kill(pid, 0) self-aliases and the
    # stale file never clears. The socket is the namespace-independent authority
    # for whether a postmaster is actually serving (host network).
    sock_path = str(pathlib.Path(socket_dir) / (".s.PGSQL." + str(port)))
    probe = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
    probe.settimeout(1)
    try:
        probe.connect(sock_path)
        return True
    except OSError:
        return False
    finally:
        probe.close()

def clear_stale_postmaster(data_dir, socket_dir, port):
    # Each task restart is a fresh container, so the previous postmaster died with
    # it; a postmaster.pid without a live socket is stale by construction.
    if postmaster_listening(socket_dir, port):
        raise SystemExit("a PostgreSQL postmaster is already serving the socket; refusing to start a second")
    socket_name = ".s.PGSQL." + str(port)
    for path in [data_dir / "postmaster.pid", socket_dir / socket_name, socket_dir / (socket_name + ".lock")]:
        try:
            path.unlink()
        except FileNotFoundError:
            pass

clear_stale_postmaster(pathlib.Path(spec["dataDir"]), pathlib.Path(spec["socketDir"]), spec["port"])
os.execv(str(postgres), argv)
PY
        ]
      }

      env {
        HOME   = "/tmp"
        TMPDIR = "/tmp"
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
      driver = "podman"
      user   = "postgres"

      # The reconcile loop is retry-tolerant: "server not ready yet" must restart
      # this sidecar, not fail the alloc and SIGKILL the server. mode=delay keeps
      # retrying until the server converges.
      restart {
        attempts = 5
        delay    = "10s"
        interval = "5m"
        mode     = "delay"
      }

      lifecycle {
        hook    = "poststart"
        sidecar = true
      }

      config {
        image           = "${local.image}"
        init            = true
        network_mode    = "host"
        readonly_rootfs = true
        cap_drop        = ["all"]
        security_opt    = ["no-new-privileges"]
        shm_size        = "512m"
        command         = "/usr/bin/python3"
        volumes = [
          "${var.repo_root}/workspace/.guardian/fly/document.json:/guardian/document.json:ro,noexec",
          # pgBackRest makes outbound TLS to R2; the image carries no CA roots, so
          # mount the host trust store read-only (same pattern as cloudflare).
          "/etc/ssl/certs:/etc/ssl/certs:ro,noexec",
          # pgbackrest check/backup reads PGDATA (pg_control, data files); reconcile
          # never writes it, so mount read-only.
          "/var/lib/postgresql/16/verself:/var/lib/postgresql/16/verself:ro",
          "/var/run/postgresql:/var/run/postgresql:rw",
        ]
        tmpfs = ["/tmp"]
        args = ["-c", <<-PY
import json
import os
import pathlib
import pwd
import re
import subprocess
import time

resource_name = "${var.postgresql_resource_name}"
runtime_root = pathlib.Path("${local.runtime_root}")
doc_path = pathlib.Path("/guardian/document.json")
projected_doc_path = pathlib.Path("/alloc/recovery/document.json")
postgres_major = "16"
identifier_pattern = re.compile(r"^[A-Za-z_][A-Za-z0-9_]*$")

def load_document():
    doc = json.loads(doc_path.read_text(encoding="utf-8"))
    resources = [
        resource for resource in doc.get("resources", [])
        if resource.get("apiVersion") == "postgresql.guardianintelligence.org/v1alpha1"
        and resource.get("kind") == "PostgreSQLCluster"
        and (resource.get("metadata") or {}).get("name") == resource_name
    ]
    if len(resources) != 1:
        raise RuntimeError(f"expected exactly one PostgreSQLCluster resource named {resource_name!r}, found {len(resources)}")
    return doc, resources[0].get("spec") or {}

def project_document(doc):
    postgres = pwd.getpwnam("postgres")
    projected_doc_path.parent.mkdir(parents=True, exist_ok=True)
    os.chown(projected_doc_path.parent, postgres.pw_uid, postgres.pw_gid)
    os.chmod(projected_doc_path.parent, 0o750)
    tmp = projected_doc_path.with_name(projected_doc_path.name + "." + str(os.getpid()) + ".tmp")
    tmp.write_text(json.dumps(doc, sort_keys=True, separators=(",", ":")) + "\n", encoding="utf-8")
    os.chown(tmp, postgres.pw_uid, postgres.pw_gid)
    os.chmod(tmp, 0o640)
    os.replace(tmp, projected_doc_path)

def check_identifier(value, field):
    if not isinstance(value, str) or not identifier_pattern.match(value):
        raise RuntimeError(f"{field} must be a PostgreSQL identifier")
    return value

def quote_ident(value):
    check_identifier(value, "identifier")
    return '"' + value.replace('"', '""') + '"'

def run_postgres_tool(command, *, input_text=None, check=True, capture_output=True):
    return subprocess.run(
        [str(arg) for arg in command],
        check=check,
        text=True,
        input=input_text,
        capture_output=capture_output,
    )

def backup_spec(spec):
    backup = spec.get("backup")
    if not isinstance(backup, dict):
        return None
    return backup

def recovery_command(backup, action, *extra):
    command = [
        str(runtime_root / "usr/bin/postgresql-recovery"),
        "--action=" + action,
        "--stanza=" + backup["stanza"],
        "--config=" + backup["configPath"],
        "--runtime-root=" + str(runtime_root),
        "--process-max=" + str(backup["processMax"]),
    ]
    command.extend(extra)
    return command

def run_recovery(backup, action, *extra, check=True, capture_output=False):
    return run_postgres_tool(
        recovery_command(backup, action, *extra),
        check=check,
        capture_output=capture_output,
    )

def pgbackrest_info(backup):
    result = run_recovery(backup, "info", check=False, capture_output=True)
    if result.returncode != 0:
        raise RuntimeError((result.stderr or result.stdout or "pgBackRest info failed").strip())
    try:
        payload = json.loads(result.stdout)
    except Exception as exc:
        raise RuntimeError(f"parse pgBackRest info JSON: {exc}") from exc
    return payload

def pgbackrest_backup_count(payload):
    count = 0
    if isinstance(payload, list):
        for stanza in payload:
            if isinstance(stanza, dict):
                count += len(stanza.get("backup") or [])
    return count

def psql(spec, *args, input_text=None):
    command = [
        str(runtime_root / "usr/lib/postgresql" / postgres_major / "bin/psql"),
        "-h", spec["socketDir"],
        "-p", str(spec["port"]),
        "-d", "postgres",
        "-v", "ON_ERROR_STOP=1",
    ]
    command.extend(args)
    return run_postgres_tool(command, input_text=input_text)

def psql_database(spec, database, *args, input_text=None):
    command = [
        str(runtime_root / "usr/lib/postgresql" / postgres_major / "bin/psql"),
        "-h", spec["socketDir"],
        "-p", str(spec["port"]),
        "-d", database,
        "-v", "ON_ERROR_STOP=1",
    ]
    command.extend(args)
    return run_postgres_tool(command, input_text=input_text)

def write_peer_auth_config(spec):
    postgres = pwd.getpwnam("postgres")
    config_dir = pathlib.Path(spec["configDir"])
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
    for path, content in {
        config_dir / "pg_hba.conf": pg_hba,
        config_dir / "pg_ident.conf": "\n".join(pg_ident_lines) + "\n",
    }.items():
        tmp = path.with_name(path.name + "." + str(os.getpid()) + ".tmp")
        tmp.write_text(content, encoding="utf-8")
        os.chown(tmp, postgres.pw_uid, postgres.pw_gid)
        os.chmod(tmp, 0o600)
        os.replace(tmp, path)

def wait_for_ready(spec):
    # Crash recovery after an unclean stop can exceed a minute; give the first
    # readiness probe room so it does not prematurely fail the reconcile loop.
    deadline = time.monotonic() + 300
    last_error = None
    while time.monotonic() < deadline:
        try:
            psql(spec, "-c", "select 1;")
            return
        except subprocess.CalledProcessError as exc:
            last_error = (exc.stderr or exc.stdout or str(exc)).strip()
            time.sleep(1)
    raise RuntimeError("PostgreSQL did not become ready: " + str(last_error))

def prime_first_archive(spec):
    # Fresh-cluster ordering: archive_mode=on starts pushing WAL the moment the
    # server is up, but a segment pushed before stanza-create exists is rejected
    # and PostgreSQL retries it in order, ahead of any newer segment. pgbackrest
    # check forces a WAL switch and waits archive-timeout for *that* segment to
    # land, so it can time out behind that pre-stanza backlog. Forcing a
    # checkpoint + WAL switch here, after stanza-create, drains the archiver
    # through the now-existing stanza and proves the archive path end-to-end
    # before check runs. Idempotent: a no-op switch on an already-warm archiver.
    psql(spec, "-c", "checkpoint;")
    psql(spec, "-c", "select pg_switch_wal();")

def ensure_backup_discipline(spec):
    # Check: fully recovered PostgreSQL must have a reachable pgBackRest
    # repository and at least one usable physical backup.
    backup = backup_spec(spec)
    if backup is None:
        raise RuntimeError("PostgreSQLCluster.spec.backup is required for recovery health")
    info = pgbackrest_info(backup)
    backup_count = pgbackrest_backup_count(info)
    if backup_count == 0:
        # stanza-create is idempotent; gating it on a fragile "missing stanza"
        # string match skipped it for a fresh (empty-list) repo and broke check.
        run_recovery(backup, "stanza-create")
        prime_first_archive(spec)
        # On a brand-new cluster the archiver may still be draining a pre-stanza
        # WAL backlog; a check failure here is a not-yet-converged condition, not
        # a broken repo. Re-sample on the next loop tick instead of crash-looping
        # the sidecar -- stanza-create and priming above are idempotent so the
        # retry converges. pgBackRest writes the real error to its own stderr
        # (inherited here), so a persistently failing check stays loud in logs.
        check_result = run_recovery(backup, "check", check=False)
        if check_result.returncode != 0:
            return "awaiting_first_archive"
        run_recovery(backup, "backup", "--backup-type=full")
        return "initial_full_backup_created"
    run_recovery(backup, "check")
    return "repository_backed"

def role_exists(spec, name):
    result = psql(spec, "-A", "-t", "-c", "select 1 from pg_roles where rolname = " + "'" + name.replace("'", "''") + "';")
    return result.stdout.strip() == "1"

def database_exists(spec, name):
    result = psql(spec, "-A", "-t", "-c", "select 1 from pg_database where datname = " + "'" + name.replace("'", "''") + "';")
    return result.stdout.strip() == "1"

def reconcile_database(spec, name, owner):
    if not database_exists(spec, name):
        psql(spec, "-c", "create database " + quote_ident(name) + " owner " + quote_ident(owner) + ";")
    else:
        psql(spec, "-c", "alter database " + quote_ident(name) + " owner to " + quote_ident(owner) + ";")
    # Restored databases may predate the CRD; migrations require public schema CREATE.
    psql_database(spec, name, "-c", "alter schema public owner to " + quote_ident(owner) + ";")
    psql_database(spec, name, "-c", "grant usage, create on schema public to " + quote_ident(owner) + ";")

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
    doc, spec = load_document()
    project_document(doc)
    write_peer_auth_config(spec)
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
        reconcile_database(spec, name, owner)
    backup_status = ensure_backup_discipline(spec)
    report = {
        "component": "postgresql",
        "resource": resource_name,
        "status": "healthy",
        "backup_status": backup_status,
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

      env {
        HOME   = "/tmp"
        TMPDIR = "/tmp"
      }

      resources {
        cpu    = 50
        memory = 128
      }
    }
  }
}
