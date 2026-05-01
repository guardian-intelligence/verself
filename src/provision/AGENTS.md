# provision

`src/provision/` owns physical machine allocation and inventory production.
Keep it limited to OpenTofu, controller-local provisioning playbooks, and
helpers that write `src/substrate/ansible/inventory/<site>.ini`.

Do not add host package convergence, daemon configuration, Nomad deployment,
or product service rollout here. Those belong to `src/substrate/`, rendered
Nomad jobs, and the `aspect deploy` path.
