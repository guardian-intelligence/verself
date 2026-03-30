# Research Corrections and Design Decisions

> Corrections to prior research, resolution of open questions, and concrete design decisions.
> Triggered by gap analysis review of the full research corpus.
>
> Researched 2026-03-29.

## Critical corrections

### 1. Go SDK is 10+ API versions behind -- use hybrid approach

The Firecracker Go SDK (`firecracker-go-sdk`) is **functionally stale**.

| Property | Value |
|----------|-------|
| SDK version | v1.0.0 (released 2022-09-07, **3.5 years ago**) |
| Swagger API target | **v1.4.1** |
| Current Firecracker | **v1.15.0** |
| Last human commit | 2025-12-24 (dep bump, not feature work) |
| Core maintainers active | No -- only community maintenance |
| Release planned | No -- [issue #590](https://github.com/firecracker-microvm/firecracker-go-sdk/issues/590) unanswered |

**Missing features in the SDK:**
- `network_overrides` on snapshot load (v1.12+)
- virtio-mem hotplug (v1.14+)
- virtio-pmem (v1.14+)
- Balloon free_page_reporting/hinting (v1.14+)
- Serial output path (v1.14+)

**What still works:** PCI transport is a CLI flag (`--enable-pci`), passable via
`VMCommandBuilder.AddArgs()`. VMClock is automatic (no API call). The SDK's process
lifecycle management (jailer, socket, CNI, signal handling, cleanup) is solid.

**Decision: hybrid approach.** Use the SDK for process lifecycle (jailer, socket
management, CNI networking, signal handling, graceful shutdown). Use a thin HTTP
client over the Unix socket for any API endpoints the SDK doesn't cover:

```go
// SDK manages lifecycle
m, _ := firecracker.NewMachine(ctx, cfg)
m.Start(ctx)

// Supplemental HTTP client for missing endpoints
fc := &http.Client{Transport: &http.Transport{
    DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
        return net.Dial("unix", socketPath)
    },
}}
fc.Post("http://localhost/memory-hotplug", "application/json", body)
```

This is the approach recommended by SDK users in
[PR #694](https://github.com/firecracker-microvm/firecracker-go-sdk/pull/694)
comments ("raw HTTP handler injection").

Prior doc [go-sdk.md](go-sdk.md) is still accurate for what the SDK provides -- it
just omits what the SDK *doesn't* provide.

### 2. OpenBao is unreachable from inside the VM -- unwrap on host side

The init-and-lifecycle.md doc recommended "guest calls OpenBao API to unwrap the
token." This is wrong. OpenBao binds to `127.0.0.1:8200` on the host. The guest
is in a network namespace with NAT to the internet -- it cannot reach host localhost.

**Options analyzed:**

| Approach | How | Security | Complexity |
|----------|-----|----------|------------|
| Route to host TAP gateway | Guest reaches `172.16.0.1:8200` | OpenBao listens on non-localhost, wider attack surface | Low |
| Unwrap on host, push secrets via vsock | Host unwraps, sends plaintext secrets over vsock | Secrets in host memory, but vsock is kernel-to-kernel | Low |
| Run OpenBao Agent inside VM | Agent connects to host via TAP gateway | Same as option 1, plus agent overhead | High |

**Decision: unwrap on host side, push secrets via vsock.**

The orchestrator (running on the host) unwraps the wrapping token itself, then pushes
the plaintext secrets over vsock as part of the init handshake. This is the simplest
approach and avoids exposing OpenBao to the VM network.

```
1. Orchestrator: bao write -wrap-ttl=120s -f auth/approle/role/ci/secret-id
2. Orchestrator: unwrap → get actual secrets (DB_PASSWORD, API_KEY, etc.)
3. Orchestrator: boot Firecracker VM
4. Orchestrator → Guest (vsock): Init{secrets: {DB_PASSWORD: "...", ...}, job_id, ...}
5. Guest init: inject secrets as env vars into CI process
```

**Trust model change:** Secrets are now plaintext in the orchestrator's memory (they
always were during the unwrap call). The vsock channel is kernel-to-kernel with no
network exposure. The guest never needs to contact OpenBao at all.

This is simpler than the prior recommendation and eliminates the reachability problem.

### 3. Forgejo runner integration: implement RunnerService protocol directly

The prior research left this undefined. After analyzing act_runner's internals:

**act_runner architecture:**
- Polls Forgejo via Connect RPC (`FetchTask` every 2s)
- Delegates to `nektos/act` for workflow execution
- Two backends: Docker (create container) and Host (exec on host)
- Forgejo fork adds LXC (wrap exec with `lxc-attach`)

**The RunnerService protocol is simple** -- 5 RPCs:

| RPC | Purpose |
|-----|---------|
| `Register` | One-time registration (name, token, labels) |
| `Declare` | Declare capabilities at startup |
| `FetchTask` | Poll for work (returns one job's YAML + context + secrets) |
| `UpdateTask` | Report step state transitions |
| `UpdateLog` | Stream log lines back |

**Decision: custom Go program implementing RunnerService directly.**

```
Forgejo server
    |  (Connect RPC, protobuf)
forge-metal orchestrator
    |
    ├── FetchTask() every 2s
    |     └── Receives: Task{workflow_payload (YAML), context, secrets}
    |
    ├── On task:
    |     1. Parse YAML with act/model.ReadWorkflow()
    |     2. zfs clone pool/golden-zvol@ready pool/ci/job-{id}
    |     3. Boot Firecracker VM
    |     4. For each step: exec via vsock, stream logs
    |     5. UpdateTask + UpdateLog every 1s
    |     6. VM exits → zfs destroy → final UpdateTask
    |
    └── Go dependencies:
          code.gitea.io/actions-proto-go
          connectrpc.com/connect
          github.com/nektos/act/pkg/model  (YAML parsing only)
```

**Why not fork act_runner?** Adding a Firecracker executor to act_runner requires
forking both act_runner and `nektos/act`, maintaining the fork, and pulling in Docker
dependencies. The protocol is simple enough (~500 lines of Go for the poll-execute-report
loop) that implementing it directly is less maintenance burden.

**Why not run act_runner inside the VM?** (actuated's approach for GitHub Actions)
This works for GitHub because the official runner binary exists. Forgejo uses a different
protocol -- you'd need act_runner inside the VM, which means networking from the VM to
Forgejo, larger golden image, and polling latency from inside the VM.

**Forgejo pre-parses workflows.** The `Task.workflow_payload` contains the full YAML
filtered to exactly one job. The orchestrator parses this single-job YAML, evaluates
`if:` conditions, and executes `run:` steps. For `uses:` actions (like `actions/checkout`),
import `nektos/act` as a library for full compatibility.

**Secrets, artifacts, cache:** Secrets delivered in `Task.Secrets`. Artifacts use
Forgejo's API directly (runner sets `ACTIONS_RUNTIME_URL` env var). Cache is optional.

Source: [Gitea Actions design](https://docs.gitea.com/usage/actions/design),
[actions-proto-go](https://pkg.go.dev/code.gitea.io/actions-proto-go),
[Forgejo discussion #152](https://codeberg.org/forgejo/discussions/issues/152)

## Important corrections

### 4. Guest kernel configs: add CONFIG_PCI=y for mainline

Firecracker's configs **fail on mainline Linux 6.1** without `CONFIG_PCI=y`.

[Issue #4881](https://github.com/firecracker-microvm/firecracker/issues/4881):
kernel panic `VFS: Cannot open root device "vda"` because mainline requires PCI for
ACPI-based VirtIO device discovery. Amazon Linux's fork has patches that bypass this.

**Fix:** Add `CONFIG_PCI=y` to the config when building against mainline. This
increases vmlinux from ~29MB to ~38MB.

microvm.nix sidesteps this entirely by using the standard NixOS kernel (~100MB+
vmlinux, all CONFIG options enabled).

**Decision:** Start with Firecracker's `microvm-kernel-ci-x86_64-6.1.config` +
`CONFIG_PCI=y`, built against mainline `v6.1.x`. Fall back to standard NixOS kernel
if other issues surface.

### 5. zvol volblocksize: 16K is correct, not 4K

The prior research said 8K default is "fine" without analysis. The correct answer:

**4K volblocksize is actively harmful:**
- Compression effectively disabled (1.07x at 4K vs 1.94x at 16K on same data)
- Metadata overhead explodes (each 4K block needs ~4K of indirect block pointers)
- Sequential throughput degrades (60% improvement going from 8K to 64K)
- Proxmox community: "4K is never a good idea when using ZFS"

**npm install creates new files (no read-modify-write penalty):**
- ZFS allocates fresh blocks for new files -- no RMW needed
- RMW only applies to in-place modifications of existing data
- With compression at 16K, a 2K .js file compresses to ~800 bytes on disk

**16K is the production default** across Proxmox (since PVE 8.x), Incus, and TrueNAS.
OpenZFS changed the default from 8K to 16K in ZFS 2.2.

**Decision:** Use 16K volblocksize (ZFS 2.2+ default). Set explicitly on the golden zvol:
```bash
zfs create -V 4G -o volblocksize=16K pool/golden-zvol
```

Source: [OpenZFS #17677](https://github.com/openzfs/zfs/issues/17677),
[OpenZFS #14771](https://github.com/openzfs/zfs/issues/14771),
[Proxmox forums](https://discourse.practicalzfs.com/t/proxmoxs-volblocksize-default-is-16k-for-qemu-vm-disks-why/2438)

### 6. tc-redirect-tap: not in nixpkgs, but trivial to package

tc-redirect-tap is **not in nixpkgs**. It's a Go binary with a simple derivation:

```nix
buildGoModule {
  pname = "tc-redirect-tap";
  version = "unstable-2025-XX-XX";
  src = fetchFromGitHub { owner = "awslabs"; repo = "tc-redirect-tap"; ... };
  vendorHash = "sha256-XXXX";
  subPackages = [ "cmd/tc-redirect-tap" ];
}
```

**Alternative:** Embed TAP+TC logic directly in the Go orchestrator using
`vishvananda/netlink` (~500 lines). This eliminates the CNI dependency and gives
tighter control over the network lifecycle.

**Decision:** Start with tc-redirect-tap Nix derivation for simplicity. Consider
embedding later if CNI overhead becomes a concern.

## Minor items (verified)

### 7. cgroups v2: confirmed default on Ubuntu 22.04+

Ubuntu 22.04+ uses the cgroupv2 unified hierarchy by default. Verify on Latitude.sh:
```bash
stat -fc %T /sys/fs/cgroup/  # should print "cgroup2fs"
```

### 8. /proc/mounts scaling: acceptable for forge-metal

forge-metal's dataset count: golden zvol (1) + N clone zvols (mounted=no) + system
datasets (~5). ZFS zvol clones do not add mount points (they are block devices, not
mounted filesystems). Only mounted ZFS datasets appear in `/proc/mounts`.
The `canmount=off` mitigation is unnecessary for zvols -- they are never mounted.

### 9. forgevm-init: unbuilt, prototype needed

The vsock protocol design, SIGCHLD reaping, and credential flow are theoretical.
Next step is a working prototype. The minimal viable init needs:
1. Mount `/proc`, `/sys`, `/dev`, `/tmp`
2. vsock listener on port 10000
3. SIGCHLD reaper goroutine
4. Receive `Init` message with secrets + job config
5. `exec.Command` the CI step with injected env vars
6. Stream stdout/stderr back over vsock
7. Report exit code
8. `syscall.Reboot` to shut down VM

Estimated size: ~300-500 lines of Go.

## Sources

- [Firecracker Go SDK issues #590, #690, #694, #707](https://github.com/firecracker-microvm/firecracker-go-sdk/issues)
- [Firecracker kernel mainline issue #4881](https://github.com/firecracker-microvm/firecracker/issues/4881)
- [OpenZFS volblocksize #17677, #14771](https://github.com/openzfs/zfs/issues/17677)
- [Gitea Actions design](https://docs.gitea.com/usage/actions/design)
- [Forgejo discussion #152](https://codeberg.org/forgejo/discussions/issues/152)
- [actions-proto-go](https://pkg.go.dev/code.gitea.io/actions-proto-go)
- [actuated FAQ](https://docs.actuated.com/faq/)
