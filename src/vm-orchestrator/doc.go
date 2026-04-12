// Package vmorchestrator manages Firecracker microVM lifecycle for sandboxed runs.
//
// It provides a single entry point: [Orchestrator.Run], which takes a
// [RunSpec] and returns a [RunResult]. The full lifecycle is:
//
//  1. Clone golden zvol (ZFS COW, ~1.7ms kernel)
//  2. Set up jail (mknod zvol device, copy kernel)
//  3. Allocate a guest /30, create a TAP device, attach static networking
//  4. Start jailer (Firecracker in chroot with namespaces)
//  5. Configure VM via REST API over Unix socket, including network, vsock, and entropy
//  6. Boot VM, establish the host-initiated vsock control stream, and stream guest logs
//  7. Wait for a result frame, collect metrics, and retain serial as diagnostics only
//  8. Cleanup (destroy clone, remove jail, remove TAP, release lease)
//
// # Design decisions
//
//   - Firecracker REST API directly, not the Go SDK. A thin HTTP client over
//     the Unix socket covers the small set of endpoints we need.
//   - Jailer from day one. The jailer provides chroot, PID namespace,
//     and device isolation. Retrofitting it later is harder.
//   - Zvol, not dataset. Firecracker takes block devices. A zvol is a
//     ZFS block device — clone/destroy/written work identically.
//   - Shell out to zfs/ip/mknod CLI, matching zfsharness conventions.
//   - LIFO cleanup on any error, matching Clone.Release pattern.
//   - The per-run runtime control plane is vsock only. MMDS is not part of the
//     steady-state execution path, and serial is not authoritative.
package vmorchestrator
