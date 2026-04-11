# Agent Workspace VMs

This is a future runtime provider, not a separate checkpoint architecture.

The shared model lives in:

- `src/sandbox-rental-service/internal/jobs/`
- `src/sandbox-rental-service/migrations/`
- `src/vm-orchestrator/`
- `src/vm-orchestrator/vmproto/`
- `src/vm-guest-telemetry/`

Firecracker is the current provider for snapshot-backed VM segments. Full Ubuntu
agent workspaces can add a future provider when desktop devices, long-running
sessions, or machine-state checkpoints are needed. They should reuse the same
org-scoped `checkpoint_refs`, immutable `checkpoint_versions`,
`execution_segments`, billing windows, and telemetry concepts.

Do not fork this into a QEMU-specific product state machine. Provider-specific
code may implement machine checkpoints later, but disk checkpoints remain
host-owned ZFS zvol versions behind customer-facing checkpoint refs.
