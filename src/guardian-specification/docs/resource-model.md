# Resource Model

Guardian alpha uses a single config document as the runtime input:

```yaml
kind: FlyProcedure

staticConfig:
  baseURL: https://gamma.guardianintelligence.org
  credentialsRef: gamma-credentials

board:
  substrate:
    stateDir: /var/lib/guardian
  access:
    ssh:
      host: 206.223.228.87
      port: 22
      user: ubuntu
      knownHostsFile: ~/.ssh/known_hosts
  seed:
    targetRoot: /var/lib/guardian/seeds
    paths:
      - source: bazel-bin/src/guardian-specification/cli/cmd/guardian/guardian_/guardian
        target: bin/guardian
        mode: "0755"

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
Guardian includes these values in the seed manifest and reports a digest.

`board` declares how to reach the substrate and which local files become the
remote seed.

`nomad` declares the jobs to plan or submit. Guardian does not model Nomad HCL
internals.

## Command Results

Command results contain observed outcomes: readiness, seed digest, seed root,
Nomad job path status, condition types, and reason codes. Command results are
ephemeral CLI responses and are not config resources.

## Versioning

Released document schemas are immutable contracts. A released schema may add
optional fields with compatible semantics. Required fields, renamed fields,
removed fields, changed defaults, or changed meanings require a new schema
version.

Protocol releases package schemas, examples, reference CLI behavior, and
conformance expectations.
