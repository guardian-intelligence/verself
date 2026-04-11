# Agent Workspace VMs

QEMU/KVM virtual machines on ZFS zvols, purpose-built for AI coding agents that operate as developers. Each workspace is a persistent Ubuntu 24.04 desktop environment with browser, dev tools, screen recording, and SSH access. Agents clone repos via ZFS (sub-second with warm deps), write code, record their screen, push PRs with video attachments to Forgejo, and iterate on review comments.

Same infrastructure is sellable through billing-service — reserve credits on workspace creation, meter CPU/memory/storage, settle on destroy, void on crash.

## Why QEMU, Not Firecracker

Firecracker is purpose-built for ephemeral compute: no display device, no PCI passthrough, no `savevm`/`loadvm` for checkpoint/resume. Agent workspaces need a full desktop (video recording, browser-based UI testing), persistent state across sessions, and the ability to checkpoint/fork a running environment.

QEMU provides: VNC/SPICE display output, `savevm`/`loadvm` for full VM state persistence (memory + device state stored directly in the backing block device), virtio-serial for host-guest agent communication, and the standard QEMU Machine Protocol (QMP) for programmatic control.

## Storage: ZFS zvols as Raw Block Devices

OpenComputer uses qcow2 overlays on XFS with reflink. We skip qcow2 entirely — QEMU reads the zvol block device directly via `/dev/zvol/forgepool/agent-vms/{id}`. No overlay management, no format conversion.

```
forgepool/
├── agent-golden-zvol           # 16GB zvol, Ubuntu 24.04 + desktop + all tooling
│   └── @ready                  # Immutable snapshot, clone source
├── agent-vms/                  # Dataset parent for per-workspace clones
│   ├── ws-{uuid-1}            # Clone of agent-golden-zvol@ready
│   └── ws-{uuid-2}            # Each starts at 0 bytes, grows with COW writes
├── repos/                      # Pre-warmed repository datasets
│   ├── forge-metal@warmed     # Snapshot after clone + npm install + go mod download
│   └── forge-metal-{uuid-1}  # Per-workspace clone, mounted as /workspace in guest
```

Provisioning a workspace: `zfs clone forgepool/agent-golden-zvol@ready forgepool/agent-vms/ws-{id}` + `zfs clone forgepool/repos/forge-metal@warmed forgepool/repos/forge-metal-{id}`. Both are instant, copy-on-write, zero additional space until the agent starts writing.

Checkpoint: `zfs snapshot forgepool/agent-vms/ws-{id}@checkpoint-{ts}` after QEMU `savevm`. Fork: `zfs clone` from checkpoint snapshot → new VM with identical state.

## Golden Image

Ubuntu 24.04 (matching host OS), built by `scripts/build-agent-workspace-rootfs.sh` and baked into a ZFS zvol by the `agent_workspace` Ansible role using the same staging→snapshot→promote pattern as the Firecracker golden.

Contents:

```
Ubuntu 24.04 server (debootstrap)
├── Display: Xvfb + Xfce4 (lightweight, ~200MB)
├── VNC: TigerVNC server (systemd unit, auto-start)
├── Browser: Chromium (Playwright-compatible, with display and headless)
├── Screen recording: ffmpeg (x11grab at 10-24 FPS, H.264 ultrafast)
├── Dev tools
│   ├── Node.js 22 LTS + npm + corepack
│   ├── Go 1.24+
│   ├── Python 3.12 + uv
│   ├── Zig 0.14
│   └── Rust (rustup, stable)
├── Agent tools
│   ├── Claude Code CLI (@anthropic-ai/claude-code)
│   ├── git + gh CLI (configured for Forgejo instance)
│   ├── Playwright (Chromium driver pre-installed)
│   └── jq, ripgrep, fd, fzf, tmux
├── System
│   ├── OpenSSH server (host keys pre-generated, authorized_keys from credstore)
│   ├── systemd (full init, manages VNC + agent + recording)
│   ├── workspace-agent binary (Go, gRPC over virtio-serial)
│   └── ntp/chrony (time sync for trace correlation)
└── Size target: ~8GB base zvol (larger than Firecracker's 4GB — full desktop)
```

## QEMU Configuration

```bash
qemu-system-x86_64 \
  -machine q35,accel=kvm \
  -cpu host \
  -smp 4 -m 8192 \
  -drive file=/dev/zvol/forgepool/agent-vms/{id},format=raw,if=virtio,cache=none \
  -drive file=/dev/zvol/forgepool/repos/{repo}-{id},format=raw,if=virtio,cache=none \
  -netdev tap,id=net0,ifname=qm-tap-{slot},script=no,downscript=no \
  -device virtio-net-pci,netdev=net0,mac={mac} \
  -vnc unix:/run/agent-workspace/{id}/vnc.sock \
  -chardev socket,id=qmp,path=/run/agent-workspace/{id}/qmp.sock,server=on,wait=off \
  -mon chardev=qmp,mode=control \
  -chardev socket,id=agent,path=/run/agent-workspace/{id}/agent.sock,server=on,wait=off \
  -device virtio-serial-pci \
  -device virtconsole,chardev=agent \
  -display none \
  -daemonize \
  -pidfile /run/agent-workspace/{id}/qemu.pid
```

Notable deviations from default QEMU:
- `cache=none` on virtio-blk: ZFS handles caching via ARC, double-caching wastes RAM
- VNC on Unix socket (not TCP): Caddy/noVNC proxy handles TLS + auth externally
- QMP on Unix socket: host-side orchestrator sends control commands (pause, savevm, loadvm, quit)
- virtio-serial + virtconsole: workspace-agent inside guest connects here for structured communication with host

## Networking

Reuses `NetworkPoolConfig`, `NetworkLease`, and `Allocator` from vm-orchestrator with minor adaptations:

| | Firecracker | Agent Workspace |
|---|---|---|
| TAP prefix | `fc-tap-` | `qm-tap-` |
| Pool CIDR | `172.16.0.0/16` | `172.17.0.0/16` (separate pool, no collisions) |
| Lease dir | `/var/lib/forge-metal/guest-artifacts/net/leases` | `/var/lib/agent-workspace/net/leases` |
| NAT | Same masquerade pattern | Same masquerade pattern |

Guests get a /30 subnet. Host is .1, guest is .2. SSH from host to guest: `ssh agent@172.17.{slot}.2`. NAT for outbound (apt, npm, git push).

## Agent Interaction

### SSH Path (Primary)

The main interaction model — identical to how a human dev works:

```
Operator's machine
  └─ ssh ubuntu@64.34.84.75          # SSH to bare metal host
       └─ ssh agent@172.17.0.2       # SSH to workspace VM
            └─ claude -p "fix the auth bug"   # or interactive claude session
```

Inside the VM, Claude Code has full access: filesystem, terminal, browser (via Playwright), git. It works exactly like it does on a developer's laptop, but with pre-installed tooling and a pre-cloned repo with all deps warm.

### Programmatic Path (Automation)

For service-triggered or webhook-triggered agent runs:

```
agent-workspace-service API
  └─ POST /workspaces/{id}/prompt  { "prompt": "fix issue #42" }
       └─ host sends prompt to workspace-agent via virtio-serial
            └─ workspace-agent runs: claude -p --output-format json "{prompt}"
                 └─ streams structured events back to host
                      └─ host writes events to ClickHouse + updates API response
```

### Video Recording

Recording starts automatically when an agent session begins:

```bash
# Inside VM, managed by workspace-agent
ffmpeg -f x11grab -framerate 15 -video_size 1920x1080 \
  -i :1 -c:v libx264 -preset ultrafast -crf 28 \
  -f mp4 /workspace/recording-{session-id}.mp4
```

On session completion, the workspace-agent:
1. Stops ffmpeg (SIGTERM → flush)
2. Uploads video to Forgejo via API as an attachment
3. Includes video link in PR body/comment markdown: `![Demo](attachment:{uuid})`

Recording at 15 FPS with ultrafast preset and CRF 28: ~2-5 MB/minute for a desktop session. A 30-minute agent session produces ~60-150MB of video.

## Checkpoint / Resume

```
Checkpoint:
  1. workspace-agent: sync; blockdev --flushbufs
  2. QMP: savevm "checkpoint-{ts}" → saves CPU + memory + device state into zvol
  3. ZFS: snapshot forgepool/agent-vms/{id}@checkpoint-{ts}
  4. (ZFS snapshot captures both the disk state and the embedded QEMU savevm state)
  5. billing: settle metered usage through checkpoint

Resume:
  1. QEMU: start VM from zvol (which contains the savevm state)
  2. QMP: loadvm "checkpoint-{ts}" → restores full VM state
  3. workspace-agent reconnects via virtio-serial
  4. billing: new reservation

Fork (branch agent state):
  1. zfs clone forgepool/agent-vms/{id}@checkpoint-{ts} → forgepool/agent-vms/{new-id}
  2. Start new QEMU instance from the clone
  3. QMP: loadvm "checkpoint-{ts}" on the new instance
  4. Two independent VMs, identical starting state, diverge from here
```

## Billing

```
Create workspace   → billing.Reserve(base_hours * hourly_rate)
Every 5 min        → billing.Renew(reservation_id, next_window)
Checkpoint/Destroy → billing.Settle(actual_cpu_seconds, memory_gb_seconds, storage_gb_hours)
Agent crash        → billing.Void(reservation_id)  // customer never overpays
```

Metering dimensions written to ClickHouse: `cpu_seconds`, `memory_gb_seconds`, `storage_gb_hours`, `egress_bytes`, `recording_minutes`.

## Service API

`agent-workspace-service` (Go/Huma), same patterns as billing-service:

```
POST   /api/v1/workspaces                    Create workspace (repo, branch, prompt)
GET    /api/v1/workspaces/{id}               Status, timing, billing
GET    /api/v1/workspaces/{id}/vnc           WebSocket proxy to VNC
POST   /api/v1/workspaces/{id}/prompt        Send prompt to running agent
POST   /api/v1/workspaces/{id}/checkpoint    Checkpoint VM state
POST   /api/v1/workspaces/{id}/resume        Resume from checkpoint
POST   /api/v1/workspaces/{id}/fork          Fork from checkpoint
DELETE /api/v1/workspaces/{id}               Destroy workspace
GET    /api/v1/workspaces/{id}/recordings    List recorded videos
GET    /api/v1/workspaces/{id}/events        SSE stream of agent events
```

Auth: Zitadel JWT via auth-middleware (same as all Go services).

## Implementation Phases

### Phase 1: QEMU Orchestrator + Golden Image

**Library**: `src/agent-workspace/` Go module
- `QemuOrchestrator`: create/start/stop/destroy VMs from ZFS zvols
- `QMPClient`: JSON-over-Unix-socket client for QEMU Machine Protocol
- Reuse `NetworkAllocator` from vm-orchestrator (parameterized by pool CIDR + lease dir)
- `PrivOps` implementation for QEMU operations (ZFS clone/destroy reuse, QEMU start/stop new)

**Golden image**: `scripts/build-agent-workspace-rootfs.sh`
- debootstrap Ubuntu 24.04 → ext4 image
- Install Xfce4, TigerVNC, Chromium, dev tools, Claude Code
- Install workspace-agent binary + systemd units
- Produce SBOM + artifact manifest (same pattern as Firecracker rootfs builder)

**Ansible role**: `roles/agent_workspace/`
- QEMU binary install (from server-tools.json)
- Golden zvol creation + atomic refresh
- Network setup (TAP pool, nftables masquerade)
- systemd service for agent-workspace host daemon

**Verification**: Boot a VM, SSH in, run `claude --version`, destroy it.

### Phase 2: Video Recording + Forgejo Integration

- Screen recording daemon inside guest (ffmpeg x11grab, controlled by workspace-agent)
- Forgejo API integration: upload video as attachment, create PR with video embed
- Agent SDLC loop: prompt → code → test → record → PR → wait for review → address comments → merge
- noVNC proxy through Caddy for human observation

### Phase 3: Service + Billing

- `agent-workspace-service` HTTP API
- Billing integration (reserve/settle/void lifecycle)
- ClickHouse workspace events table
- Checkpoint/resume/fork via QMP + ZFS

### Phase 4: Automation Triggers

- Forgejo webhook: new issue → create workspace → agent fixes it
- Forgejo webhook: PR review comment → resume workspace → agent addresses feedback
- Cron: scheduled code maintenance (dependency updates, security patches)
- API: external trigger for custom automation flows

## Project Structure

```
src/agent-workspace/                    # Go library — QEMU VM orchestration
├── go.mod                              # github.com/forge-metal/agent-workspace
├── orchestrator.go                     # QemuOrchestrator: lifecycle management
├── qemu.go                             # QEMU process + QMP client
├── qmp.go                              # QMP protocol (JSON over Unix socket)
├── storage.go                          # ZFS zvol operations for QEMU VMs
├── network.go                          # Bridge to vm-orchestrator NetworkAllocator
├── privops.go                          # QemuPrivOps interface implementation
├── workspace.go                        # Workspace state machine (creating → running → checkpointed → destroyed)
└── cmd/workspace-agent/                # Guest agent binary
    └── main.go                         # gRPC over virtio-serial, manages Claude Code + ffmpeg

src/agent-workspace-service/            # HTTP API service
├── go.mod                              # imports: agent-workspace, auth-middleware
├── cmd/agent-workspace-service/
│   └── main.go                         # systemd LoadCredential, service bootstrap
├── routes.go                           # Huma API registration
├── workspaces.go                       # Service layer: billing + orchestrator
└── migrations/                         # PostgreSQL schemas for workspace state

src/platform/
├── ansible/roles/agent_workspace/      # Ansible role for host setup
├── scripts/build-agent-workspace-rootfs.sh  # Golden image builder
└── server-tools.json                   # + QEMU version pin
```
