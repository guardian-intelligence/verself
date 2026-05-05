# provisioning

`src/tools/provisioning/` owns physical machine allocation and inventory
production. Keep it limited to OpenTofu, controller-local provisioning
playbooks, and helpers that write
`src/host/ansible/<site>.ini`.

Do not add host package convergence, daemon configuration, Nomad deployment,
or product service rollout here. Those belong to `src/host/`, rendered
Nomad jobs, and the `aspect deploy` path.
