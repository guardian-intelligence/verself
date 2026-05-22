# deployment

The typed Go orchestrator for verself deploys. It owns the Bazel-to-Nomad
adapter layer: Nomad CLI job-run handoff and Bazel artifact resolution. Operator
database access is owned by
`src/tools/operator/cmd/aspect-operator` and the shared `src/tools/operator-runtime/go`
packages, not this deployment orchestrator.

## Layout

- `cmd/verself-deploy/` — single binary, subcommands grouped under
  `verself-deploy <group> <action>` (mirrors the `aspect <group> <action>`
  surface).
- `internal/identity/` — derives the verself deploy identity env and emits W3C
  baggage so every span this binary creates carries `verself.deploy_run_key`,
  `verself.deploy_id`, `verself.site`, `verself.author`.
- `internal/deploymodel/` — shared value types for Garage artifact delivery and
  resolved Nomad job-run payloads.
- `internal/nomadclient/` — typed wrapper around `github.com/hashicorp/nomad/api`.
  Keeps server-version-aligned HCL parsing and service-catalog reads in Go; job
  submission and rollout monitoring are delegated to the pinned Nomad CLI via
  `nomad job run -json`.

## Phase boundaries

This module owns deploy orchestration: Bazel component discovery, Garage
artifact publication, Nomad job-run handoff, and identity propagation. It emits
OpenTelemetry spans and stdout. ClickHouse is an observability backend, not a
runtime dependency of this binary. Host bootstrap and patching are outside this
binary.

## Conventions

- All subprocesses go through context-aware OTel-instrumented wrappers.
- All exported errors are wrapped with `%w`.
- Service name is `verself-deploy`. The shared `verselfotel` package owns
  resource attribute construction; this binary only adds span attributes.
