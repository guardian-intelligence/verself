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
//  1. Accept a lease with immutable resource shape and a bounded deadline,
//     returning LeaseStateAcquiring once host-local state is durable.
//  2. Boot the lease actor asynchronously: clone the substrate zvol with ZFS
//     COW; clone any composed
//     toolchain image zvols (gh-actions-runner, etc.) the runner_class
//     requests, mounted read-only at the configured guest paths.
//  3. Allocate a /30 TAP slot for the lease and create the Firecracker jail.
//  4. Activate Firecracker by restoring a pre-control snapshot when one matches
//     the prepared zvol layout and runtime ABI, otherwise cold boot and publish
//     a reusable pre-control snapshot for later leases.
//  5. Initialize vm-bridge over a deterministic vsock control stream; LeaseInit
//     applies per-lease network state, filesystem mounts, and wall clock.
//  6. Mark the lease ready; StartExec waits for LeaseStateReady before sending
//     guest work and then enforces the lease runtime concurrency cap.
//  7. Stream guest logs, filesystem lifecycle events, and telemetry as host facts.
//  8. Release or expire the lease, killing in-flight execs and cleaning host
//     resources exactly once.
//
// Product policy and workload admission are not host concepts. The host emits
// observed facts; product services interpret them.
package vmorchestrator
