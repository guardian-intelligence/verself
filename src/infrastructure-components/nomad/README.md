# Nomad Recovery Contract

Nomad is the executor boundary for recovery and deployment. Guardian boarding
delivers the pinned `:runtime_artifact` and `nomad-recover` binary to the
target through the boarded repo. `nomad-recover` installs the runtime, writes
the host-local agent config, starts the systemd unit, and waits for the local
agent API.

This component owns the Nomad binary pin and bootstrap defaults. The current
alpha slice publishes no Guardian CRD for Nomad. Add a component CRD only when
Nomad has static configuration that cannot live in `nomad-recover` defaults or
the component job files.

Product rollout ordering and service-specific bootstrap belong to deployable
components through their Nomad jobs.
