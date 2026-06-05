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
`board` and `fly` inputs use the production config document schema.

Conformance suites test:

- config document validation;
- static config digesting;
- seed source validation;
- seed digest stability;
- command result conditions;
- provider boundary behavior through fake providers.

Dogfooding a config such as `gamma` is operational evidence. It uses real
infrastructure and real component recovery jobs. That evidence informs
implementation quality and release readiness; portable conformance fixtures
define protocol compatibility.

## Release Discipline

Released document schemas have immutable meaning. Breaking changes use a new
schema version and a materialized conversion path.

Each release includes a changelog that identifies:

- added fields;
- deprecated fields;
- changed condition and reason codes;
- conformance profile changes.
