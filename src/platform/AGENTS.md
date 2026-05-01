# platform

`src/platform/` now contains platform policy and architecture documents that
have not moved to service-local homes yet. Do not add substrate convergence,
OpenTofu provisioning, or Nomad deploy code here.

- Host and daemon convergence lives in `src/substrate/`.
- Bare-metal allocation lives in `src/provision/`.
- Public policy content rendered by verself-web lives under
  `src/viteplus-monorepo/apps/verself-web/src/routes/_workshop/policy`.
