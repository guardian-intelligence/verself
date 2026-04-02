# homestead-smelter

`homestead-smelter` is the new Zig workspace for Firecracker-specific guest and host agents.

 guest agents -> homestead-smelter-host
                            -> Effect live stream
                            -> ClickHouse batch writer

Current protocol shape:

- `homestead-smelter-guest` listens on a dedicated vsock port inside the guest
- each guest connection sends one fixed-size `hello` frame, then one fixed-size `sample` frame every `500ms`
- `homestead-smelter-host serve` runs as a long-lived daemon on the bare-metal worker
- `homestead-smelter-host ping` verifies the daemon over a local Unix socket
- `homestead-smelter-host snapshot` returns the current in-memory view of live Firecracker guests as JSON
- the host daemon owns the long-lived guest streams and is the collection point for VM telemetry

## Bridge Startup Policy

- discovery treats `root/run/forge-control.sock` as VM presence, not bridge readiness
- each discovered VM gets one long-lived worker thread owned by the discovery loop; completed workers are joined explicitly before respawn
- ordinary connect and stream errors are recorded in `last_error` and retried with a fixed `200ms` backoff until the VM disappears or a connection succeeds

The existing Go control plane stays in place on port `10789`. `homestead-smelter` uses port `10790`.

The bare-metal host agent is now deployed by the Firecracker Ansible role as a standalone binary at `/usr/local/bin/homestead-smelter-host`. It is not packaged into the Nix server profile.

## Build

```bash
cd homestead-smelter
zig build -Doptimize=ReleaseSafe
```

Artifacts land in `homestead-smelter/zig-out/bin/`.

## Run Against a Firecracker VM

The guest binary is a required part of the Alpine rootfs. `make guest-rootfs` now requires `zig` and uploads a prebuilt `homestead-smelter-guest` so the VM image always contains it. `forgevm-init` still auto-starts it on boot while the guest cutover is in progress.

Run the host daemon locally:

```bash
homestead-smelter/zig-out/bin/homestead-smelter-host serve \
  --listen-uds /tmp/homestead-smelter.sock \
  --jailer-root /srv/jailer/firecracker
```

In another shell, verify it:

```bash
homestead-smelter/zig-out/bin/homestead-smelter-host ping \
  --control-uds /tmp/homestead-smelter.sock
```

Expected output:

```text
PONG homestead-smelter-host
```

Ask the daemon for its current guest view:

```bash
homestead-smelter/zig-out/bin/homestead-smelter-host snapshot \
  --control-uds /tmp/homestead-smelter.sock
```

Expected output shape:

```json
{"schema_version":4,"jailer_root":"/srv/jailer/firecracker","observed_at_unix_ms":0,"vms":[]}
```

Once a VM is running, `snapshot` includes the latest `hello` metadata and the most recent fixed-size `sample` frame per VM. A typical VM entry looks like this:

```bash
{
  "job_id": "<job-id>",
  "worker_active": true,
  "connected": true,
  "last_update_unix_ms": 1710000000000,
  "last_error": null,
  "hello": {
    "mono_ns": 1000000,
    "wall_ns": 1710000000000000000,
    "boot_id": "...",
    "net_iface": "eth0",
    "block_dev": "vda"
  },
  "sample": {
    "seq": 12,
    "cpu_user_ticks": 1234,
    "mem_total_kb": 516096,
    "mem_available_kb": 401232
  }
}
```

The host daemon discovers guests from the Firecracker jail tree, opens the existing Unix-domain vsock bridge once per VM, then continuously reads exact `128`-byte frames. Another local process can poll `snapshot` or attach to a future RPC/feed server without talking to guest bridges directly.

Read @homestead-smelter/docs/TIGER_STYLE.md for coding guidance.
