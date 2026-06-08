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

variable "zot_resource_name" {
  type    = string
  default = "zot"
}

# No default: the digest IS the image version. Leaving this unset forces the
# deploy to supply the sha of the bytes it built, so changed image bytes can
# never be silently served from a stale archive.
variable "zot_image_sha256" {
  type = string
}

# Fixed in-container runtime prefix: the zot_runtime tar lays the binaries under
# /opt/verself/zot/usr/bin. The image digest IS the runtime version; there is no
# releases/current indirection.
locals {
  zot_binary    = "/opt/verself/zot/usr/bin/zot"
  config_binary = "/opt/verself/zot/usr/bin/zot-config"
  storage_dir   = "/var/lib/zot/storage"
  # L6: zot IS the OCI registry, so its own image must load from a local
  # docker-archive file on the host filesystem, never pulled from a running
  # registry. Nothing here needs zot running for zot to start.
  image = "docker-archive:${var.artifact_root}/sha256/${var.zot_image_sha256}"
}

job "zot" {
  name        = "zot"
  datacenters = ["*"]
  type        = "service"

  meta {
    image_sha256 = "${var.zot_image_sha256}"
  }

  group "zot" {
    count = 1

    reschedule {
      delay          = "15s"
      delay_function = "exponential"
      max_delay      = "5m"
      unlimited      = true
    }

    network {
      mode = "host"
      port "http" {
        host_network = "loopback"
        static       = 5080
        to           = 5080
      }
    }

    # L1: the podman driver does not create bind-mount sources (unlike
    # `docker -v`); a missing host path fails container creation with
    # `statfs: no such file or directory`. The durable registry storage tree
    # must exist before the podman tasks bind it. uid/gid 1001 matches the fixed
    # identity baked into the image (L7), so blob/manifest ownership is
    # deterministic across rebuilds and the in-container zot user can read it.
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
          "-o",
          "1001",
          "-g",
          "1001",
          "-m",
          "0750",
          "${local.storage_dir}",
        ]
      }

      resources {
        cpu    = 25
        memory = 16
      }
    }

    # Renders the ephemeral, derived runtime config into /alloc: config.json and
    # the bcrypt htpasswd line. Both are derived state (L5) regenerated every
    # alloc from the CRD + the durable publisher password, so they live in
    # /alloc, never on a host bind-mount. The renderer is the in-image zot-config
    # Go binary (no python in the image; mirrors cloudflare's document.json
    # reader).
    task "config" {
      driver = "podman"
      user   = "zot"

      # Restart budget absorbs the prestart race with provision-host-dirs (near
      # instant vs. podman container creation) and transient podman socket flaps
      # on reconverge; rendering is idempotent.
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
        role = "zot-runtime"
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
        cap_drop        = ["all"]
        security_opt    = ["no-new-privileges"]
        command         = "${local.config_binary}"
        args = [
          "--resource-graph=/guardian/document.json",
          "--resource-name=${var.zot_resource_name}",
          # /secrets is the in-container NOMAD_SECRETS_DIR mount; matches the
          # template destination below. Hardcoded (not ${env[...]} interpolated)
          # to follow the postgresql precedent — the podman driver does not
          # expand env in args.
          "--password-file=/secrets/zot.publisher.password",
        ]
        volumes = [
          "${var.repo_root}/workspace/.guardian/fly/document.json:/guardian/document.json:ro,noexec",
        ]
        tmpfs = ["/tmp"]
      }

      env {
        HOME   = "/tmp"
        TMPDIR = "/tmp"
      }

      resources {
        cpu    = 50
        memory = 64
      }

      # Generate-once publisher credential. The release publisher (mksk-release)
      # authenticates to zot with this plaintext, so it must be a retrievable
      # SecretPath in kv-runtime, generated once when OpenBao reconciles -- not a
      # per-alloc random value that would rotate the credential out from under
      # every pusher on each reschedule. zot-config derives the bcrypt htpasswd
      # line from this same plaintext.
      template {
        change_mode = "restart"
        destination = "secrets/zot.publisher.password"
        perms       = "0600"
        data        = <<-EOT
{{ with secret "kv-runtime/data/secret/org/zot.publisher.password" }}{{ .Data.data.value }}{{ end }}
EOT
      }
    }

    task "serve" {
      driver = "podman"
      user   = "zot"

      # L4: Nomad owns process lifecycle. Long-lived: "process running" is a
      # maintained invariant (DR contract). mode=delay never gives up, so a
      # killed zot or a transient podman socket flap is retried indefinitely
      # instead of failing the alloc. No /proc supervision is reimplemented here.
      restart {
        attempts = 5
        delay    = "10s"
        interval = "5m"
        mode     = "delay"
      }

      config {
        image        = "${local.image}"
        init         = true
        network_mode = "host"
        # L2: zot writes only to the durable storage bind-mount (blobs,
        # manifests, dedupe cache) and /alloc (its rendered config). zot is a
        # registry -- inbound only, no sync uplink configured -- so it makes no
        # outbound HTTPS and needs no CA trust store mounted (L3 N/A).
        readonly_rootfs = true
        cap_drop        = ["all"]
        security_opt    = ["no-new-privileges"]
        command         = "${local.zot_binary}"
        args            = ["serve", "/alloc/config/config.json"]
        volumes = [
          "${local.storage_dir}:${local.storage_dir}:rw",
        ]
        tmpfs = ["/tmp"]
      }

      env {
        HOME   = "/tmp"
        TMPDIR = "/tmp"
      }

      resources {
        cpu    = 400
        memory = 512
      }

      service {
        name         = "zot-http"
        port         = "http"
        provider     = "nomad"
        address_mode = "auto"

        check {
          name     = "zot-registry-v2"
          type     = "http"
          path     = "/v2/"
          interval = "2s"
          timeout  = "3s"
        }
      }
    }
  }
}
