# homestead-smelter

`homestead-smelter` is the new Zig workspace for Firecracker-specific guest and host agents.

 guest agents -> homestead-smelter-host
                            -> Effect live stream
                            -> ClickHouse batch writer

Current protocol shape:

- `homestead-smelter-guest` listens on a dedicated vsock port inside the guest
- each guest connection sends one fixed-size `hello` frame, then one fixed-size `sample` frame at a fixed `60Hz`
- `homestead-smelter-host serve` runs as a long-lived daemon on the bare-metal worker
- `homestead-smelter-host snapshot` attaches to the host socket, decodes the current binary host view into human-readable lines, and exits at `SNAPSHOT_END`
- `homestead-smelter-host check-live` succeeds when a given job UUID has both hello and sample telemetry
- the host daemon owns the long-lived guest streams and is the collection point for VM telemetry

## Host Runtime

- `host_core.zig` is the only fleet-state implementation
- `host.zig` is a thin Linux runtime shell around `host_core`
- one `epoll` loop owns the control listener, timerfd, guest bridge sockets, and attached consumers
- discovery treats `root/run/forge-control.sock` as VM presence, not bridge readiness
- ordinary connect and stream errors are recorded as typed disconnect reasons and retried with a fixed `200ms` backoff until the VM disappears or a connection succeeds

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

In another shell, verify it and inspect its current guest view:

```bash
homestead-smelter/zig-out/bin/homestead-smelter-host snapshot \
  --control-uds /tmp/homestead-smelter.sock
```

Expected output shape when no VMs are live:

```text
SNAPSHOT_END host_seq=1
```

Once a VM is running, `snapshot` prints the latest `hello` metadata and most recent `sample` frame per live VM. A typical output looks like this:

```text
HELLO job_id=<job-id> stream_generation=1 host_seq=17 guest_seq=0 boot_id=<boot-id> mem_total_kb=516096
SAMPLE job_id=<job-id> stream_generation=1 host_seq=18 guest_seq=94 mem_available_kb=401232 cpu_user_ticks=1234
SNAPSHOT_END host_seq=19
```

The host daemon discovers guests from the Firecracker jail tree, opens the existing Unix-domain vsock bridge once per VM, then continuously reads exact `128`-byte frames. The host control socket is `AF_UNIX` `SOCK_SEQPACKET`; another local process can attach once, receive a snapshot replay, then keep tailing the same binary stream without talking to guest bridges directly.

Check whether a specific VM is live:

```bash
homestead-smelter/zig-out/bin/homestead-smelter-host check-live \
  --control-uds /tmp/homestead-smelter.sock \
  --job-id 00000000-0000-0000-0000-000000000001
```

The wire contract is documented in [docs/protocol.md](docs/protocol.md).

Read @homestead-smelter/docs/zig-coding/STYLE.md for coding guidance.
