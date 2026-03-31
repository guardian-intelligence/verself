// Package firecracker manages Firecracker microVM lifecycle for CI jobs.
//
// It provides a single entry point: [Orchestrator.Run], which takes a
// [JobConfig] and returns a [JobResult]. The full lifecycle is:
//
//  1. Clone golden zvol (ZFS COW, ~1.7ms kernel)
//  2. Mount clone, write job config to /etc/ci/job.json, unmount
//  3. Set up jail (mknod zvol device, copy kernel)
//  4. Allocate a guest /30, create a TAP device, attach static networking
//  5. Start jailer (Firecracker in chroot with namespaces)
//  6. Configure VM via REST API over Unix socket
//  7. Boot VM, stream serial output
//  8. Wait for exit, collect metrics
//  9. Cleanup (destroy clone, remove jail, remove TAP, release lease)
//
// # Design decisions
//
//   - Firecracker REST API directly, not the Go SDK. The SDK (v1.0.0)
//     targets API v1.4.1; Firecracker in nixpkgs is v1.14.2. A thin
//     HTTP client over the Unix socket covers the 6 endpoints we need.
//   - Jailer from day one. The jailer provides chroot, PID namespace,
//     and device isolation. Retrofitting it later is harder.
//   - Zvol, not dataset. Firecracker takes block devices. A zvol is a
//     ZFS block device — clone/destroy/written work identically.
//   - Shell out to zfs/ip/mknod CLI, matching zfsharness conventions.
//   - LIFO cleanup on any error, matching Clone.Release pattern.
//   - PCI transport enabled (--enable-pci) for 20-50% I/O improvement.
package firecracker
