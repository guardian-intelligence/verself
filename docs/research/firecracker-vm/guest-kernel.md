# Firecracker Guest Kernel Configuration for CI

> Building a minimal, fast-booting guest kernel for ephemeral CI workloads.
> Primary source: Firecracker's `resources/guest_configs/` and `docs/kernel-policy.md`.
>
> Repo: [firecracker-microvm/firecracker](https://github.com/firecracker-microvm/firecracker)
> Researched 2026-03-29.

## Official kernel configs

Firecracker provides configs in `resources/guest_configs/`:

| File | Purpose |
|------|---------|
| `microvm-kernel-ci-x86_64-6.1.config` | Primary x86_64 CI config (1246 enabled, 1240 disabled) |
| `microvm-kernel-ci-x86_64-5.10-no-acpi.config` | Legacy non-ACPI config (5.10 kernel) |
| `ci.config` | Fragment merged on top of base configs |
| `pcie.config` | PCI Express fragment (8 options for virtio-pci transport) |
| `virtio-mem.config` | `CONFIG_VIRTIO_MEM=y` for dynamic memory |
| `vmclock.config` | `CONFIG_PTP_1588_CLOCK_VMCLOCK=y` (v1.15+) |

**Critical caveat** from [`DISCLAIMER.md`](https://github.com/firecracker-microvm/firecracker/blob/main/resources/guest_configs/DISCLAIMER.md):
these configs target Amazon Linux's microvm kernel fork (`github.com/amazonlinux/linux`,
`microvm-kernel-*` tags), not upstream mainline. They may include backported patches.

Source: [`resources/guest_configs/`](https://github.com/firecracker-microvm/firecracker/tree/main/resources/guest_configs)

## Absolute minimum for boot (x86_64)

With a root block device (forge-metal's use case -- ZFS zvol with ext4 as `/dev/vda`):

```
CONFIG_VIRTIO_BLK=y          # virtio block device
CONFIG_ACPI=y                # ACPI boot path (6.1+ default)
CONFIG_PCI=y                 # needed for ACPI init, even without PCI devices
CONFIG_KVM_GUEST=y           # KVM_CLOCK for timekeeping
```

With an initrd (alternative):

```
CONFIG_BLK_DEV_INITRD=y
CONFIG_KVM_GUEST=y
```

Source: [`docs/kernel-policy.md`](https://github.com/firecracker-microvm/firecracker/blob/main/docs/kernel-policy.md)

## Recommended production config

```
# --- Timekeeping ---
CONFIG_KVM_GUEST=y
CONFIG_PARAVIRT=y
CONFIG_PARAVIRT_CLOCK=y
CONFIG_PTP_1588_CLOCK=y
CONFIG_PTP_1588_CLOCK_KVM=y

# --- Entropy ---
CONFIG_HW_RANDOM_VIRTIO=y             # virtio RNG device
CONFIG_RANDOM_TRUST_CPU=y             # trust RDRAND at boot (faster initial entropy)

# --- Virtio Devices ---
CONFIG_VIRTIO_MMIO=y                  # MMIO transport (still needed alongside ACPI)
CONFIG_VIRTIO_BLK=y                   # block devices
CONFIG_VIRTIO_NET=y                   # networking
CONFIG_VIRTIO_VSOCKETS=y              # vsock for host<->guest comms
CONFIG_VIRTIO_BALLOON=y               # memory balloon
CONFIG_MEMORY_BALLOON=y

# --- Boot/Init ---
CONFIG_BLK_DEV_INITRD=y
CONFIG_DEVTMPFS=y                     # auto-populate /dev
CONFIG_DEVTMPFS_MOUNT=y               # auto-mount devtmpfs

# --- Shutdown ---
CONFIG_SERIO_I8042=y                  # i8042 controller
CONFIG_KEYBOARD_ATKBD=y               # Ctrl+Alt+Del shutdown

# --- Serial (optional, adds ~10-20ms boot latency) ---
CONFIG_SERIAL_8250_CONSOLE=y
CONFIG_PRINTK=y
```

## Container support (containerd + runc inside VM)

Everything must be `=y` (no modules -- `CONFIG_MODULES` is disabled):

```
# --- Namespaces ---
CONFIG_NAMESPACES=y
CONFIG_UTS_NS=y
CONFIG_IPC_NS=y
CONFIG_USER_NS=y
CONFIG_PID_NS=y
CONFIG_NET_NS=y

# --- Cgroups ---
CONFIG_CGROUPS=y
CONFIG_MEMCG=y
CONFIG_MEMCG_KMEM=y
CONFIG_BLK_CGROUP=y
CONFIG_CGROUP_SCHED=y
CONFIG_CGROUP_PIDS=y
CONFIG_CGROUP_FREEZER=y
CONFIG_CGROUP_DEVICE=y
CONFIG_CGROUP_CPUACCT=y
CONFIG_CGROUP_HUGETLB=y
CONFIG_CGROUP_PERF=y
CONFIG_CGROUP_BPF=y
CONFIG_CGROUP_NET_PRIO=y
CONFIG_CGROUP_NET_CLASSID=y
CONFIG_CPUSETS=y
CONFIG_FAIR_GROUP_SCHED=y

# --- Container networking ---
CONFIG_VETH=y                   # virtual ethernet pairs
CONFIG_BRIDGE=y
CONFIG_BRIDGE_NETFILTER=y

# --- Netfilter/iptables ---
CONFIG_NETFILTER=y
CONFIG_NETFILTER_ADVANCED=y
CONFIG_NF_CONNTRACK=y
CONFIG_NF_NAT=y
CONFIG_NF_NAT_MASQUERADE=y
CONFIG_NETFILTER_XTABLES=y
CONFIG_IP_NF_IPTABLES=y
CONFIG_IP_NF_FILTER=y
CONFIG_IP_NF_NAT=y
CONFIG_IP_NF_TARGET_MASQUERADE=y

# --- Overlay filesystem ---
CONFIG_OVERLAY_FS=y
```

All of these are already enabled in Firecracker's CI configs.

Source: [Gentoo Docker wiki](https://wiki.gentoo.org/wiki/Docker),
[firecracker-containerd](https://github.com/firecracker-microvm/firecracker-containerd)

## ext4 on virtio-block (ZFS zvol use case)

The guest sees `/dev/vda` with ext4. Zero ZFS awareness needed:

```
CONFIG_VIRTIO_BLK=y
CONFIG_EXT4_FS=y
CONFIG_EXT4_USE_FOR_EXT2=y         # handle ext2/3 without separate drivers
CONFIG_EXT4_FS_POSIX_ACL=y
CONFIG_EXT4_FS_SECURITY=y          # security labels for containers
CONFIG_BLK_MQ_VIRTIO=y             # multiqueue for virtio-blk
```

## gVisor/runsc (systrap platform)

gVisor's **systrap** platform is the right choice inside a Firecracker VM since there
is no nested KVM. It works entirely in userspace via seccomp + signals:

1. Workload threads started as child processes via ptrace
2. Restrictive seccomp filter installed with `SECCOMP_RET_TRAP`
3. Trapped syscalls fire SIGSYS, caught by gVisor's signal handler
4. Sentry processes the syscall and resumes the thread

Required kernel config:

```
CONFIG_SECCOMP=y
CONFIG_SECCOMP_FILTER=y           # seccomp-bpf
CONFIG_BPF=y
CONFIG_BPF_SYSCALL=y
```

**No nested KVM needed.** Systrap runs entirely in userspace. Kernel >= 5.14
recommended for full `core_sched` support.

Source: [gVisor Platform Guide](https://gvisor.dev/docs/architecture_guide/platforms/),
[gVisor systrap](https://github.com/google/gvisor/blob/master/pkg/sentry/platform/systrap/README.md)

## Boot time optimization

### Firecracker's boot guarantee

**<= 125ms** from `InstanceStart` to `/sbin/init`, with serial disabled, minimal kernel,
on M5D.metal / M6G.metal instances.

Source: [`SPECIFICATION.md`](https://github.com/firecracker-microvm/firecracker/blob/main/SPECIFICATION.md)

### What adds latency

| Option | Impact | Rationale |
|--------|--------|-----------|
| `CONFIG_SERIAL_8250_CONSOLE=y` | ~10-20ms | UART init + character output |
| `CONFIG_PRINTK=y` | Variable | Every `printk` adds time |
| `CONFIG_MODULES=y` | Significant | Module loading stalls |
| `CONFIG_ACPI=y` | ~5-10ms | Table parsing (offset by avoiding legacy probing) |
| `CONFIG_DEBUG_INFO=y` | None at runtime | Increases kernel image size |
| `CONFIG_NR_CPUS=64` | Small | Larger per-CPU structures; set to actual max |
| `CONFIG_AUDIT=y` | Small | Audit framework init |
| `CONFIG_PROFILING=y` | Small | Profiling infrastructure init |

### Default kernel command line (injected by Firecracker)

```
reboot=k panic=1 nomodule 8250.nr_uarts=0 i8042.noaux i8042.nomux i8042.dumbkbd swiotlb=noforce
```

- `nomodule` -- skip module loading subsystem
- `8250.nr_uarts=0` -- skip UART enumeration
- `i8042.noaux i8042.nomux i8042.dumbkbd` -- skip mouse/mux probing
- `swiotlb=noforce` -- don't force software I/O TLB

### What to strip beyond Firecracker's CI config (for maximum speed)

```
# CONFIG_SERIAL_8250_CONSOLE is not set   # if boot logs not needed
# CONFIG_PRINTK is not set                # if kernel messages not needed
# CONFIG_HIBERNATION is not set            # pointless in ephemeral VMs
# CONFIG_MEMORY_HOTPLUG is not set         # if using fixed memory
# CONFIG_NUMA is not set                   # single-socket microVM
# CONFIG_RANDOMIZE_BASE is not set         # KASLR, if snapshot determinism matters
# CONFIG_PERF_EVENTS is not set            # if not profiling
# CONFIG_AUDIT is not set                  # if no auditing
CONFIG_NR_CPUS=8                           # reduce from default 64
```

## PVH boot mode (v1.12+)

PVH (Para-Virtualized Hardware) is the x86/HVM direct boot ABI. Boots the guest
directly into the kernel without firmware or legacy BIOS paths.

```
CONFIG_PVH=y    # already set in Firecracker's 6.1 and 5.10 configs
```

Firecracker auto-detects the ELF Note and uses PVH when present (Linux >= 5.0 with
`CONFIG_PVH=y`). PVH is x86_64 only.

**Practical impact:** Both PVH and traditional boot load an uncompressed `vmlinux`
directly into guest memory. The theoretical advantage (eliminating MPTable parsing,
legacy device enumeration) is already mostly mitigated by kernel cmdline params. The
measurable delta is likely single-digit ms. PVH's primary value is enabling non-Linux
OSes (FreeBSD).

Source: [PR #3155](https://github.com/firecracker-microvm/firecracker/pull/3155),
[Discussion #4611](https://github.com/firecracker-microvm/firecracker/discussions/4611)

### Two boot architecture choices

**Option A: ACPI boot (recommended, current default):**
```
CONFIG_ACPI=y
CONFIG_PCI=y                                   # needed for ACPI init
CONFIG_KVM_GUEST=y
# CONFIG_X86_MPPARSE is not set                # disable legacy MPTable
# CONFIG_VIRTIO_MMIO_CMDLINE_DEVICES is not set
```

**Option B: Legacy MMIO boot (lighter, deprecated path):**
```
# CONFIG_ACPI is not set
# CONFIG_PCI is not set
CONFIG_VIRTIO_MMIO=y
CONFIG_VIRTIO_MMIO_CMDLINE_DEVICES=y
CONFIG_KVM_GUEST=y
CONFIG_X86_MPPARSE=y
```

Firecracker recommends Option A for new deployments.

## Build process

Firecracker requires **uncompressed ELF** (`vmlinux`) on x86_64. No bootloader,
no decompression step. The VMM loads the kernel directly into guest memory.

```bash
git clone https://github.com/torvalds/linux.git
cd linux && git checkout v6.1
cp /path/to/.config .config
make vmlinux    # produces uncompressed ELF (~30-40MB)
```

Or use Firecracker's devtool:
```bash
./tools/devtool build_ci_artifacts kernels
```

Source: [`docs/rootfs-and-kernel-setup.md`](https://github.com/firecracker-microvm/firecracker/blob/main/docs/rootfs-and-kernel-setup.md)

## initrd vs direct init binary

### Option A: Root block device + init binary (recommended for forge-metal)

```
root=/dev/vda rw init=/sbin/init
```

Kernel mounts ext4 from the zvol clone directly. No intermediate copy. ZFS COW
works optimally.

### Option B: initrd

```
CONFIG_BLK_DEV_INITRD=y
```

Cannot use `pivot_root` (must use `switch_root`). Cannot combine with `is_root_device: true`.
Adds boot latency (RAM allocation, cpio extraction, switch_root).

### Option C: Custom init binary as PID 1 (no systemd)

This is what actuated and the 28ms snapshot project do. A statically-linked Go binary
becomes PID 1:

```go
// Minimal init (from alexellis/firecracker-init-lab)
mount("proc", "/proc", "proc", 0, "")
mount("sysfs", "/sys", "sysfs", 0, "")
mount("devtmpfs", "/dev", "devtmpfs", 0, "")
mount("devpts", "/dev/pts", "devpts", 0, "")
mount("tmpfs", "/dev/shm", "tmpfs", 0, "")
mount("tmpfs", "/run", "tmpfs", 0, "")
// Mount cgroup controllers, configure network, run job
```

Fastest userspace startup (~3ms for minimal Go binary). No systemd overhead
(systemd boot adds 200-500ms). Must handle SIGCHLD reaping, shutdown signals,
mount ordering manually. Ideal for ephemeral CI.

### Recommendation for forge-metal

**Option A + Option C**: ZFS zvol clone as `/dev/vda`, custom Go init as PID 1.

- ZFS clone: ~1.7ms
- Firecracker cold boot to init: ~125ms
- Custom init setup: ~3-5ms
- **Total to job start: ~130ms cold, ~28ms from snapshot**

Source: [`docs/initrd.md`](https://github.com/firecracker-microvm/firecracker/blob/main/docs/initrd.md),
[alexellis/firecracker-init-lab](https://github.com/alexellis/firecracker-init-lab)

## Reference projects

| Project | Approach | Notes |
|---------|----------|-------|
| [actuated](https://actuated.com/blog/firecracker-container-lab) | Pre-built Firecracker kernels, Go init, Docker preinstalled | 110k+ CI VMs, <1s boot |
| [Weaveworks Ignite](https://github.com/weaveworks/ignite) | OCI kernel images, custom config patches | Archived 2023, good config reference |
| [firecracker-containerd](https://github.com/firecracker-microvm/firecracker-containerd) | AWS's official containerd-in-Firecracker | `overlay-init` pattern, cgroup v1 mounts |
| [Bottlerocket](https://github.com/bottlerocket-os/bottlerocket-kernel-kit) | Container-optimized Linux | [Issue #812](https://github.com/bottlerocket-os/bottlerocket/issues/812) for Firecracker variant |

## Sources

- [kernel-policy.md](https://github.com/firecracker-microvm/firecracker/blob/main/docs/kernel-policy.md)
- [rootfs-and-kernel-setup.md](https://github.com/firecracker-microvm/firecracker/blob/main/docs/rootfs-and-kernel-setup.md)
- [SPECIFICATION.md](https://github.com/firecracker-microvm/firecracker/blob/main/SPECIFICATION.md)
- [initrd.md](https://github.com/firecracker-microvm/firecracker/blob/main/docs/initrd.md)
- [guest_configs/ DISCLAIMER.md](https://github.com/firecracker-microvm/firecracker/blob/main/resources/guest_configs/DISCLAIMER.md)
- [gVisor Platform Guide](https://gvisor.dev/docs/architecture_guide/platforms/)
- [gVisor systrap](https://github.com/google/gvisor/blob/master/pkg/sentry/platform/systrap/README.md)
- [alexellis/firecracker-init-lab](https://github.com/alexellis/firecracker-init-lab)
- [28ms sandbox boot](https://dev.to/adwitiya/how-i-built-sandboxes-that-boot-in-28ms-using-firecracker-snapshots-i0k)
