# Site Recovery Intents

`aspect site recover` submits a typed recovery intent to the control plane. The
operation starts with read-only observation, builds a provider-neutral plan, and
applies only the transitions authorized by the intent file.

Normal deployments continue through `aspect deploy`. A recovery intent is used
when a site, node, provider surface, or secret set needs adoption, repair,
reconstruction, or disaster-recovery rehearsal.

## Command Surface

```text
aspect site recover --intent=<path>
aspect site recover --intent=<path> --check
aspect site recover --operation-id=<operation-id>
aspect site recover --operation-id=<operation-id> --events
```

The intent file carries the requested operation mode. `--check` forces a
read-only request for local inspection. The command rejects an intent that lacks
an explicit mode, scope, trust class, source commit, and artifact policy.

The client performs local file resolution and validation, packages approved
local artifacts, authenticates to the control plane, submits the request, and
waits on the returned operation unless status-only flags are used.

## API Shape

```text
POST /api/v1/site-recovery-operations:check
POST /api/v1/site-recovery-operations:recover
GET  /api/v1/site-recovery-operations/{operation_id}
GET  /api/v1/site-recovery-operations/{operation_id}/events
```

The API accepts a recovery intent document plus a content-addressed artifact
set. Local paths are a client-side concern and are not part of the service
contract. The service sees artifact IDs, digests, declared media types,
sensitivity class, byte lengths, and sealed payload handles.

The API has no built-in site names. Site names are arbitrary stable identifiers
inside intent data. A site commonly called `prod` is represented by a trust
class and capability set, not by a reserved API value.

## Intent File

Intent files are JSON-compatible YAML or JSON. YAML input is limited to one
document with mappings, sequences, strings, booleans, integers, and nulls.
Custom tags, anchors, aliases, merge keys, duplicate keys, and unknown fields
are rejected before hashing.

Top-level shape:

```yaml
version: verself.site_recovery_intent.v1

operation:
  mode: recover_if_needed
  idempotency_key: recover-ash-control-20260604
  scope:
    - external_provider_state
    - runtime_secret_import
    - host_adoption

source:
  commit: 0123456789abcdef0123456789abcdef01234567
  repo: https://github.com/guardian-intelligence/verself.git

site:
  name: ash-control-01
  trust_class_ref: platform_prod_control_plane.v1
  installation_id: inst_5NZSEA08R8P3HN566DNH8D301M
  domains:
    product: verself.sh
    company: guardianintelligence.org

trust_classes:
  - id: platform_prod_control_plane.v1
    role: global_provider_control_plane
    custody: platform_owned_bare_metal
    exposure: public_customer
    tenant_data: customer_data_allowed
    provider_authority: global_platform_authority
    secret_authority: site_local_secrets_service
    recovery_authorizers:
      - founder_site_root_key
      - deployment_service_operator_api
    required_controls:
      - spiffe_mtls_internal
      - zitadel_operator_auth
      - zanzibar_authorization
      - secrets_service_openbao_boundary
      - governance_audit_events
      - clickhouse_recovery_evidence

local_artifacts:
  - id: cloudflare-account-authority
    path: ./secrets/cloudflare-account-authority.json
    media_type: application/json
    sensitivity: provider_admin_secret
    sha256: 8d969eef6ecad3c29a3a629280e686cf0c3f5d5a86aff3ca12020c923adc6c92
    max_bytes: 8192
    required_mode: "0600"

capabilities:
  external_provider_state:
    providers:
      cloudflare:
        account_ref: cloudflare-account
        dns: reconcile_owned_records
        tls: ensure_public_edge_certificates
        r2: ensure_site_capabilities
  runtime_secret_import:
    imports:
      - name: cloudflare.account_admin
        artifact_ref: cloudflare-account-authority
        target: secrets_service
  host_adoption:
    node:
      provider: latitude
      public_ipv4: 203.0.113.10
      access: operator_recovery_ssh
```

Optional capability sections are usable only when listed in `operation.scope`.
The validator rejects unscoped capability sections so an added block cannot
silently expand authority. A listed scope requires the corresponding capability
section to be present and complete.

## Operation Modes

| Mode | Behavior |
| --- | --- |
| `check_only` | Observe current state, build a plan, record drift and missing prerequisites, and stop before mutation. |
| `recover_if_needed` | Observe, plan, apply missing authorized transitions, verify, and record evidence. |
| `assimilate_node` | Adopt a fresh or replaced node into the declared site trust class, including host state and selected secret ingress. |

Every mode is check-first. Mutation starts only after the observed state,
expected state hash, scope, trust class, and artifact digests are recorded.

## State Machine

```text
requested
  -> authenticated
  -> authorized
  -> intent_received
  -> intent_validated
  -> local_artifacts_resolved
  -> expected_state_loaded
  -> observed
  -> planned
  -> no_recovery_needed
     | recovery_required
     | recovery_started
  -> provider_state_applied
  -> host_state_applied
  -> secrets_imported
  -> services_reconciled
  -> verified
  -> evidence_recorded
  -> completed
```

Terminal failures are stable problem codes:

```text
auth_failed
authorization_failed
intent_invalid
artifact_untrusted
expected_state_invalid
authority_unavailable
provider_observe_failed
provider_apply_failed
host_adoption_failed
secret_import_failed
verification_failed
recovery_required_but_not_authorized
```

Failures are operation data. Each event records the phase, resource type,
observed digest or provider request ID when available, remediation, and trace
context.

## Trust Classes

A Site Trust Class is a named composite of authority, custody, exposure, data,
and control requirements. It is ordinary versioned configuration referenced by
an intent file. It does not derive from the site name.

Trust class dimensions:

| Dimension | Examples |
| --- | --- |
| `role` | `global_provider_control_plane`, `site_local_control_plane`, `workload_cell`, `recovery_drill_site` |
| `custody` | `platform_owned_bare_metal`, `customer_owned_cloud`, `customer_owned_onprem` |
| `exposure` | `public_customer`, `public_rehearsal`, `operator_private`, `customer_private` |
| `tenant_data` | `customer_data_allowed`, `synthetic_data_only`, `no_persistent_customer_data` |
| `provider_authority` | `global_platform_authority`, `site_scoped_platform_authority`, `customer_supplied_authority`, `none` |
| `secret_authority` | `site_local_secrets_service`, `customer_kms_envelope`, `operator_local_recovery_only` |
| `network_boundary` | `public_edge`, `pomerium_gated`, `outbound_only_cell`, `private_wan_cell` |
| `billing_surface` | `platform_showback`, `billable_customer_capacity`, `non_billable_rehearsal` |
| `evidence_level` | `governance_audit`, `clickhouse_recovery`, `provider_request_ids`, `host_attestation` |

Realistic initial classes:

| Class | Use |
| --- | --- |
| `platform_prod_control_plane.v1` | Customer-facing platform site that owns global provider state and production deployment orchestration. |
| `platform_rehearsal_control_plane.v1` | Public or gated release rehearsal site with production-shaped configuration and synthetic or restricted data. |
| `operator_dev_site.v1` | Operator-private development site with disposable provider resources and no customer data. |
| `customer_byoc_cloud_cell.v1` | Customer-owned cloud or colocation compute cell attached to the hosted control plane through outbound mTLS. |
| `customer_onprem_connected_cell.v1` | Customer-owned on-prem compute cell with customer network controls, customer-held physical custody, hosted admission, and local execution evidence. |

The `platform_prod_control_plane.v1` class is what gives a site production
authority. The string used as the site name is only an identifier.

## Local Artifact Handling

The Aspect client resolves local artifact paths relative to the intent file
directory unless an absolute path is provided. Each artifact must be explicitly
declared with an ID, media type, sensitivity, maximum size, and SHA-256 digest.

File checks:

- path is canonicalized before use;
- path is a regular file;
- symlinks, hard links outside the intent directory policy, device files, FIFOs,
  sockets, and directories are rejected;
- sensitive artifacts require owner-only permissions;
- byte length is at or below `max_bytes`;
- SHA-256 matches the declared digest;
- media type matches the declared decoder;
- decoded JSON/YAML rejects unknown fields and duplicate keys.

Artifacts are uploaded as content-addressed operation inputs. Secret artifacts
are delivered only to the service that owns the target boundary, typically
`secrets-service`. Deployment-service records artifact digests, classifications,
and target operation IDs. It does not return secret values in status responses
or events.

## Authority Boundaries

```text
aspect site recover
  -> deployment-service recovery API
  -> provider or host control-plane component
  -> secrets-service when secret material is required
  -> OpenBao
  -> external provider or target node
```

Deployment-service owns the recovery operation, idempotency, authorization,
planning, status, and evidence. Provider components own provider-specific plan,
apply, drift, credential rotation, and request ID semantics. Secrets-service is
the normal OpenBao boundary.

An intent may authorize local secret ingress for recovery. That ingress is
scoped by artifact ID, target secret name, target service, trust class,
operation ID, and actor. Provider account slot selection and durable credential
paths remain internal provider or secrets-service state.

## Check And Recover Semantics

The service loads expected state from the requested commit and the submitted
intent. It observes provider state, host state, service state, and secret
presence before planning. The planner classifies every delta:

| Class | Meaning |
| --- | --- |
| `satisfied` | Current state matches expected state. |
| `missing` | Expected resource is absent and can be created if scope allows. |
| `different_owned` | Owned resource differs and can be updated if scope allows. |
| `unmanaged_conflict` | Provider resource exists without adoption metadata or ownership proof. |
| `dangerous_delete` | Current resource would need deletion or destructive replacement. |
| `blocked` | Required authority, artifact, host access, or service dependency is unavailable. |

Create and update operations for owned resources are ordinary recovery actions.
Delete and adoption of unmanaged resources require explicit capability flags in
the intent and a separate evidence record. The first version should gate deletes
out of `recover_if_needed`.

## Provider Key Onboarding

Provider key onboarding is outside the core recovery API. The recovery API
accepts declared authority references and declared local artifacts. Later
provider-specific onboarding can create those references through narrower
commands, password-manager integrations, or provider OAuth/device flows.

Those flows should produce one of:

- a secrets-service reference usable by a recovery intent;
- a content-addressed local artifact declaration with digest and sensitivity;
- a provider control-plane operation ID that can be referenced by the intent.

The recovery API remains stable while provider onboarding gets easier.

## Verification

A completed recovery operation requires:

- operation record with source commit, intent hash, trust class, and actor;
- artifact digest inventory and sensitivity classes;
- provider plans and provider request IDs;
- host adoption evidence when host state is in scope;
- secrets-service import or read evidence when secrets are in scope;
- ClickHouse evidence rows for every applied phase;
- final verification results from the owning provider and service components.

The final status is `completed` only when all scoped phases have terminal
evidence. A partial repair ends in a terminal failure with the completed phases
and remaining blockers preserved in the event stream.

## Related Documents

- `docs/architecture/service-change-reference-architecture.md`
- `docs/architecture/secrets-and-integrations.md`
- `docs/architecture/disaster-recovery-tracer-bullets.md`
- `docs/architecture/byoc-on-prem-cells.md`
