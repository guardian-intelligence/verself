# deployment

The typed Go adapter for Verself deploys. It owns the narrow Bazel-to-Nomad
handoff: build deployable `nomad_component` targets, parse owner-local Nomad
jobs with the target Nomad API, and register those jobs. Operator database
access is owned by `src/tools/operator/cmd/aspect-operator` and the shared
`src/tools/operator-runtime/go` packages, not this deployment adapter.

## Layout

- `cmd/verself-deploy/` — single binary, subcommands grouped under
  `verself-deploy <group> <action>` (mirrors the `aspect <group> <action>`
  surface). `run` builds deployable units and registers their Nomad jobs.
- `internal/identity/` — derives the verself deploy identity env and emits W3C
  baggage so every span this binary creates carries `verself.deploy_run_key`,
  `verself.deploy_id`, `verself.site`, `verself.author`.
- `internal/nomadclient/` — typed wrapper around `github.com/hashicorp/nomad/api`.

## Phase boundaries

This module does not publish artifacts, reconcile OpenBao/Postgres, mutate HCL,
run canaries, monitor rollouts, or roll jobs back. Bazel produces the deployable
bytes and Nomad owns deployment mechanics. ClickHouse is an observability
backend, not a runtime dependency of this binary. Host bootstrap and patching
are outside this binary.

## Conventions

- All subprocesses go through context-aware OTel-instrumented wrappers.
- All exported errors are wrapped with `%w`.
- Service name is `verself-deploy`. The shared `verselfotel` package owns
  resource attribute construction; this binary only adds span attributes.
