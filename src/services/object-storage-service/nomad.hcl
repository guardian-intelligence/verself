variable "guardian_repo_root" {
  type    = string
  default = "/home/ubuntu/.local/state/guardian/repo/current"
}

variable "object_storage_resource_name" {
  type    = string
  default = "object-storage"
}

variable "object_storage_runtime_root" {
  type    = string
  default = "/var/lib/object-storage-service/runtime"
}

job "object-storage-service" {
  name        = "object-storage-service"
  datacenters = ["*"]
  type        = "service"

  group "object-storage-service" {
    count = 2

    network {
      mode = "host"
      port "public_http" {
        host_network = "loopback"
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
import shutil
import subprocess
import tarfile

repo_root = pathlib.Path("${var.guardian_repo_root}")
runtime_root = pathlib.Path("${var.object_storage_runtime_root}")
resource_name = "${var.object_storage_resource_name}"
artifact = repo_root / "bazel-bin/src/services/object-storage-service/cmd/object-storage-service/object-storage-service.tar"
doc_path = repo_root / "workspace/.guardian/fly/document.json"
projected_doc_path = pathlib.Path("/run/verself/recovery/object-storage/document.json")

def run(args):
    subprocess.run(args, check=True)

def ensure_group(name):
    try:
        grp.getgrnam(name)
    except KeyError:
        run(["/usr/sbin/groupadd", "--system", name])

def ensure_user(name, uid, group):
    try:
        pwd.getpwnam(name)
        return
    except KeyError:
        pass
    run([
        "/usr/sbin/useradd",
        "--system",
        "--uid", str(uid),
        "--gid", group,
        "--home-dir", "/var/lib/object-storage-service",
        "--shell", "/usr/sbin/nologin",
        "--no-create-home",
        name,
    ])

def mkdir(path, uid, gid, mode):
    pathlib.Path(path).mkdir(parents=True, exist_ok=True)
    os.chown(path, uid, gid)
    os.chmod(path, mode)

ensure_group("object_storage_service")
ensure_user("object_storage_service", 960, "object_storage_service")
ensure_user("object_storage_admin", 961, "object_storage_service")

service = pwd.getpwnam("object_storage_service")
mkdir("/var/lib/object-storage-service", service.pw_uid, service.pw_gid, 0o750)
mkdir(runtime_root, 0, service.pw_gid, 0o755)
mkdir(runtime_root / "releases", 0, service.pw_gid, 0o755)
mkdir(runtime_root / "tmp", 0, service.pw_gid, 0o755)

if not artifact.is_file():
    raise SystemExit(f"missing object-storage-service runtime artifact: {artifact}")

try:
    guardian_doc = json.loads(doc_path.read_text(encoding="utf-8"))
except Exception as exc:
    raise SystemExit(f"load Guardian resource graph {doc_path}: {exc}") from exc

matches = [
    resource for resource in guardian_doc.get("resources", [])
    if resource.get("apiVersion") == "objectstorage.guardianintelligence.org/v1alpha1"
    and resource.get("kind") == "ObjectStorageService"
    and (resource.get("metadata") or {}).get("name") == resource_name
]
if len(matches) != 1:
    raise SystemExit(f"expected exactly one ObjectStorageService resource named {resource_name!r}, found {len(matches)}")

pathlib.Path("/run/verself").mkdir(parents=True, exist_ok=True)
os.chown("/run/verself", 0, 0)
os.chmod("/run/verself", 0o755)
pathlib.Path("/run/verself/recovery").mkdir(parents=True, exist_ok=True)
os.chown("/run/verself/recovery", 0, 0)
os.chmod("/run/verself/recovery", 0o711)
projected_doc_path.parent.mkdir(parents=True, exist_ok=True)
os.chown(projected_doc_path.parent, 0, service.pw_gid)
os.chmod(projected_doc_path.parent, 0o750)
tmp_doc_path = projected_doc_path.with_name(projected_doc_path.name + "." + str(os.getpid()) + ".tmp")
tmp_doc_path.write_text(json.dumps(guardian_doc, sort_keys=True, separators=(",", ":")) + "\n", encoding="utf-8")
os.chown(tmp_doc_path, 0, service.pw_gid)
os.chmod(tmp_doc_path, 0o640)
os.replace(tmp_doc_path, projected_doc_path)

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
                raise SystemExit(f"unsafe runtime artifact member: {member.name}")
            if member.issym() or member.islnk():
                raise SystemExit(f"runtime artifact links are not allowed: {member.name}")
        tf.extractall(dest)

lock_path = runtime_root / "install.lock"
with lock_path.open("w") as lock:
    fcntl.flock(lock, fcntl.LOCK_EX)
    release = runtime_root / "releases" / ("sha256-" + sha256_file(artifact))
    tmp = runtime_root / "tmp" / (release.name + "." + str(os.getpid()))
    if not (release / "bin/object-storage-service").is_file():
        shutil.rmtree(tmp, ignore_errors=True)
        tmp.mkdir(parents=True, mode=0o755)
        safe_extract_tar(artifact, tmp)
        shutil.rmtree(release, ignore_errors=True)
        os.replace(tmp, release)
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

run([
    "/usr/sbin/runuser",
    "-u",
    "object_storage_service",
    "--",
    str(runtime_root / "current/bin/object-storage-service"),
    "migrate",
    "--resource-graph=/run/verself/recovery/object-storage/document.json",
    "--resource-name=${var.object_storage_resource_name}",
    "up",
])
PY
        ]
      }

      resources {
        cpu    = 100
        memory = 128
      }
    }

    task "object-storage-service" {
      driver         = "raw_exec"
      user           = "object_storage_service"
      kill_signal    = "SIGTERM"
      kill_timeout   = "30s"
      shutdown_delay = "5s"

      vault {
        env  = false
        role = "object-storage-service-runtime"
      }

      identity {
        name = "vault_default"
        aud  = ["vault.io"]
        ttl  = "1h"
      }

      config {
        command = "${var.object_storage_runtime_root}/current/bin/object-storage-service"
        args = [
          "--role=s3",
          "--resource-graph=/run/verself/recovery/object-storage/document.json",
          "--resource-name=${var.object_storage_resource_name}",
          "--listen-addr=127.0.0.1:$${NOMAD_PORT_public_http}",
        ]
      }

      env {
        CREDENTIALS_DIRECTORY        = "$${NOMAD_SECRETS_DIR}"
        OTEL_EXPORTER_OTLP_ENDPOINT = "http://127.0.0.1:4317"
        OTEL_RESOURCE_ATTRIBUTES    = "verself.supervisor=nomad"
        OTEL_SERVICE_NAME           = "object-storage-service"
        VERSELF_SUPERVISOR          = "nomad"
      }

      template {
        change_mode = "restart"
        destination = "secrets/object-storage-service.credential_kek"
        perms       = "0600"
        data        = <<-EOT
{{ with secret "kv-runtime/data/secret/org/object-storage-service.credential_kek" }}{{ .Data.data.value }}{{ end }}
EOT
      }

      template {
        change_mode = "restart"
        destination = "secrets/object-storage-service.r2.proxy_access_key_id"
        perms       = "0600"
        data        = <<-EOT
{{ with secret "kv-runtime/data/secret/org/object-storage-service.r2.proxy_access_key_id" }}{{ .Data.data.value }}{{ end }}
EOT
      }

      template {
        change_mode = "restart"
        destination = "secrets/object-storage-service.r2.proxy_secret_access_key"
        perms       = "0600"
        data        = <<-EOT
{{ with secret "kv-runtime/data/secret/org/object-storage-service.r2.proxy_secret_access_key" }}{{ .Data.data.value }}{{ end }}
EOT
      }

      resources {
        cpu    = 500
        memory = 512
      }

      restart {
        attempts = 3
        delay    = "15s"
        interval = "300s"
        mode     = "delay"
      }

      service {
        name         = "object-storage-service-public-http"
        port         = "public_http"
        provider     = "nomad"
        address_mode = "auto"
        check {
          name            = "object-storage-service-http-public_http"
          type            = "http"
          path            = "/healthz"
          protocol        = "https"
          tls_skip_verify = true
          port            = "public_http"
          interval        = "1s"
          timeout         = "3s"
        }
      }
    }

    update {
      max_parallel      = 1
      health_check      = "checks"
      min_healthy_time  = "3s"
      healthy_deadline  = "300s"
      progress_deadline = "600s"
      canary            = 1
      auto_revert       = true
      auto_promote      = true
    }
  }

  group "object-storage-admin" {
    count = 1

    network {
      mode = "host"
      port "admin_http" {
        host_network = "loopback"
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
import shutil
import subprocess
import tarfile

repo_root = pathlib.Path("${var.guardian_repo_root}")
runtime_root = pathlib.Path("${var.object_storage_runtime_root}")
resource_name = "${var.object_storage_resource_name}"
artifact = repo_root / "bazel-bin/src/services/object-storage-service/cmd/object-storage-service/object-storage-service.tar"
doc_path = repo_root / "workspace/.guardian/fly/document.json"
projected_doc_path = pathlib.Path("/run/verself/recovery/object-storage/document.json")

def run(args):
    subprocess.run(args, check=True)

def ensure_group(name):
    try:
        grp.getgrnam(name)
    except KeyError:
        run(["/usr/sbin/groupadd", "--system", name])

def ensure_user(name, uid, group):
    try:
        pwd.getpwnam(name)
        return
    except KeyError:
        pass
    run([
        "/usr/sbin/useradd",
        "--system",
        "--uid", str(uid),
        "--gid", group,
        "--home-dir", "/var/lib/object-storage-service",
        "--shell", "/usr/sbin/nologin",
        "--no-create-home",
        name,
    ])

def mkdir(path, uid, gid, mode):
    pathlib.Path(path).mkdir(parents=True, exist_ok=True)
    os.chown(path, uid, gid)
    os.chmod(path, mode)

ensure_group("object_storage_service")
ensure_user("object_storage_service", 960, "object_storage_service")
ensure_user("object_storage_admin", 961, "object_storage_service")

service = pwd.getpwnam("object_storage_service")
mkdir("/var/lib/object-storage-service", service.pw_uid, service.pw_gid, 0o750)
mkdir(runtime_root, 0, service.pw_gid, 0o755)
mkdir(runtime_root / "releases", 0, service.pw_gid, 0o755)
mkdir(runtime_root / "tmp", 0, service.pw_gid, 0o755)

if not artifact.is_file():
    raise SystemExit(f"missing object-storage-service runtime artifact: {artifact}")

try:
    guardian_doc = json.loads(doc_path.read_text(encoding="utf-8"))
except Exception as exc:
    raise SystemExit(f"load Guardian resource graph {doc_path}: {exc}") from exc

matches = [
    resource for resource in guardian_doc.get("resources", [])
    if resource.get("apiVersion") == "objectstorage.guardianintelligence.org/v1alpha1"
    and resource.get("kind") == "ObjectStorageService"
    and (resource.get("metadata") or {}).get("name") == resource_name
]
if len(matches) != 1:
    raise SystemExit(f"expected exactly one ObjectStorageService resource named {resource_name!r}, found {len(matches)}")

pathlib.Path("/run/verself").mkdir(parents=True, exist_ok=True)
os.chown("/run/verself", 0, 0)
os.chmod("/run/verself", 0o755)
pathlib.Path("/run/verself/recovery").mkdir(parents=True, exist_ok=True)
os.chown("/run/verself/recovery", 0, 0)
os.chmod("/run/verself/recovery", 0o711)
projected_doc_path.parent.mkdir(parents=True, exist_ok=True)
os.chown(projected_doc_path.parent, 0, service.pw_gid)
os.chmod(projected_doc_path.parent, 0o750)
tmp_doc_path = projected_doc_path.with_name(projected_doc_path.name + "." + str(os.getpid()) + ".tmp")
tmp_doc_path.write_text(json.dumps(guardian_doc, sort_keys=True, separators=(",", ":")) + "\n", encoding="utf-8")
os.chown(tmp_doc_path, 0, service.pw_gid)
os.chmod(tmp_doc_path, 0o640)
os.replace(tmp_doc_path, projected_doc_path)

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
                raise SystemExit(f"unsafe runtime artifact member: {member.name}")
            if member.issym() or member.islnk():
                raise SystemExit(f"runtime artifact links are not allowed: {member.name}")
        tf.extractall(dest)

lock_path = runtime_root / "install.lock"
with lock_path.open("w") as lock:
    fcntl.flock(lock, fcntl.LOCK_EX)
    release = runtime_root / "releases" / ("sha256-" + sha256_file(artifact))
    tmp = runtime_root / "tmp" / (release.name + "." + str(os.getpid()))
    if not (release / "bin/object-storage-service").is_file():
        shutil.rmtree(tmp, ignore_errors=True)
        tmp.mkdir(parents=True, mode=0o755)
        safe_extract_tar(artifact, tmp)
        shutil.rmtree(release, ignore_errors=True)
        os.replace(tmp, release)
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
PY
        ]
      }

      resources {
        cpu    = 50
        memory = 64
      }
    }

    task "object-storage-admin" {
      driver         = "raw_exec"
      user           = "object_storage_admin"
      kill_signal    = "SIGTERM"
      kill_timeout   = "30s"
      shutdown_delay = "5s"

      vault {
        env  = false
        role = "object-storage-service-runtime"
      }

      identity {
        name = "vault_default"
        aud  = ["vault.io"]
        ttl  = "1h"
      }

      config {
        command = "${var.object_storage_runtime_root}/current/bin/object-storage-service"
        args = [
          "--role=admin",
          "--resource-graph=/run/verself/recovery/object-storage/document.json",
          "--resource-name=${var.object_storage_resource_name}",
          "--admin-listen-addr=127.0.0.1:$${NOMAD_PORT_admin_http}",
        ]
      }

      env {
        CREDENTIALS_DIRECTORY        = "$${NOMAD_SECRETS_DIR}"
        OTEL_EXPORTER_OTLP_ENDPOINT = "http://127.0.0.1:4317"
        OTEL_RESOURCE_ATTRIBUTES    = "verself.supervisor=nomad"
        OTEL_SERVICE_NAME           = "object-storage-admin"
        VERSELF_SUPERVISOR          = "nomad"
      }

      template {
        change_mode = "restart"
        destination = "secrets/iam-service.zitadel.auth_audience"
        perms       = "0600"
        data        = <<-EOT
{{ with secret "kv-runtime/data/secret/org/iam-service.zitadel.auth_audience" }}{{ .Data.data.value }}{{ end }}
EOT
      }

      template {
        change_mode = "restart"
        destination = "secrets/object-storage-service.credential_kek"
        perms       = "0600"
        data        = <<-EOT
{{ with secret "kv-runtime/data/secret/org/object-storage-service.credential_kek" }}{{ .Data.data.value }}{{ end }}
EOT
      }

      template {
        change_mode = "restart"
        destination = "secrets/object-storage-service.r2.admin_access_key_id"
        perms       = "0600"
        data        = <<-EOT
{{ with secret "kv-runtime/data/secret/org/object-storage-service.r2.admin_access_key_id" }}{{ .Data.data.value }}{{ end }}
EOT
      }

      template {
        change_mode = "restart"
        destination = "secrets/object-storage-service.r2.admin_secret_access_key"
        perms       = "0600"
        data        = <<-EOT
{{ with secret "kv-runtime/data/secret/org/object-storage-service.r2.admin_secret_access_key" }}{{ .Data.data.value }}{{ end }}
EOT
      }

      resources {
        cpu    = 500
        memory = 512
      }

      restart {
        attempts = 3
        delay    = "15s"
        interval = "300s"
        mode     = "delay"
      }

      service {
        name         = "object-storage-admin-internal-https"
        port         = "admin_http"
        provider     = "nomad"
        address_mode = "auto"
        check {
          name     = "object-storage-admin-tcp-admin_http"
          type     = "tcp"
          port     = "admin_http"
          interval = "1s"
          timeout  = "3s"
        }
      }
    }

    update {
      max_parallel      = 1
      health_check      = "checks"
      min_healthy_time  = "3s"
      healthy_deadline  = "300s"
      progress_deadline = "600s"
      canary            = 1
      auto_revert       = true
      auto_promote      = true
    }
  }
}
