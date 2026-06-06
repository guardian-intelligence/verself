# Nomad Recovery Contract

Nomad is the executor boundary for recovery and deployment. Guardian boarding
delivers the pinned `:runtime_artifact` and `nomad-recover` binary to the
target through the boarded repo. `nomad-recover` installs the runtime, writes
the host-local agent config, starts the systemd unit, and waits for the local
agent API.

On a wiped host, Guardian boarding prepares OpenBao before Nomad starts.
`openbao-recover prepare` installs the OpenBao runtime, host directories,
config, and `/etc/verself/openbao/ca.pem` without initializing or unsealing the
store. `nomad-recover` then starts the local single-node agent once, with Vault
integration already present in the generated config.

OpenBao itself is then submitted as a component-owned Nomad job. Its recovery
task initializes, restores, unseals, reconciles, or reports concrete blockers
without forcing another Nomad agent restart.

This component owns the Nomad binary pin and bootstrap defaults. The current
alpha slice publishes no Guardian CRD for Nomad. Add a component CRD when
Nomad has static configuration that no longer fits in `nomad-recover` defaults
or the component job files.

Product rollout ordering and service-specific bootstrap belong to deployable
components through their Nomad jobs.
