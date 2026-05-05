# deployment

The typed Go orchestrator for verself deploys. It owns the Bazel-to-Nomad
adapter layer: Nomad submit/monitor, Bazel artifact resolution, and deploy
evidence writes. Operator database access is owned by
`src/tools/operator/cmd/aspect-operator` and the shared `src/tools/operator-runtime/go`
packages, not this deployment orchestrator.

## Layout

- `cmd/verself-deploy/` — single binary, subcommands grouped under
  `verself-deploy <group> <action>` (mirrors the `aspect <group> <action>`
  surface).
- `internal/identity/` — derives the verself deploy identity env and emits W3C
  baggage so every span this binary creates carries `verself.deploy_run_key`,
  `verself.deploy_id`, `verself.site`, `verself.author`.
- `internal/nomadclient/` — typed wrapper around `github.com/hashicorp/nomad/api`.
- `internal/deploymodel/` — shared value types for Garage artifact delivery and
  resolved Nomad submit jobs.
- `internal/nomadclient/` — typed wrapper around `github.com/hashicorp/nomad/api`.
  Uses `Plan` → `EnforceRegister` for CAS-safe submit, then mirrors the
  upstream `nomad deployment status -monitor` blocking-query loop on
  `Deployments.Info`.

## Phase boundaries

This module owns deploy orchestration: Bazel component discovery, Garage
artifact publication, Nomad submit/monitor, identity propagation, and
ClickHouse deploy evidence rows. Host bootstrap and patching are outside this
binary.

## Conventions

- All subprocesses go through context-aware OTel-instrumented wrappers.
- All exported errors are wrapped with `%w`.
- Service name is `verself-deploy`. The shared `verselfotel` package owns
  resource attribute construction; this binary only adds span attributes.
