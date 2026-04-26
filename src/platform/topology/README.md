# Topology

`src/platform/topology` is the deployment topology source. CUE validates the
desired-state graph and renders typed Ansible inputs under
`src/platform/ansible/group_vars/all/generated/`.

Commands:

```sh
make topology-generate
make topology-check
make topology-proof
```

Components own their deployment facets: runtime identity, workload identity,
PostgreSQL bindings, public routes, and sync adapters such as Electric. The
generated artifacts are projections of those facets for existing Ansible roles:
`topology_endpoints`, `topology_routes`, `topology_runtime`,
`topology_postgres`, `topology_spire`, `topology_electric_instances`, and the
remaining compatibility variables in `ops.yml`.

Edit CUE, regenerate, then use `make topology-proof` to assert the
`topology-compiler` and `ansible` spans proving the generated artifacts are
fresh in ClickHouse. The compiler emits `topology-compiler` spans on every run;
`topology-proof` is the live ClickHouse assertion gate.
