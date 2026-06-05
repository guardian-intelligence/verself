# Nomad Recovery Contract

Nomad is the executor boundary for recovery and deployment. The controller-side
SSH/upload machinery must deliver the pinned `:runtime_artifact` to a target
node and start the agent with a site-local config before submitting component
recovery jobs.

This component owns the Nomad binary pin. It does not own product rollout
ordering or service-specific bootstrap; those belong to the deployable
components that submit Nomad jobs.
