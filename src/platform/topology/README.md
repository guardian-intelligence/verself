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

The generated artifacts define `topology_endpoints`, `topology_routes`,
`topology_runtime`, `topology_postgres`, and the other typed deployment inputs.
Edit CUE, regenerate, then use `make topology-proof` to assert the
`topology-compiler` and `ansible` spans proving the generated artifacts are
fresh in ClickHouse. The compiler emits `topology-compiler` spans on every run;
`topology-proof` is the live ClickHouse assertion gate.
