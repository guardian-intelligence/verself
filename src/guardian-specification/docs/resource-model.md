# Resource Model

Guardian alpha uses a single config document as the runtime input:

```yaml
kind: FlyProcedure

staticConfig:
  baseURL: https://gamma.guardianintelligence.org
  credentialsRef: gamma-credentials

board:
  access:
    argv: [ssh, -T, ubuntu@206.223.228.87, true]
  upload:
    run:
      argv: [ansible-playbook, src/sites/gamma/board.yml]
    verify:
      argv: [ssh, -T, ubuntu@206.223.228.87, sha256sum /path/to/upload.tar.zst]

nomad:
  address: http://127.0.0.1:4646
  namespace: default
  jobs:
    - path: src/infrastructure-components/openbao/nomad.hcl
      requiredFor: [recovery]
```

`name` is optional metadata. Implementations do not use `name` for dispatch.

## Desired Inputs

`staticConfig.baseURL` is the configured external base URL.
`staticConfig.credentialsRef` points at the credential bundle to resolve.

`board.access`, `board.upload.run`, and `board.upload.verify` are lifecycle
hooks. Guardian executes them as argv commands after preparing the local upload
bundle. The hooks own access, transfer, permissions, and remote tooling.

`nomad` declares the jobs to plan or submit. Guardian does not model Nomad HCL
internals.

## Command Results

Command results contain observed outcomes: readiness, upload digest, observed
digest, hook status, Nomad job path status, condition types, and reason codes.
Command results are ephemeral CLI responses and are not config resources.

## Versioning

Released document schemas are immutable contracts. A released schema may add
optional fields with compatible semantics. Required fields, renamed fields,
removed fields, changed defaults, or changed meanings require a new schema
version.

Protocol releases package schemas, examples, reference CLI behavior, and
conformance expectations.
