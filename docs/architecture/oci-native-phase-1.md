# OCI-Native Phase 1

Phase 1 establishes the deployable-unit pattern for OCI images without turning
host substrate tasks into container work.

## Counters

| Metric | Main Baseline | Phase 1 Branch | Target Direction |
| --- | ---: | ---: | --- |
| Nomad job files with `raw_exec` | 40 | 39 | Down for ordinary services |
| Nomad job files with `podman` | 1 | 1 | Up for ordinary services |
| OCI-converted deployables | 1 | 1 | Up |
| GitHub main-to-gamma deploy workflows | 1 | 0 | Stay zero until gamma recovery is explicit |

The migration unit is a Nomad job file. Task counts are secondary because some
jobs correctly contain setup or host-bound `raw_exec` tasks.

## Phase 1 Contract

Each converted deployable owns:

- a Bazel-built OCI image target;
- a generated rules_oci digest target;
- a generated rules_oci `oci_push` target;
- a pinned trust-store layer when the workload performs outbound TLS;
- a Nomad `podman` task that refers to the image through
  `verself-oci://<output>`;
- the same health checks, canary policy, and service registrations it had before
  conversion.

Deployment reads the digest file, pushes the image through the declared `oci_push`
target to the site-local Zot registry, and rewrites `verself-oci://<output>` into
`<registry>/<repository>@sha256:<digest>`. Nomad and Podman only see immutable
digest references.

References:

- rules_oci `oci_image`, `oci_image_index`, generated `[name].digest`, and
  `oci_push`
- OCI Distribution digest references
- Nomad Podman driver image references

## Inventory

| Deployable | Raw Exec Tasks | Classification | Phase 1 Status | Proof |
| --- | ---: | --- | --- | --- |
| `src/services/analytics-service/nomad.hcl` | 0 | ordinary service | converted | `driver = "podman"`; image rendered from `verself-oci://analytics-service` to a digest reference |
| `src/services/profile-service/nomad.hcl` | 2 | ordinary service | pending | Convert after analytics canary succeeds |
| `src/services/notifications-service/nomad.hcl` | 2 | ordinary service | pending | Convert after analytics canary succeeds |
| `src/services/projects-service/nomad.hcl` | 2 | ordinary service | pending | Convert after analytics canary succeeds |
| `src/services/governance-service/nomad.hcl` | 2 | ordinary service | pending | Convert after analytics canary succeeds |
| `src/services/billing-service/nomad.hcl` | 2 | ordinary service | pending | Requires worker split review |
| `src/services/distribution-service/nomad.hcl` | 2 | ordinary service | pending | Requires release artifact path review |
| `src/services/email-service/nomad.hcl` | 2 | ordinary service | pending | Requires mail/session mount review |
| `src/services/github-integration-service/nomad.hcl` | 2 | ordinary service | pending | Requires GitHub credential mount review |
| `src/services/iam-service/nomad.hcl` | 2 | ordinary service | pending | Requires SpiceDB/Zitadel client review |
| `src/services/secrets-service/nomad.hcl` | 1 | ordinary service | pending | Requires OpenBao client review |
| `src/services/source-code-hosting-service/nomad.hcl` | 2 | ordinary service | pending | Requires Forgejo dependency review |
| `src/services/deployment-service/nomad.hcl` | 3 | controller service | pending | Convert after artifact publication path stabilizes |
| `src/services/object-storage-service/nomad.hcl` | 3 | storage/control-plane service | pending | Convert after R2/object-storage session cutover |
| `src/services/sandbox-rental-service/nomad.hcl` | 3 | compute-control service | pending | Requires VM substrate boundary review |
| `src/services/email-service/resend-keys.nomad.hcl` | 1 | one-shot controller | pending | Convert or fold into owner service |
| `src/integrations/cloudflare/r2-control-plane/nomad.hcl` | 1 | integration controller | pending | Replace during object-storage service cutover |
| `src/viteplus-monorepo/apps/company/nomad.hcl` | 1 | web frontend | pending | Needs Vite output image target |
| `src/viteplus-monorepo/apps/verself-web/nomad.hcl` | 1 | web frontend | pending | Needs Vite output image target |
| `src/infrastructure-components/nomad-observer/nomad.hcl` | 1 | observer | pending | Low-risk follow-up conversion |
| `src/infrastructure-components/verdaccio/nomad.hcl` | 2 | registry substrate | pending | Convert after package cache volume review |
| `src/infrastructure-components/spicedb/nomad.hcl` | 2 | datastore-adjacent substrate | pending | Convert after migration path review |
| `src/infrastructure-components/grafana/nomad.hcl` | 3 | operator UI substrate | pending | Convert after plugin/data directory review |
| `src/infrastructure-components/otelcol/nomad.hcl` | 2 | telemetry substrate | pending | Convert after host log/socket mount review |
| `src/infrastructure-components/electric/nomad.hcl` | 6 | projection substrate | pending | Delete custom containerd path during conversion |
| `src/infrastructure-components/electric/containerd.nomad.hcl` | 1 | superseded runtime helper | pending | Delete with Electric Podman conversion |
| `src/infrastructure-components/nats/nomad.hcl` | 2 | messaging substrate | pending | Requires storage/cluster review |
| `src/infrastructure-components/openbao/nomad.hcl` | 3 | security substrate | pending | Requires seal/unseal and storage review |
| `src/infrastructure-components/postgresql/nomad.hcl` | 3 | datastore substrate | pending | Requires durable volume and user mapping review |
| `src/infrastructure-components/stalwart/nomad.hcl` | 2 | mail substrate | pending | Requires persistent storage review |
| `src/infrastructure-components/temporal-platform/nomad.hcl` | 3 | workflow substrate | pending | Requires schema/bootstrap split review |
| `src/infrastructure-components/tigerbeetle/nomad.hcl` | 1 | ledger substrate | pending | Requires mlock/capability review |
| `src/infrastructure-components/zitadel/nomad.hcl` | 2 | identity substrate | pending | Requires masterkey and setup review |
| `src/infrastructure-components/zitadel/auth-control-plane.nomad.hcl` | 1 | identity controller | pending | Convert with Zitadel controller path |
| `src/infrastructure-components/forgejo/nomad.hcl` | 3 | source substrate | pending | Requires storage and SSH path review |
| `src/infrastructure-components/clickhouse/nomad.hcl` | 1 | datastore substrate | pending | Requires host package split review |
| `src/infrastructure-components/clickhouse/host-install.nomad.hcl` | 1 | host install | keep raw_exec | Host package installation is not an OCI workload |
| `src/infrastructure-components/haproxy/nomad.hcl` | 1 | edge substrate | pending | Requires bind/cert path review |
| `src/infrastructure-components/nftables/nomad.hcl` | 1 | host policy | keep raw_exec | Host firewall policy is not an OCI workload |
| `src/substrate/vm-orchestrator/nomad.hcl` | 3 | privileged host substrate | keep raw_exec | Firecracker/ZFS/TAP boundary stays host-native |

## Next Hillclimb

The next service should be another ordinary service with two allocations and an
existing HTTP readiness check. The acceptance check is:

```text
raw_exec task count decreases
podman task count increases
the converted job keeps canary = 1
the converted job's readiness check passes after deploy
an unchanged SHA does not roll the converted job
```
