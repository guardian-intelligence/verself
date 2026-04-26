# Topology

`src/platform/topology` is the deployment topology source. CUE validates the
service endpoint graph, artifact manifest, identity, data, and network inputs,
then renders
Ansible vars to `src/platform/ansible/group_vars/all/generated/topology.yml`.

Commands:

```sh
make topology-generate
make topology-check
make topology-proof
```

The generated topology file is the only `group_vars` file that defines the
top-level `services` variable, deploy artifact manifests, public origins,
SPIFFE IDs, PostgreSQL connection budgets, object-storage UIDs, WireGuard
placement, and host firewall policy. Edit CUE, regenerate, then use
`make topology-proof` to assert the `topology-compiler` and `ansible` spans
proving the compiled topology is fresh in ClickHouse. The compiler emits
`topology-compiler` spans on every run; `topology-proof` is the live ClickHouse
assertion gate.
