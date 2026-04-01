# homestead-smelter

`homestead-smelter` is the new Zig workspace for Firecracker-specific guest and host agents.

This first cut is intentionally small:

- `homestead-smelter-guest` listens on a dedicated vsock port inside the guest
- `homestead-smelter-host serve` runs as a long-lived daemon on the bare-metal worker
- `homestead-smelter-host ping` verifies the daemon over a local Unix socket
- `homestead-smelter-host probe-guest` connects through Firecracker's Unix-domain vsock bridge
- the host daemon is intended to become the collection point for VM telemetry

The existing Go control plane stays in place on port `10789`. The hello-world guest agent uses port `10790`.

## Build

```bash
cd homestead-smelter
zig build -Doptimize=ReleaseSafe
```

Artifacts land in `homestead-smelter/zig-out/bin/`.

## Run Against a Firecracker VM

The guest binary is installed into the Alpine rootfs when `make guest-rootfs` is run with `zig` available in `PATH`. `forgevm-init` auto-starts it on boot if `/usr/local/bin/homestead-smelter-guest` exists.

Run the host daemon locally:

```bash
homestead-smelter/zig-out/bin/homestead-smelter-host serve \
  --listen-uds /tmp/homestead-smelter.sock
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

Once a VM is running, point the guest probe at the jail's vsock bridge:

```bash
homestead-smelter/zig-out/bin/homestead-smelter-host \
  probe-guest \
  --uds-path /srv/jailer/firecracker/<job-id>/root/run/forge-control.sock \
  --port 10790 \
  --message "hello from host"
```

Expected output:

```text
hello from homestead-smelter guest on port 10790: received "hello from host"
```

## Scope

This is a bootstrap workspace, not the final telemetry design. The next steps are to replace the line-based hello payload with structured frames, add periodic health samples in the guest, and add ClickHouse / OTLP sinks on the host side.
