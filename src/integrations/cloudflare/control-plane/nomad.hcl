variable "guardian_repo_root" {
  type    = string
  default = "/home/ubuntu/.local/state/guardian/repo/current"
}

job "cloudflare-integration-recovery" {
  name = "cloudflare-integration-recovery"
  datacenters = ["*"]
  type = "batch"

  group "cloudflare-integration-recovery" {
    count = 1

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
import json
import os
import pathlib
import shutil
import tarfile

runtime_root = pathlib.Path("/var/lib/cloudflare-control-plane/runtime")
artifact = pathlib.Path("${var.guardian_repo_root}") / "bazel-bin/src/integrations/cloudflare/control-plane/cloudflare-control-plane-runtime.tar"
if not artifact.is_file():
    raise SystemExit(f"missing Cloudflare control-plane runtime artifact: {artifact}")

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

runtime_root.mkdir(parents=True, exist_ok=True)
tmp = runtime_root / ("tmp." + str(os.getpid()))
shutil.rmtree(tmp, ignore_errors=True)
tmp.mkdir(parents=True, mode=0o755)
safe_extract_tar(artifact, tmp)

release = runtime_root / "current"
next_release = runtime_root / "current.next"
if next_release.exists() or next_release.is_symlink():
    if next_release.is_dir() and not next_release.is_symlink():
        shutil.rmtree(next_release)
    else:
        next_release.unlink()
os.replace(tmp, next_release)
if release.exists() or release.is_symlink():
    if release.is_dir() and not release.is_symlink():
        shutil.rmtree(release)
    else:
        release.unlink()
os.replace(next_release, release)

doc_path = pathlib.Path("${var.guardian_repo_root}") / "workspace/.guardian/fly/document.json"
try:
    guardian_doc = json.loads(doc_path.read_text(encoding="utf-8"))
except Exception as exc:
    raise SystemExit(f"load Guardian resource graph {doc_path}: {exc}") from exc

matches = [
    resource for resource in guardian_doc.get("resources", [])
    if resource.get("apiVersion") == "cloudflare.guardianintelligence.org/v1alpha1"
    and resource.get("kind") == "CloudflareControlPlane"
]
if len(matches) != 1:
    raise SystemExit(f"expected exactly one CloudflareControlPlane resource, found {len(matches)}")

recovery_config = pathlib.Path("/run/verself/recovery/cloudflare/gamma.cloudflare-recovery.json")
recovery_config.parent.mkdir(parents=True, exist_ok=True)
tmp_config = recovery_config.with_name(recovery_config.name + "." + str(os.getpid()) + ".tmp")
tmp_config.write_text(json.dumps(matches[0], sort_keys=True, separators=(",", ":")) + "\n", encoding="utf-8")
os.chmod(tmp_config, 0o600)
os.replace(tmp_config, recovery_config)
PY
        ]
      }

      resources {
        cpu    = 50
        memory = 64
      }
    }

    task "recover" {
      driver = "raw_exec"
      user = "root"

      vault {
        env  = false
        role = "cloudflare-integration-recovery-runtime"
      }

      identity {
        name = "vault_default"
        aud  = ["vault.io"]
        ttl  = "1h"
      }

      config {
        command = "/var/lib/cloudflare-control-plane/runtime/current/bin/cloudflare-control-plane"
        args = [
          "--action=recover",
          "--recovery-config=/run/verself/recovery/cloudflare/gamma.cloudflare-recovery.json",
          "--timeout=10m",
        ]
      }

      env {
        BAO_ADDR = "https://127.0.0.1:8200"
        BAO_CACERT = "/etc/verself/openbao/ca.pem"
        OTEL_EXPORTER_OTLP_ENDPOINT = "http://127.0.0.1:4317"
        OTEL_RESOURCE_ATTRIBUTES = "verself.supervisor=nomad"
        OTEL_SERVICE_NAME = "cloudflare-integration-recovery"
        VERSELF_SITE = "gamma"
        VERSELF_SUPERVISOR = "nomad"
      }

      resources {
        cpu = 150
        memory = 128
      }

      restart {
        attempts = 20
        delay = "15s"
        interval = "10m"
        mode = "fail"
      }
    }
  }
}
