# Historical Firecracker Improvements Note

This document predates the Firecracker v1.15.0 vsock cutover. It is retained
only as a brief historical pointer and should not be treated as the current
runtime design.

The following ideas from the older report are now superseded:

- MMDS as the per-job runtime config channel
- serial parsing as the authoritative guest result path
- per-job guest network state encoded in kernel boot arguments

Current references:

- Current runtime contract: [`internal/firecracker/doc.go`](../internal/firecracker/doc.go)
- Shared control protocol: [`internal/vmproto/protocol.go`](../internal/vmproto/protocol.go)

The remaining major follow-on from that report is snapshot/restore. That phase
should extend the existing vsock control plane and pinned guest artifact model;
it should not reintroduce MMDS or serial control semantics.
