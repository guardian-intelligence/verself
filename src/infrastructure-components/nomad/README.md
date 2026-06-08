# Nomad Recovery Contract

Nomad is the executor boundary for recovery and deployment. The Nomad
component owns the pinned Nomad binary and runtime artifact. Guardian preflight
materializes that artifact on the target and starts the single-node agent
before `guardian fly` submits component-owned jobs.

On a wiped host, preflight prepares and starts OpenBao as a systemd root
service before Nomad starts. Nomad is then configured with OpenBao integration
inputs already present, and `guardian fly` submits component-owned Nomad jobs
after both root services are healthy.

The current alpha slice publishes no Guardian CRD for Nomad. Add a component
CRD only when Nomad has static configuration that no longer fits the preflight
playbook and Nomad-owned job files.

Product rollout ordering and service-specific bootstrap belong to deployable
components through their Nomad jobs.
