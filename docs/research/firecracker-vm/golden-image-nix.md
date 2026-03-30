# Golden Image Construction, Nix Packaging, and KVM Availability

> How to build the ext4-inside-zvol guest rootfs with Nix, what's already in nixpkgs,
> and whether KVM works on Latitude.sh bare metal.
>
> Researched 2026-03-29.

## Nix tooling for rootfs construction

### `make-ext4-fs.nix` (recommended for forge-metal)

Source: [`nixos/lib/make-ext4-fs.nix`](https://github.com/NixOS/nixpkgs/blob/master/nixos/lib/make-ext4-fs.nix)

A standalone Nix derivation that builds an ext4 filesystem image. Key parameters:

- `storePaths` -- list of derivations whose closures are included
- `populateImageCommands` -- shell commands to populate a `./files` directory (copied to rootfs)
- `volumeLabel`, `uuid` -- filesystem metadata

Build process:
1. Computes closure of all `storePaths` via `pkgs.closureInfo`
2. Creates `./rootImage/nix/store/` and copies all store paths
3. Copies `./files/` into `./rootImage/` (custom init, config, etc.)
4. Calculates image size: `numInodes * 2 * 4096 + numDataBlocks * 4096 * 1.20`
5. Creates ext4 image with `mkfs.ext4 -d ./rootImage` (**no mount, no root, no loop device**)
6. Runs `fsck.ext4` + `resize2fs -M` + adds 16 MiB headroom

The `-d` flag populates from a directory in userspace. This is critical: it works
inside the Nix sandbox with no privileges. No FUSE, no loop devices.

### `make-disk-image.nix` (overkill for this use case)

Source: [`nixos/lib/make-disk-image.nix`](https://github.com/NixOS/nixpkgs/blob/master/nixos/lib/make-disk-image.nix)

Designed for full NixOS bootable images with partition tables. Uses `nixos-install`
and supports qcow2/vdi output. The `partitionTableType = "none"` option produces a
bare filesystem, but it still runs `nixos-install` which is unnecessary for a non-NixOS
rootfs.

### microvm.nix

Source: [github.com/microvm-nix/microvm.nix](https://github.com/microvm-nix/microvm.nix)

The most mature NixOS-native Firecracker integration. Uses erofs/squashfs (read-only)
for `/nix/store`, not ext4. Designed for long-running NixOS VMs. Not directly usable
for forge-metal's ephemeral CI VMs (needs writable rootfs, custom init, no systemd).

Useful reference for kernel handling:
```nix
kernelPath = "${kernel.dev}/vmlinux";  # extracts vmlinux from kernel .dev output
```

Boot args: `console=ttyS0 noapic acpi=off reboot=k panic=1 i8042.noaux i8042.nomux
i8042.nopnp i8042.dumbkbd`

## Golden image build recipe for forge-metal

```nix
# In flake.nix
ciGuestRootfs = pkgs.callPackage (pkgs.path + "/nixos/lib/make-ext4-fs.nix") {
  storePaths = [
    forgevm-init        # custom Go init binary (PID 1)
    pkgs.nodejs_22      # Node.js for CI jobs
    pkgs.gvisor         # runsc for syscall sandboxing
    pkgs.openbao        # bao CLI for secret unwrap
    pkgs.coreutils pkgs.bash pkgs.git pkgs.openssh
    nodeModulesClosure  # pre-installed node_modules
  ];

  populateImageCommands = ''
    # Custom init as PID 1
    mkdir -p ./files/sbin
    cp ${forgevm-init}/bin/forgevm-init ./files/sbin/init

    # Essential directories
    mkdir -p ./files/{dev,proc,sys,tmp,run,etc,home/runner,opt}

    # DNS resolution (baked into golden image)
    echo "nameserver 8.8.8.8" > ./files/etc/resolv.conf

    # gVisor runsc
    mkdir -p ./files/usr/bin
    cp ${pkgs.gvisor}/bin/runsc ./files/usr/bin/runsc

    # OpenBao CLI
    cp ${pkgs.openbao}/bin/bao ./files/usr/bin/bao

    # Pre-warmed node_modules cache
    mkdir -p ./files/opt/node_modules
    cp -a ${nodeModulesClosure}/* ./files/opt/node_modules/

    # Passwd/group for the CI runner user
    echo "root:x:0:0::/root:/bin/bash" > ./files/etc/passwd
    echo "runner:x:1000:1000::/home/runner:/bin/bash" >> ./files/etc/passwd
    echo "root:x:0:" > ./files/etc/group
    echo "runner:x:1000:" >> ./files/etc/group
  '';

  volumeLabel = "ci-rootfs";
};

# The vmlinux kernel for Firecracker
ciKernel = pkgs.linuxManualConfig {
  src = pkgs.linuxKernel.kernels.linux_6_1.src;
  version = "6.1-firecracker";
  configfile = ./resources/microvm-kernel-ci-x86_64-6.1.config;
};
# Or use the standard NixOS kernel: "${pkgs.linuxPackages_latest.kernel.dev}/vmlinux"
```

### Deployment to host ZFS zvol

The Nix-built ext4 image is a file. To create the golden zvol:

```bash
# Build the image
nix build .#ciGuestRootfs

# Create the zvol (sized to match image + headroom for writes)
zfs create -V 4G pool/golden-zvol

# Write the ext4 image into the zvol
dd if=result/root.ext4 of=/dev/zvol/pool/golden-zvol bs=1M

# Snapshot for cloning
zfs snapshot pool/golden-zvol@ready

# Per-job:
zfs clone pool/golden-zvol@ready pool/ci/job-abc
# Boot Firecracker with /dev/zvol/pool/ci/job-abc as rootfs
```

### What goes into the golden image vs what's injected per-job

| Golden image (baked in) | Per-job (MMDS or vsock) |
|------------------------|------------------------|
| Node.js, npm, git, bash | Repo URL, commit SHA |
| gVisor runsc | Job ID, job timeout |
| OpenBao bao CLI | Wrapping token (via vsock) |
| Custom init binary | Environment variables |
| DNS config | Runner registration token |
| Pre-warmed node_modules cache | |
| `/etc/passwd`, `/etc/group` | |

## Firecracker in nixpkgs

### Package status

Source: [`pkgs/by-name/fi/firecracker/package.nix`](https://github.com/NixOS/nixpkgs/blob/master/pkgs/by-name/fi/firecracker/package.nix)

| Property | Value |
|----------|-------|
| Version | **1.14.2** (nixpkgs unstable) |
| Language | Rust (`rustPlatform.buildRustPackage`) |
| Builds | `cargoBuildFlags = [ "--workspace" ]` -- builds **both `firecracker` and `jailer`** |
| Install | All executable binaries from release dir -- **jailer is included** |
| Platform | `lib.platforms.linux` only |
| License | Apache 2.0 |
| Dependencies | `libseccomp`, `cmake`, `gcc`, `rust-bindgen` |

### No NixOS module

There is no `services.firecracker` or `virtualisation.firecracker` NixOS module.
The package provides just the binaries. For NixOS integration, use microvm.nix or
manage Firecracker directly.

### firecracker-containerd: not in nixpkgs

Only a third-party Nix environment exists:
[MarcoPolo/firecracker-containerd-nix](https://github.com/MarcoPolo/firecracker-containerd-nix)

### Kernel options

1. **Standard NixOS kernel vmlinux:** `${pkgs.linuxPackages_latest.kernel.dev}/vmlinux`
   Works but ~100MB+. Has all needed CONFIG options.

2. **Custom minimal kernel:** Use `linuxManualConfig` with Firecracker's recommended
   configs from `resources/guest_configs/`. Produces ~10-20MB vmlinux.

3. **microvm.nix kernel:** Provides a pre-configured Firecracker kernel derivation.

### Adding to forge-metal's flake.nix

```nix
# In the server-profile or as a separate output
packages.firecracker = pkgs.firecracker;  # includes firecracker + jailer
packages.ciGuestRootfs = ciGuestRootfs;   # ext4 golden image
packages.ciKernel = ciKernel;             # minimal vmlinux
```

## KVM on Latitude.sh bare metal

### Answer: yes, guaranteed

Latitude.sh provides bare metal servers with full hardware access, no hypervisor.
Their fleet is [95% AMD EPYC](https://www.amd.com/en/resources/case-studies/latitude-sh.html).
All AMD EPYC processors have AMD-V enabled -- there is no BIOS setting to disable it
on server-class EPYC chips.

Available models:

| Plan | CPU | Cores | Price |
|------|-----|-------|-------|
| `m4.metal.medium` | AMD EPYC 9124 | 16 | $0.57/hr |
| `m4.metal.large` | AMD EPYC 9254 | 24 | $1.00/hr |
| `rs4.metal.large` | AMD EPYC 9354P | 32 | $1.77/hr |
| `rs4.metal.xlarge` | AMD EPYC 9554P | 64 | $3.14/hr |

AMD EPYC 9000-series (Zen 4) supports AMD-V, AMD-Vi (IOMMU), SEV, and SEV-SNP.

### Verification after provisioning

```bash
# Check CPU virtualization flags
grep -c -E 'svm|vmx' /proc/cpuinfo

# Check /dev/kvm exists
ls -la /dev/kvm

# If not present, load the module
sudo modprobe kvm_amd

# Set permissions for non-root use
sudo setfacl -m u:${USER}:rw /dev/kvm
```

On Ubuntu 24.04 (forge-metal's target), the `kvm_amd` module auto-loads. The only
scenario where `/dev/kvm` might not appear is if the module is not loaded, which is
trivially fixed.

### Integration with forge-metal's Ansible

Add to the `base` role or a dedicated `kvm` role:

```yaml
- name: Ensure KVM module is loaded
  modprobe:
    name: kvm_amd
    state: present

- name: Verify /dev/kvm exists
  stat:
    path: /dev/kvm
  register: kvm_dev
  failed_when: not kvm_dev.stat.exists
```

Source: [Firecracker dev machine setup](https://github.com/firecracker-microvm/firecracker/blob/main/docs/dev-machine-setup.md),
[AMD case study](https://www.amd.com/en/resources/case-studies/latitude-sh.html),
[Latitude.sh blog](https://www.latitude.sh/blog/what-is-bare-metal-cloud)

## Sources

- [make-ext4-fs.nix](https://github.com/NixOS/nixpkgs/blob/master/nixos/lib/make-ext4-fs.nix)
- [make-disk-image.nix](https://github.com/NixOS/nixpkgs/blob/master/nixos/lib/make-disk-image.nix)
- [nixpkgs firecracker package.nix](https://github.com/NixOS/nixpkgs/blob/master/pkgs/by-name/fi/firecracker/package.nix)
- [microvm.nix](https://github.com/microvm-nix/microvm.nix)
- [Cloudkernels: rootfs for Firecracker](https://blog.cloudkernels.net/posts/fc-rootfs/)
- [Hans Pistor: Building a rootfs](https://hans-pistor.tech/posts/building-a-rootfs-for-firecracker/)
- [ForgeVM 28ms blog](https://dev.to/adwitiya/how-i-built-sandboxes-that-boot-in-28ms-using-firecracker-snapshots-i0k)
- [Firecracker rootfs-and-kernel-setup.md](https://github.com/firecracker-microvm/firecracker/blob/main/docs/rootfs-and-kernel-setup.md)
- [Firecracker dev-machine-setup.md](https://github.com/firecracker-microvm/firecracker/blob/main/docs/dev-machine-setup.md)
- [AMD EPYC Latitude.sh case study](https://www.amd.com/en/resources/case-studies/latitude-sh.html)
