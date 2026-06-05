# SPIRE Recovery Contract

SPIRE owns workload identity material and the registry of component-declared
SPIFFE identities. Recovery starts from the pinned `:runtime_artifact`, a
site-local trust domain, and the `:identity_registry` descriptor.

Components declare identities next to their deployable units. This package
collects those declarations; it does not own service rollout or runtime secret
delivery.
