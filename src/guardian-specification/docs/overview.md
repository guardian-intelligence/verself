# Overview

Guardian Specification defines configuration documents consumed by the
`guardian` reference CLI and compatible implementations. A document describes a
single convergence state machine over source artifacts, network access, a
substrate, shared public configuration, and component-owned Nomad recovery
logic.

A Guardian resource graph document contains:

- an `entrypoint` object reference;
- a `FlyProcedure` resource;
- a `Substrate` resource with access and upload lifecycle hooks;
- shared configuration resources such as `PublicOrigin`;
- component extension resources.

Implementations load resource graphs from CUE, YAML, JSON, TOML, or TOON. The
source format is an interchange detail.

See [Spec Scope](spec-scope.md) for the boundary between the base Guardian
protocol, component CRDs, component-owned Nomad jobs, and runtime evidence.

## Operations

`board` runs the state machine through substrate readiness. It resolves the
entrypoint and referenced substrate, checks local build artifacts, computes the
upload digest, runs the `Substrate` access and upload lifecycle hooks, and
emits a structured command result with `ready_to_fly`, upload details, hook
details, and stable conditions.

`fly` starts with boarding and writes the resource graph into the boarded
workspace. Component recovery is expressed in component-owned Nomad job files,
typically as prestart tasks that install, restore, reconcile, or block with
stable health evidence.

## Boundaries

Guardian owns config parsing, local artifact checks, upload digest computation,
lifecycle hook execution, digest comparison, resource graph materialization, and
structured command responses.

Lifecycle hook commands own substrate access, file transfer, permissions,
remote directories, and remote tooling.

Infrastructure components own Nomad job files and recovery behavior.
Root-of-trust stores, runtime executors, edge components, and provider
integrations each define their own recovery logic.

Shared config resources describe stable facts that multiple components need.
`PublicOrigin` describes an externally visible origin. Component extension
resources describe component needs without adding new base Guardian concepts.

## Root Trust

Root trust material is external authority required to continue recovery. It may
be Shamir unseal shares, an HSM/KMS seal, operator-recipient PGP identities,
provider parent credentials, or backup retrieval authority.

Guardian surfaces root trust blockers through conditions. The base condition is
`RootTrustMaterialAvailable`. Secret values are never embedded in resource
graphs, environment variables, argv, command output, telemetry, or durable host
files.
