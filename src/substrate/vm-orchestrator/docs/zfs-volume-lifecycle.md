# ZFS Volume Lifecycle

vm-orchestrator owns privileged ZFS lifecycle for Firecracker leases. Product
services pass service-authorized refs over the Unix socket; guests never pass
ZFS dataset names, host paths, or mutation intents.

## Dataset Roots

Default roots under the configured pool:

| Root | Purpose |
| --- | --- |
| `images/` | Read-only composable toolchain images seeded by `SeedImage`. |
| `orgs/<org>/images/` | Org-encrypted received copies of platform images used as clone origins for that org. |
| `orgs/<org>/workloads/` | Ephemeral per-lease root disks and writable mount clones under the org quota. |
| `orgs/<org>/goldens/` | Immutable durable zvol generations committed after a seal-eligible successful execution under the org quota. |

```mermaid
flowchart TD
    bootstrap["daemon startup"] --> roots["EnsureRoots()"]
    roots --> image_root["images/"]
    roots --> org_root["orgs/"]

    capacity["runner capacity reconcile"] --> ensure["EnsureOrgRuntime(shape)"]
    ensure --> key["load org ZFS key"]
    ensure --> namespace["ensure org namespace + quota"]
    ensure --> org_images["materialize org-local image snapshots"]
    org_images --> resident["resident OrgRuntimeManager state"]

    acquire["AcquireLease"] --> require["RequireReady(shape)"]
    resident --> require
    require --> boot["clone lease zvols"]
    require -->|miss| fail["fail lease acquisition"]
```

## Customer Dataset Encryption

```mermaid
flowchart TD
    key_file["host-only org key file"] --> ensure["EnsureOrgRuntime"]
    ensure --> kernel_key["ZFS key loaded in kernel"]
    kernel_key --> org_root["orgs/<org>/ encryption root"]

    global_image["images/<ref>@ready"] -->|zfs receive under org key| org_image["orgs/<org>/images/<ref-digest>@ready"]
    org_root --> org_image
    org_root --> workloads["orgs/<org>/workloads/<lease>/..."]
    org_root --> goldens["orgs/<org>/goldens/<scope>/..."]

    org_image --> root_clone["lease root clone"]
    org_image --> toolchain_mount["toolchain mount clone"]
    goldens --> durable_mount["durable mount clone"]
```

| Boundary | Rule |
| --- | --- |
| Runtime key owner | vm-orchestrator only. |
| Product services and guests | Never receive raw keys or dataset names. |
| Org datasets | `mountpoint=none`, `canmount=off`; guests receive zvol block devices. |
| Lease cleanup | Releases daemon raw-key material; does not unload the kernel-held ZFS key. |
| Key unload / rotation | Security event, host drain/shutdown policy, or org tombstone. |

## Backup Exclusion

| Artifact | Classification |
| --- | --- |
| Customer zvol snapshots | Cache lifecycle artifact. |
| Customer zvol clones | Cache lifecycle artifact. |
| CI golden artifact loss | Cache miss and rebuild. |

```mermaid
flowchart LR
    zvol["customer zvol snapshots/clones"] --> cache["cache lifecycle only"]
    cache --> miss["loss = cache miss + rebuild"]
    zvol -. forbidden .-> backup["backup catalog / object upload / provider backup"]
```

## Lease Boot

```mermaid
sequenceDiagram
    participant SR as sandbox-rental
    participant ORM as OrgRuntimeManager
    participant VMO as vm-orchestrator
    participant ZFS as ZFS
    participant FC as Firecracker
    participant VB as vm-bridge

    SR->>SR: resolve runner class, quota, durable sources, golden VM manifest
    SR->>ORM: EnsureOrgRuntime(shape)
    ORM->>ZFS: load key, ensure namespace, materialize image refs
    ORM-->>SR: ready state resident

    SR->>VMO: AcquireLease(spec)
    VMO->>ORM: RequireReady(shape)
    ORM-->>VMO: org-local image snapshot refs
    VMO->>ZFS: clone root + mount zvols
    alt golden VM manifest hit
        VMO->>FC: LoadSnapshot after staged drive graph
        VMO->>VB: AfterRestore(filesystems, network, clock)
        VB-->>VMO: AfterRestoreResult
    else golden VM miss
        VMO->>FC: cold boot with static drive topology
        VMO->>VB: LeaseInit(filesystems, network, clock)
        VB-->>VMO: LeaseInitResult
    end
    VMO-->>SR: lease ready
```

| Mount case | Result |
| --- | --- |
| Missing durable generation | Cache miss; create fresh ext4 zvol. |
| Missing platform image | Lease failure. |
| Required mount failure | Lease failure. |
| Optional mount failure | Degraded cache state. |
| Post-boot block-device attach | Unsupported. |

## Commit

```mermaid
sequenceDiagram
    participant SR as sandbox-rental
    participant VMO as vm-orchestrator
    participant VB as vm-bridge
    participant FC as Firecracker
    participant ZFS as ZFS
    participant PG as Postgres

    SR->>SR: verify provider job success
    opt promotable golden VM checkpoint
        SR->>VMO: CheckpointGoldenVM(lease)
        VMO->>VB: BeforeGoldenSnapshot + guest sync
        VMO->>FC: pause microVM
        VMO->>ZFS: snapshot root + mount zvols while paused
        VMO->>FC: create vmstate/memory
        VMO-->>SR: golden VM artifact refs + zvol checkpoint refs
    end
    SR->>VMO: CommitFilesystemMount(mount)
    VMO->>VB: seal mount
    VMO->>ZFS: flush block device
    VMO->>ZFS: select checkpoint snapshot or snapshot lease mount clone
    VMO->>ZFS: clone to goldens/<scope>/generations/<generation>
    VMO->>ZFS: promote clone + create @sealed
    VMO-->>SR: sealed snapshot ref, used bytes, written bytes
    SR->>PG: record durable generation + golden VM manifest
    SR->>PG: CAS promote durable and golden VM pointers
```

| Promotion step | Owner |
| --- | --- |
| Golden VM checkpoint and Firecracker artifact creation | vm-orchestrator |
| Host seal and immutable zvol generation creation | vm-orchestrator |
| Operation, generation, and manifest metadata | sandbox-rental |
| Protected-branch current pointer CAS | sandbox-rental |

## Retention

```mermaid
flowchart TD
    sealed["@sealed generation"] --> cas{promotion CAS}
    sealed --> vm_manifest["golden VM manifest dependency"]
    cas -->|wins| current["current durable pointer"]
    vm_manifest --> vm_current["current golden VM pointer"]
    cas -->|loses| retained["unreferenced retained candidate"]
    retained --> retention["retention worker"]
    retention --> destroy["vm-orchestrator destroy by generation ref"]
```

Retention must preserve any durable generation referenced by
`durable_current_pointer` or `golden_vm_snapshot_generation`. Firecracker
vmstate and memory artifacts are retained through the golden VM manifest, not
through ZFS lineage.
