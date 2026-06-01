# deployment

The deployment tooling is moving behind the site-local deployment-service.
`aspect deploy` should remain a thin client: resolve the site endpoint,
authenticate, submit a commit-SHA deployment request, and follow the returned
deployment ID. The service owns build orchestration, artifact publication,
Nomad submission, deployment state, errors as data, realtime health ingestion,
and promotion evidence.

## Layout

- `cmd/verself-deploy/` — single binary behind the `aspect deploy` task. Keep
  request/auth/follow behavior in the CLI and move deployment mutation into the
  site-local deployment-service.
- `internal/identity/` — derives the verself deploy identity env and emits W3C
  baggage so every span this binary creates carries `verself.deploy_run_key`,
  `verself.deploy_id`, `verself.site`, `verself.author`.
- `internal/nomadclient/` — typed wrapper around `github.com/hashicorp/nomad/api`.

## Phase boundaries

This module must not grow new deployment authority while the service boundary is
being introduced. Bazel produces the deployable bytes, the deployment-service
records the state machine and evidence, and Nomad executes runtime rollout
mechanics. Host bootstrap and patching remain outside this module.

## Conventions

- All subprocesses go through context-aware OTel-instrumented wrappers.
- All exported errors are wrapped with `%w`.
- Service name is `verself-deploy`. The shared `verselfotel` package owns
  resource attribute construction; this binary only adds span attributes.
