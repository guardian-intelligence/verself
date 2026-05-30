# provisioning

`src/tools/provisioning/` owns physical machine allocation and inventory
production. Keep it limited to OpenTofu declarations and helpers that produce
site inventory. The typed operator command surface is `aspect site allocate` /
`aspect site destroy`.

Do not add host package convergence, daemon configuration, Nomad deployment,
or product service rollout here. Those belong to `src/host/`, rendered
Nomad jobs, and the `aspect deploy` path.
