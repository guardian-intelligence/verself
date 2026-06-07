# Overview

Guardian Specification defines configuration documents consumed by the
`guardian` reference CLI and compatible implementations. A document describes a
single convergence state machine over source artifacts, network access, a
substrate, shared public configuration, and component-owned Nomad runtime
logic.

A Guardian resource graph document contains:

- an `entrypoint` object reference;
- a `FlyProcedure` resource;
- a `Substrate` resource with remote access facts;
- shared configuration resources such as `PublicOrigin`;
- component extension resources.

Implementations load resource graphs from CUE, YAML, JSON, TOML, or TOON. The
source format is an interchange detail.

See [Spec Scope](spec-scope.md) for the boundary between the base Guardian
protocol, component CRDs, component-owned Nomad jobs, and runtime evidence.

## Operations

`preflight` runs the state machine through substrate readiness. It resolves the
profile, entrypoint, and referenced substrate, checks local build artifacts,
materializes the resolved resource graph, runs the configured Ansible
preflight, and emits a structured command result with `ready_to_fly`, upload
details, hook details, and stable conditions. Preflight recovers the fixed
Nomad/OpenBao substrate prerequisites.

`fly` starts with preflight and writes the resource graph into the materialized
workspace, then runs the configured Nomad job hook. Component behavior is
expressed in component-owned Nomad job files, typically as lifecycle tasks that
install, restore, reconcile, or block with stable health evidence.

## Boundaries

Guardian owns config parsing, local artifact checks, resource graph
materialization, preflight invocation, Nomad hook invocation, and structured
command responses.

The preflight playbook owns substrate access, file transfer, permissions,
remote directories, and remote tooling.

Infrastructure components own Nomad job files and runtime behavior.
Root-of-trust stores, runtime executors, edge components, and provider
integrations each define their own convergence logic.

Shared config resources describe stable facts that multiple components need.
`PublicOrigin` describes an externally visible origin. Component extension
resources describe component needs without adding new base Guardian concepts.

## Secrets

Secret values are never embedded in resource graphs, environment variables,
argv, command output, telemetry, or durable host files. Component recovery
binaries request operator-held authority through component-owned paths and
report concrete component blockers such as unseal, snapshot restore, provider
import, or service reconciliation.
