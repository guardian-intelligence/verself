# provisioning

`src/tools/provisioning/` owns the lean OpenTofu module for physical machine
allocation. The typed operator command surface is `aspect site allocate` /
`aspect site destroy`; it runs this module with site-local state and writes
`src/host/sites/<site>/inventory.ini`.

Do not add host package convergence, daemon configuration, Nomad deployment,
or product service rollout here. Those belong to `src/host/`, rendered
Nomad jobs, and the `aspect deploy` path.
