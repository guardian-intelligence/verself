# Agent Workspace VMs

This is a future runtime provider, not a separate golden environment architecture.

The shared model lives in:

- `src/services/sandbox-rental-service/internal/jobs/`
- `src/services/sandbox-rental-service/migrations/`
- `src/substrate/vm-orchestrator/`
- `src/substrate/vm-orchestrator/vmproto/`
- `src/substrate/vm-guest-telemetry/`

Firecracker is the current provider for snapshot-backed VM segments. Full Ubuntu
agent workspaces can add a future provider when desktop devices, long-running
sessions, or durable machine-state versions are needed. They should reuse the
same org-scoped golden refs, immutable generation records, execution segments,
billing windows, and telemetry concepts.

Do not fork this into a QEMU-specific product state machine. Provider-specific
code may implement machine-state versions later, but disk generations remain
host-owned ZFS zvol versions behind customer-facing golden refs.
