# homestead-smelter

`homestead-smelter` is the Zig workspace for Firecracker-specific guest and host agents.

```
guest agents ──vsock──▶ homestead-smelter-host ──AF_UNIX SEQPACKET──▶ consumers
```

Consumers attach to the host control socket. The host daemon does not expose JSON and does not
write to ClickHouse directly. Wire format details are in [docs/protocol.md](docs/protocol.md).

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

The guest binary is a required part of the Alpine rootfs. The `guest-rootfs.yml` playbook requires `zig` and uploads a prebuilt `homestead-smelter-guest` so the VM image always contains it. The current guest boot path starts it from `forgevm-init`.

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

Check whether a specific VM is live:

```bash
homestead-smelter/zig-out/bin/homestead-smelter-host check-live \
  --control-uds /tmp/homestead-smelter.sock \
  --job-id 00000000-0000-0000-0000-000000000001
```

## Cross-Language Conformance

`protocol/vectors.json` contains golden test vectors generated from the Zig reference encoder. Each vector pairs hex-encoded wire bytes with expected decoded field values. TypeScript or Go consumers validate their decoders against these vectors.

Regenerate after changing the binary protocol layout:

```bash
cd homestead-smelter
zig build run-generate-vectors > protocol/vectors.json
zig build test  # staleness test verifies the checked-in file matches
```

See [docs/protocol.md](docs/protocol.md) for the vector file format and conformance testing model.

Read @homestead-smelter/docs/zig-coding/STYLE.md for coding guidance.
