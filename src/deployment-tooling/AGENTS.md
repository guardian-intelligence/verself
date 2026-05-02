# deployment-tooling

The typed Go orchestrator for verself deploys. Subsumes the bash + python
plumbing under `src/substrate/scripts/` for the orchestration layer
(Nomad submit/monitor, Bazel artifact resolution, Ansible run wrapping,
ledger writes). Substrate-side scripts that wrap third-party CLIs
(`pg.sh`, `clickhouse.sh`, `tigerbeetle.sh`) stay in shell — they have no
correctness invariants for this binary to encode.

## Layout

- `cmd/verself-deploy/` — single binary, subcommands grouped under
  `verself-deploy <group> <action>` (mirrors the `aspect <group> <action>`
  surface).
- `internal/identity/` — reads the verself deploy identity env (set by
  `scripts/deploy_identity.sh`) and emits W3C baggage so every span this
  binary creates carries `verself.deploy_run_key`, `verself.deploy_id`,
  `verself.site`, `verself.author`.
- `internal/nomadclient/` — typed wrapper around `github.com/hashicorp/nomad/api`.
  Uses `Plan` → `EnforceRegister` for CAS-safe submit, then mirrors the
  upstream `nomad deployment status -monitor` blocking-query loop on
  `Deployments.Info`.

## Phase boundaries

This module owns deploy orchestration: BEP-driven artifact resolution,
Nomad submit/monitor, streaming Ansible event capture, identity propagation,
and ClickHouse ledger rows.

## Conventions

- All subprocesses go through context-aware OTel-instrumented wrappers.
- All exported errors are wrapped with `%w`.
- Service name is `verself-deploy`. The shared `verselfotel` package owns
  resource attribute construction; this binary only adds span attributes.
