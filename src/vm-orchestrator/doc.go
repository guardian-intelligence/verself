// Package vmorchestrator manages run-centric Firecracker VM lifecycle.
//
// Orchestrator.Run takes a [RunSpec] and returns a [RunResult]. The run is
// owned by host state from submission through cleanup; the guest only sees a
// deterministic control protocol and a telemetry stream. The lifecycle is:
//
//  1. Clone the golden zvol with ZFS COW.
//  2. Set up the jail and place the Firecracker process inside it.
//  3. Allocate a guest /30, create a TAP device, and install host networking.
//  4. Start vm-bridge and configure the VM over the Firecracker REST API.
//  5. Establish the host-initiated vsock control stream and stream guest logs.
//  6. Enforce deterministic control framing:
//     - control envelopes are monotonic and non-zero by default
//     - `ack.for_type` must be `result`
//     - `ack.for_seq` must match the emitted `result` sequence
//     - zero-sequence frames are reserved for explicit fault injection only
//     - protocol violations are surfaced from the explicit wait states
//       `await_run_request`, `run_phase`, `await_result_ack`, and `await_shutdown`
//  7. Validate telemetry with hello-first and monotonic sequence checks.
//     Forward gaps produce a `gap` diagnostic; regressions produce a
//     `regression` diagnostic and are not emitted as run events.
//  8. Cleanup by destroying the clone, removing the jail, removing the TAP,
//     and releasing the lease.
//
// # Design decisions
//
//   - Firecracker REST API directly, not the Go SDK. A thin HTTP client over
//     the Unix socket covers the small set of endpoints we need.
//   - Jailer from day one. The jailer provides chroot, PID namespace,
//     and device isolation. Retrofitting it later is harder.
//   - Zvol, not dataset. Firecracker takes block devices. A zvol is a
//     ZFS block device, so clone/destroy/written semantics line up.
//   - Shell out to zfs/ip/mknod CLI, matching zfsharness conventions.
//   - LIFO cleanup on any error, matching Clone.Release pattern.
//   - The per-run runtime control plane is vsock only. MMDS is not part of the
//     steady-state execution path, and serial is not authoritative.
package vmorchestrator
