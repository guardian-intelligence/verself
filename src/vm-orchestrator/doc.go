// Package vmorchestrator manages lease-scoped Firecracker VM lifecycle.
//
// The daemon exposes a local V1 gRPC API over a Unix socket. A lease owns VM
// lifetime and capacity; execs are units of work attached to an existing lease.
// The host enforces absolute lease deadlines without a control-plane round trip
// and cleans up ZFS clones, jailer processes, TAP slots, and bridge sessions on
// every terminal path.
//
// The lifecycle is:
//
//  1. Acquire a lease with immutable resource shape and a bounded deadline.
//  2. Clone the substrate zvol with ZFS COW; clone any composed
//     toolchain image zvols (gh-actions-runner, etc.) the runner_class
//     requests, mounted read-only at the configured guest paths.
//  3. Allocate a /30 TAP slot for the lease and create the Firecracker jail.
//  4. Start Firecracker and initialize vm-bridge over a deterministic vsock
//     control stream.
//  5. Start one or more execs subject to the lease runtime concurrency cap.
//  6. Stream guest logs, checkpoint requests, and telemetry as host facts.
//  7. Release or expire the lease, killing in-flight execs and cleaning host
//     resources exactly once.
//
// Product policy and workload admission are not host concepts. The host emits
// observed facts; product services interpret them.
package vmorchestrator
