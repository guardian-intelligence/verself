# bootstrap

This directory owns first-site and recovery bootstrap only:

- `ansible/` converges host OS state, host tools, Nomad, SPIRE, and recovery
  access.
- `sites/<site>/` contains non-secret site inventory and site facts.
- `cmd/site-bootstrap/` is the operator entrypoint behind `aspect
  bootstrap-deploy`, `aspect site inventory-write`, and `aspect site
  root-handoff`.
- `internal/sitebootstrap/` contains bootstrap orchestration internals. Keep it
  focused on machine configuration, secret projection, local artifact copy, and
  Nomad handoff.
- `credentials.yml` is a non-secret pointer inventory. It must not contain live,
  encrypted, redacted, or example secret values.

Bootstrap code may use deployment-service component descriptors to submit the
first Nomad jobs, but it must not grow steady-state deployment authority. Normal
deployments belong to deployment-service and `aspect deploy`.
