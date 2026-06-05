# Overview

Guardian Specification defines configuration documents consumed by the
`guardian` reference CLI and compatible implementations.

A Guardian config document contains:

- `kind: FlyProcedure`;
- optional `name` metadata;
- `staticConfig`: base URL and credential bundle reference;
- `board.substrate`: the remote state directory used by Guardian;
- `board.access.ssh`: the SSH path to the substrate, with optional WireGuard
  fallback coordinates;
- `board.seed`: local files copied into a content-addressed remote seed;
- `nomad`: Nomad address, namespace, and job files.

Implementations load config documents from CUE, YAML, JSON, TOML, or TOON. The
source format is an interchange detail.

## Operations

`board` checks configuration, access settings, and local seed sources. It
computes the seed digest and emits a structured command result with
`ready_to_fly`, seed details, access details, and stable conditions.

`fly` wraps Nomad. It verifies boarding readiness from the same config
document, resolves job files, plans or submits Nomad jobs, and emits a
structured command result.

## Boundaries

Guardian owns substrate access, static configuration readiness, credential
reference readiness, seed readiness, and Nomad submission evidence.

Nomad owns scheduling, task execution, task lifecycle hooks, service restart
behavior, and component-local recovery tasks.

Components own their recovery behavior inside their Nomad job definitions and
any component-local config seeded by `board`.
