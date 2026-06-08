# deployment

The internal deployment path is controller-owned. `aspect deploy` should keep
the authority to resolve the site, build Bazel outputs, publish immutable
artifacts, submit owner-local Nomad jobs, and emit deployment evidence. Do not
grow a site-local service that needs a mutable source checkout or Bazel in
order to repair the site.

## Layout

- `cmd/verself-deploy/` — single binary behind the `aspect deploy` task. Keep
  deployment mutation controller-local unless a narrower admission service is
  introduced.
- `cmd/site-bootstrap/` — first-bootstrap/recovery entrypoint for sites
  that do not yet have the normal deploy path online.
- `internal/identity/` — derives the verself deploy identity env and emits W3C
  baggage so every span this binary creates carries `verself.deploy_run_key`,
  `verself.deploy_id`, `verself.site`, `verself.author`.
- Shared deploy runtime packages may still live under
  `src/services/deployment-service` during the cutover. Do not add new service
  dependencies to that boundary.

## Phase boundaries

This module is the deployment controller boundary for internal sites. Bazel
produces the deployable bytes, object storage holds immutable artifacts, and
Nomad executes runtime rollout mechanics. Site preflight and patching remain
outside this module.

## Conventions

- All subprocesses go through context-aware OTel-instrumented wrappers.
- All exported errors are wrapped with `%w`.
- Service name is `verself-deploy`. The shared `verselfotel` package owns
  resource attribute construction; this binary only adds span attributes.
