# VM Execution Control Plane

sandbox-rental-service owns customer execution semantics: org policy, workflow
planning, checkpoint refs, billing windows, logs, and public API DTOs. The
privileged VM/ZFS boundary stays in `src/vm-orchestrator/`.

Code pointers:

- `internal/jobs/` - execution, segment, billing, and checkpoint state machines.
- `internal/api/` - secured Huma routes and IAM operation catalog.
- `migrations/` - PostgreSQL tables for executions, segments, checkpoint refs,
  checkpoint versions, save requests, logs, and billing windows.
- `docs/durable-execution-workflow-plan.md` - phased rewrite plan for durable
  River-backed execution workflow, reconciliation, pagination, and evidence gates.
- `../../vm-orchestrator/proto/v1/` - host daemon gRPC API consumed by this service.
- `../../apiwire/sandbox.go` - frontend/service wire DTOs.
- `../openapi/` - generated OpenAPI contracts.

Target model:

- `executions` are customer-visible runs.
- `execution_attempts` are retries and reconciliation units.
- `execution_segments` are VM boots.
- `checkpoint_refs` are mutable, org-scoped names like `pg-demo`.
- `checkpoint_versions` are immutable disk or future machine checkpoint assets.
- `checkpoint_save_requests` are guest-initiated, host-authorized save attempts.

The guest may request `vm-bridge snapshot save <ref>`, but vm-orchestrator performs every
ZFS operation, accepts saves only for host-authorized refs, and never accepts
guest-provided ZFS paths. Direct shell execution and Forgejo workflow execution
should compile into the same segment/checkpoint state model.
