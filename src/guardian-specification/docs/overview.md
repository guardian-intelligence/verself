# Overview

Guardian Specification defines configuration documents consumed by the
`guardian` reference CLI and compatible implementations.

A Guardian config document contains:

- `kind: FlyProcedure`;
- optional `name` metadata;
- `staticConfig`: base URL and credential bundle reference;
- `board.access`: access lifecycle hook;
- `board.upload.run`: upload lifecycle hook;
- `board.upload.verify`: digest verification lifecycle hook;
- `nomad`: Nomad address, namespace, and job files.

Implementations load config documents from CUE, YAML, JSON, TOML, or TOON. The
source format is an interchange detail.

## Operations

`board` checks configuration and local build artifacts, computes the upload
digest, runs access and upload lifecycle hooks, and emits a structured command
result with `ready_to_fly`, access details, upload details, hook details, and
stable conditions.

`fly` wraps Nomad. It verifies boarding readiness from the same config
document, resolves job files, plans or submits Nomad jobs, and emits a
structured command result.

## Boundaries

Guardian owns config parsing, local artifact checks, upload digest computation,
lifecycle hook execution, digest comparison, and Nomad submission evidence.

Lifecycle hook commands own substrate access, file transfer, permissions,
remote directories, and remote tooling.

Nomad owns scheduling, task execution, task lifecycle hooks, service restart
behavior, and component-local recovery tasks.

Components own their recovery behavior inside their Nomad job definitions.
