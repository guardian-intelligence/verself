# Firecracker Snapshot Lifecycle

vm-orchestrator treats Firecracker snapshots as lease activation cache
artifacts. They accelerate the interval between host resource preparation and
guest control initialization. ZFS generations remain the durable source of
truth for workspace and cache state after a workload succeeds.

The lease boot path has three phases:

1. Prepare the lease environment.
2. Activate the microVM.
3. Initialize the guest lease.

Preparation resolves all host resources that must exist before Firecracker can
run: encrypted storage namespace, root zvol clone, composed filesystem zvol
clones, jail root, block device nodes, TAP lease, API socket path, vsock control
socket path, and metrics path.

Activation chooses one of two strategies:

- Cold boot configures Firecracker from the prepared environment, starts the
  instance, waits for vm-bridge to reach the pre-control listener boundary,
  pauses the VM, creates a pre-control snapshot artifact, resumes the VM, and
  continues into guest initialization.
- Snapshot restore loads a pre-control snapshot into a fresh Firecracker
  process, applies a restore-time network override that binds `eth0` to the
  lease's allocated TAP, resumes the VM, and continues into guest
  initialization.

Guest initialization is shared by both activation strategies. The host connects
to vm-bridge over the restored or cold-booted vsock control socket and sends
`LeaseInit`. vm-bridge applies the lease network, mounts filesystems, syncs the
wall clock, starts its local control socket, and then returns `LeaseInitResult`.
The lease becomes ready only after that result.

## Snapshot Boundary

The reusable snapshot boundary is after Firecracker instance start and after
vm-bridge has opened its vsock listener, before the host connects to the control
socket. This is the pre-control boundary.

The snapshot does not contain:

- lease ID;
- attempt ID;
- customer exec state;
- wall-clock-correct guest time;
- per-lease network address;
- mounted durable filesystems after `LeaseInit`;
- product bootstrap secrets or runner tokens.

The snapshot contains the booted guest kernel, substrate userspace, vm-bridge at
the vsock listener boundary, Firecracker device model state, and memory needed
to resume at that boundary.

## LeaseInit Contract

`LeaseInit` is the only post-activation hook. It is responsible for all
lease-specific state:

- flushing and assigning the guest IP, route, DNS, and neighbor table;
- mounting the prepared filesystem devices at the requested guest paths;
- applying read-only toolchain overlays;
- syncing wall clock from the host;
- starting vm-bridge local control after network and filesystems are valid.

This keeps cold boot and snapshot restore behavior identical after activation.
No customer workload runs before `LeaseInitResult`.

## Cache Key

The snapshot key is a hash over:

- `fc-precontrol-v1`;
- the prepared disk layout key;
- the Firecracker runtime ABI key;
- the network model key.

The disk layout key is derived from the resolved zvol graph: root substrate
image, root size, ordered drive IDs, source refs, mounted paths, bind paths,
filesystem type, and read-only flags. GitHub workflow and matrix semantics stay
in sandbox-rental's durable cache and job-shape model. If a matrix job changes
the durable zvol generation mounted into the VM, the resolved disk layout key
changes.

The runtime ABI key covers the compatibility surface that ZFS keys do not:
Firecracker version, kernel path/content identity, kernel command line,
vm-bridge/vmproto protocol version, vCPU count, memory size, and snapshot hook
version.

The network model key is `network-overrides+lease-init-v1`. Firecracker
`LoadSnapshot.network_overrides` binds the restored `eth0` device to the
lease's fresh TAP. vm-bridge `LeaseInit` applies the guest IP, gateway, and DNS.
The key does not include TAP name, slot index, guest IP, or gateway.

## Deploy Ownership

The `vm-orchestrator` Nomad component owns both sides of the host/guest ABI:
the daemon binary and the substrate image containing vm-bridge. Its prestart
task runs `vm-orchestrator-cli stage-guest-images`, which digest-checks the
Bazel-built substrate input bundle, rebuilds the substrate ext4 when needed,
and atomically stages
`/var/lib/verself/guest-images/{substrate.ext4,vmlinux,...}` before the daemon
starts. The poststart `seed-catalog` task then materializes those staged files
into ZFS image zvols.

This makes additive vm-bridge fields, snapshot hook changes, and guest
after-restore behavior deploy with the daemon revision that expects them.

## Post-Workload State

Successful workload state is saved through `CommitFilesystemMount`. vm-bridge
seals the writable filesystem, the host flushes the block device, and ZFS
creates the immutable durable generation. That generation feeds future disk
layout keys.

Reusable Firecracker snapshots are not created from post-workload VMs. The
accepted-control state includes host control sessions, guest process state,
time-sensitive kernel state, and workload effects that are outside the
pre-control contract. Cache refreshes build a fresh pre-control artifact from
the resolved zvol layout.
