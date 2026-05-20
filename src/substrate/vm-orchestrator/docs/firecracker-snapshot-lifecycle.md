# Firecracker Snapshot Lifecycle

vm-orchestrator treats Firecracker snapshots as lease activation cache
artifacts. ZFS generations remain durable workspace and cache state.

```mermaid
flowchart LR
    prepare["1. Prepare lease environment"] --> activate["2. Activate microVM"]
    activate --> init["3. LeaseInit"]
    init --> ready["lease ready"]
```

```mermaid
flowchart TD
    acquire["AcquireLease"] --> require["RequireReady(org runtime)"]
    require --> zvols["clone root + mount zvols"]
    zvols --> jail["jail root + block devices"]
    jail --> tap["TAP lease + socket paths"]
    tap --> hit{pre-control snapshot hit?}

    hit -->|no| cold["cold boot Firecracker"]
    hit -->|yes| restore["LoadSnapshot + network_overrides"]
    cold --> precontrol["vm-bridge pre-control listener"]
    restore --> precontrol

    precontrol --> leaseinit["LeaseInit(network, filesystems, clock)"]
    leaseinit --> result["LeaseInitResult"]
```

| Case | Behavior |
| --- | --- |
| Snapshot hit | Restore, apply network override, run `LeaseInit`. |
| Snapshot miss | Cold boot, run `LeaseInit`, record computed key. |
| Snapshot creation | Trusted cache refresh only, after the zvol graph is known-good. |

## Snapshot Boundary

```mermaid
sequenceDiagram
    participant VMO as vm-orchestrator
    participant FC as Firecracker
    participant VB as vm-bridge

    VMO->>FC: start or restore instance
    FC->>VB: boot userspace
    VB-->>VMO: pre-control ready on vsock:10788
    Note over VMO,VB: reusable pre-control snapshot boundary
    VMO->>VB: LeaseInit on vsock:10789
    VB-->>VMO: LeaseInitResult
```

| Port | Scope | Protocol |
| --- | --- | --- |
| `10788` | Guest vsock namespace | Pre-control readiness probe. |
| `10789` | Guest vsock namespace | Lease control protocol. |

```mermaid
flowchart TD
    lease["lease"] --> jail["lease jail socket path"]
    lease --> cid["Firecracker guest CID"]
    lease --> tap["TAP name"]
    lease --> init["LeaseInit payload"]
    cid --> ports["fixed guest ports 10788/10789"]
```

| Snapshot contains |
| --- |
| Booted guest kernel. |
| Substrate userspace. |
| vm-bridge at pre-control listener boundary. |
| Firecracker device model state. |
| Memory required to resume at the boundary. |

| Snapshot excludes |
| --- |
| Lease ID. |
| Attempt ID. |
| Customer exec state. |
| Wall-clock-correct guest time. |
| Per-lease network address. |
| Mounted durable filesystems after `LeaseInit`. |
| Product bootstrap secrets or runner tokens. |

```mermaid
flowchart LR
    miss["cache miss"] --> cold["cold boot"]
    cold --> key["record snapshot key"]
    key --> refresh["later trusted cache refresh"]
    refresh --> artifact["pre-control snapshot artifact"]
```

## LeaseInit Contract

```mermaid
flowchart TD
    leaseinit["LeaseInit"] --> network["flush + assign guest IP, route, DNS, neighbor table"]
    leaseinit --> mounts["mount prepared filesystem devices"]
    leaseinit --> overlays["apply read-only toolchain overlays"]
    leaseinit --> clock["sync wall clock from host"]
    leaseinit --> local["start vm-bridge local control"]
    network --> result["LeaseInitResult"]
    mounts --> result
    overlays --> result
    clock --> result
    local --> result
    result --> workload["customer workload may start"]
```

## Cache Key

```mermaid
flowchart TD
    key["snapshot key hash"] --> version["fc-precontrol-v1"]
    key --> disk["prepared disk layout key"]
    key --> abi["Firecracker runtime ABI key"]
    key --> network["network-overrides+lease-init-v1"]

    disk --> root["root substrate image + root size"]
    disk --> drives["ordered drive IDs + source refs"]
    disk --> mounts["mount paths + bind paths + fs type + read-only flags"]

    abi --> fc["Firecracker version"]
    abi --> kernel["kernel content + command line"]
    abi --> bridge["vm-bridge/vmproto version"]
    abi --> shape["vCPU count + memory size + hook version"]

    network --> override["LoadSnapshot.network_overrides binds eth0 to fresh TAP"]
    network --> leaseinit["LeaseInit applies guest IP, gateway, DNS"]
```

| Excluded from snapshot key | Owner |
| --- | --- |
| GitHub workflow semantics | sandbox-rental job-shape model |
| Matrix semantics | sandbox-rental job-shape model |
| Durable zvol generation choice | Prepared disk layout key after source resolution |
| TAP name, slot index, guest IP, gateway | Per-lease runtime state |

## Deploy Ownership

```mermaid
flowchart LR
    bazel["Bazel substrate bundle"] --> prestart["prestart: stage-guest-images"]
    prestart --> staged["/var/lib/verself/guest-images/..."]
    staged --> daemon["vm-orchestrator daemon"]
    staged --> seed["poststart: seed-catalog"]
    seed --> image["images/<ref>@ready + vs:source_digest"]
    image --> ensure["EnsureOrgRuntime receives org-local encrypted copy"]
    ensure --> lease["lease root clone"]
```

| Artifact | Owner |
| --- | --- |
| daemon binary | `vm-orchestrator` Nomad component |
| substrate image with vm-bridge | `vm-orchestrator` Nomad component |
| staged guest images | prestart `stage-guest-images` task |
| ZFS image zvols | poststart `seed-catalog` task |

## Post-Workload State

```mermaid
flowchart TD
    success["successful workload"] --> commit["CommitFilesystemMount"]
    commit --> seal["vm-bridge seals writable filesystem"]
    seal --> flush["host flushes block device"]
    flush --> generation["ZFS immutable durable generation"]
    generation --> future_key["future disk layout keys"]

    success -. forbidden .-> post_snapshot["reusable snapshot from post-workload VM"]
    generation --> refresh["trusted cache refresh"]
    refresh --> precontrol["fresh pre-control artifact"]
```
