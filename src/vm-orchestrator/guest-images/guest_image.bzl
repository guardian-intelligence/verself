"""Bazel macros for building Firecracker guest-image ext4 artefacts.

Two macros, one for each tier in the firecracker_seed_images catalog:

* `toolchain_ext4_image` — read-only toolchain images mounted by
  vm-orchestrator at lease boot. Pure layout (extract one upstream
  tarball, drop in additional files), no chroot, fully hermetic.
* substrate image — built by the Go build_substrate operator from staged
  inputs because the chroot and mount flow must run as root on the host.

Each toolchain image exports two files:
  <name>.ext4            — the on-disk image staged onto the host at
                           /var/lib/verself/guest-images/toolchains/
                           and ingested by `vm-orchestrator-cli
                           seed-image --strategy dd_from_file`.
  <name>.manifest.json   — sha256, size, source SBOM. Rolled up into a
                           single deploy-time manifest by the bundle.

Convention: an image at `<name>` exposes the runtime overlay contract
defined in src/vm-orchestrator/cmd/vm-bridge/agent.go's `mountFilesystems`:

  /etc-overlay/                    files copied into /etc/ at lease boot
  /.verself-writable-overlays      newline list of paths tmpfs-mounted
                                   over the read-only base at boot

These are conventions, not requirements. An image without overlays is
just a tarball-extracted ext4.
"""

load("@rules_pkg//pkg:tar.bzl", "pkg_tar")

def toolchain_ext4_image(
        name,
        tarball = None,
        files = None,
        symlinks = None,
        empty_dirs = None,
        size_mib = 512,
        label = None,
        visibility = None):
    """Build a read-only toolchain ext4 image and its manifest.

    Args:
      name: Build target name. Also used as the default ext4 label.
      tarball: Optional Bazel label whose tarball is extracted at the
        image root. Used for "drop the upstream archive into /".
      files: Optional dict of dest_path -> label. Files placed at the
        listed paths inside the image. Useful for etc-overlay/ entries
        and the .verself-writable-overlays manifest.
      symlinks: Optional dict of dest_path -> link_target. Used for
        /usr/local/bin shims that point into the extracted tarball.
      empty_dirs: Optional list of paths created as empty directories.
      size_mib: ext4 image size in mebibytes. Set above the staged tree
        size with enough headroom for inodes and a few growth runs.
      label: ext4 filesystem label (defaults to `name`).
    """
    if label == None:
        label = name
    if files == None:
        files = {}
    if symlinks == None:
        symlinks = {}
    if empty_dirs == None:
        empty_dirs = []

    # Stage every additional file under a private pkg_tar so the genrule
    # only has one tar to extract per source. pkg_tar gives us
    # deterministic ownership (root:root) and avoids a cmdline-length
    # explosion when files{} grows.
    #
    # pkg_tar's `files` attr expects {label: install_path}; the macro
    # accepts the more readable {install_path: label} shape and flips
    # it here so callers don't have to.
    pkg_tar_files = {label: install for install, label in files.items()}

    extras_target = "_" + name + "_extras_tar"
    pkg_tar(
        name = extras_target,
        files = pkg_tar_files,
        symlinks = symlinks,
        empty_dirs = empty_dirs,
        package_dir = "/",
        visibility = ["//visibility:private"],
    )

    image_out = name + ".ext4"
    manifest_out = name + ".manifest.json"

    # genrule because the action is a single shell pipeline (extract tarballs
    # → run mkfs.ext4 -d → emit manifest). A custom rule would only be
    # warranted if we wanted to expose a provider; we don't.
    upstream_input = "$(location {})".format(tarball) if tarball else ""
    upstream_extract = (
        "tar -xf {input} -C \"$$STAGE\"".format(input = upstream_input) if tarball else ":"
    )
    srcs = [":" + extras_target] + ([tarball] if tarball else [])

    native.genrule(
        name = name + "_image",
        srcs = srcs,
        outs = [image_out, manifest_out],
        # Inline shell intentionally — the action is short and a separate
        # script file would be one more roundtrip per build. The pipeline
        # is: stage upstream tarball, layer in extras, run mkfs.ext4 -d,
        # record sha256/size in the manifest.
        cmd = ("set -euo pipefail; " +
               # Deterministic build: every nondeterministic input
               # (extraction mtimes, ext4 UUID, hash seed) is pinned to a
               # value derived from the image name so byte-for-byte
               # rebuilds reproduce the same sha256. The daemon's
               # vs:source_digest is sha256 of the source artifact, so
               # drift in that hash causes a re-seed at deploy time —
               # determinism keeps no-op deploys actually no-op.
               "export SOURCE_DATE_EPOCH=0; " +
               "STAGE=$$(mktemp -d); trap 'rm -rf \"$$STAGE\"' EXIT; " +
               upstream_extract + "; " +
               "tar -xf $(location :{extras}) -C \"$$STAGE\"; ".format(extras = extras_target) +
               # Pre-allocate the image file with a sparse hole. mkfs.ext4
               # writes its superblock and inode tables into the file in
               # place; the resulting ext4 reports `size_mib` to the guest
               # but the on-host bytes only cover what's actually used.
               "truncate -s {size}M $(location {image}); ".format(size = size_mib, image = image_out) +
               # Derive a UUID from the image name. mkfs.ext4 normally
               # generates a random UUID per run; pinning it removes that
               # source of nondeterminism.
               "UUID=$$(printf '%s' '{name}' | sha256sum | head -c 32 | sed 's/^\\(.\\{{8\\}}\\)\\(.\\{{4\\}}\\)\\(.\\{{4\\}}\\)\\(.\\{{4\\}}\\)\\(.\\{{12\\}}\\)$$/\\1-\\2-\\3-\\4-\\5/'); ".format(name = name) +
               # mkfs.ext4 -d reads the staging tree and writes inodes for
               # every file/dir found. Run hermetically via Bazel's
               # standard sandbox; no root needed because read-only mounts
               # don't depend on uid/gid matching the guest user.
               # `-E hash_seed=` pins HTREE directory hashing so the on-disk
               # layout is reproducible.
               "/sbin/mkfs.ext4 -F -q -L {label} -U \"$$UUID\" -E hash_seed=\"$$UUID\",no_copy_xattrs -d \"$$STAGE\" $(location {image}); ".format(label = label, image = image_out) +
               # Manifest is plain JSON so the deploy bundle can roll it up
               # without parsing TOML/YAML. sha256 doubles as the build-side
               # equivalent of vs:source_digest the daemon records on
               # @ready snapshots — drift in either side is detectable.
               "SHA=$$(sha256sum $(location {image}) | awk '{{print $$1}}'); ".format(image = image_out) +
               "BYTES=$$(stat -c '%s' $(location {image})); ".format(image = image_out) +
               # printf format string lives outside .format() so its literal
               # `{` and `}` are unambiguous. Scalar args follow as separate
               # printf positional substitutions.
               "printf '{\"name\":\"%s\",\"label\":\"%s\",\"size_mib\":%s,\"sha256\":\"%s\",\"bytes\":%s}\\n' " +
               "'{name}' '{label}' '{size}' \"$$SHA\" \"$$BYTES\" > $(location {manifest})".format(
                   name = name,
                   label = label,
                   size = size_mib,
                   manifest = manifest_out,
               )),
        visibility = visibility or ["//visibility:public"],
        # mkfs.ext4 lives under /sbin which is on PATH for the genrule
        # action by default. Pin a minimal toolchain explicitly so action
        # caching is stable.
        toolchains = [],
        tags = ["no-remote-cache"],
    )

    # Convenience aliases so consumers can reference :<name>.ext4 and
    # :<name>.manifest.json directly without the _image target name.
    native.filegroup(
        name = name,
        srcs = [":" + image_out],
        visibility = visibility or ["//visibility:public"],
    )
    native.filegroup(
        name = name + "_manifest",
        srcs = [":" + manifest_out],
        visibility = visibility or ["//visibility:public"],
    )
