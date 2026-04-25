# Topology

`src/platform/topology` is the deployment topology source. CUE validates the
service endpoint graph and renders Ansible inputs to
`src/platform/ansible/group_vars/all/generated/services.yml`.

Commands:

```sh
make topology-generate
make topology-check
make topology-proof
```

The generated registry is the only `group_vars` file that defines the top-level
`services` variable. Edit CUE, regenerate, then use `make topology-proof` to
assert the `topology-compiler` and `ansible` spans proving the compiled registry
is fresh in ClickHouse. The compiler emits `topology-compiler` spans on every
run; `topology-proof` is the live ClickHouse assertion gate.
