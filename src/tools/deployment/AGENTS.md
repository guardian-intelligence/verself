# deployment

The typed Go orchestrator for verself deploys. It owns the Bazel-to-Nomad
adapter layer: Nomad submit/monitor and Bazel artifact resolution. Operator
database access is owned by
`src/tools/operator/cmd/aspect-operator` and the shared `src/tools/operator-runtime/go`
packages, not this deployment orchestrator.

## Layout

- `cmd/verself-deploy/` — single binary, subcommands grouped under
  `verself-deploy <group> <action>` (mirrors the `aspect <group> <action>`
  surface). `run` performs deployment and rollback-gated post-deploy checks;
  `canary` runs declared checks without submitting jobs.
- `internal/identity/` — derives the verself deploy identity env and emits W3C
  baggage so every span this binary creates carries `verself.deploy_run_key`,
  `verself.deploy_id`, `verself.site`, `verself.author`.
- `internal/nomadclient/` — typed wrapper around `github.com/hashicorp/nomad/api`.
- `internal/deploymodel/` — shared value types for S3 artifact delivery and
  resolved Nomad submit jobs.
- `internal/nomadclient/` — typed wrapper around `github.com/hashicorp/nomad/api`.
  Uses `Plan` → `EnforceRegister` for CAS-safe submit, mirrors the upstream
  `nomad deployment status -monitor` blocking-query loop on `Deployments.Info`,
  and exposes revert/deregister helpers for failed post-deploy canaries.

## Phase boundaries

This module owns deploy orchestration: Bazel component discovery, R2-backed
artifact publication, Nomad submit/monitor, post-deploy canary execution, and
identity propagation. It emits OpenTelemetry spans and stdout. ClickHouse is an
observability backend, not a runtime dependency of this binary. Host bootstrap
and patching are outside this binary.

## Post-Deploy Canaries

Deployable packages declare live checks with `post_deploy_canary` and attach
them to `nomad_component(post_deploy_canaries = [...])`. The rule records:

- `kind`: `cli` or `browser`;
- `canary_size`: `medium` or `large`;
- `target`: executable Bazel target;
- `args`: target argv with deploy placeholders such as `{site}`, `{repo_root}`,
  `{deploy_run_key}`, `{sha}`, `{component}`, and `{job_id}`;
- `canary_timeout`: optional Go duration.

`verself-deploy run --post-deploy-checks=medium|large|all|none` gates changed
Nomad jobs after rollout health and before reporting healthy. A failed canary
causes a best-effort rollback to the prior Nomad version, or deregistration when
the job had no prior version. `verself-deploy canary --size=medium|large|all`
runs the same declarations against the current site without rollback context.

## Conventions

- All subprocesses go through context-aware OTel-instrumented wrappers.
- All exported errors are wrapped with `%w`.
- Service name is `verself-deploy`. The shared `verselfotel` package owns
  resource attribute construction; this binary only adds span attributes.
