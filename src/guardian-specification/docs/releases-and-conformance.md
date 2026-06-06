# Releases and Conformance

Guardian Specification releases package:

- normative documents;
- schemas;
- examples;
- reference CLI behavior;
- development-only conformance fixtures.

Protocol releases use semantic versions. Document schemas are versioned by the
Guardian release that publishes them.

## Conformance

Conformance is a development-only API. Conformance fixtures verify
implementations during development and release qualification. Production
`preflight` and `fly` inputs use the production resource graph schema.

Conformance suites test:

- resource graph validation;
- resource graph digesting;
- local build artifact validation;
- upload digest stability;
- command result conditions;
- lifecycle hook behavior through fake commands;
- dry-run `fly` path resolution;
- repeatable live `fly` convergence in implementation-owned fixtures.

Dogfooding a config such as `gamma` is operational evidence. It uses real
infrastructure and real component Nomad jobs. The strongest dogfood signal is
two consecutive successful `fly` runs with no unexpected allocation churn.
Portable conformance fixtures define protocol compatibility.

## Release Discipline

Released document schemas have immutable meaning. Breaking changes use a new
schema version and a materialized conversion path.

Each release includes a changelog that identifies:

- added fields;
- deprecated fields;
- changed condition and reason codes;
- conformance profile changes.
