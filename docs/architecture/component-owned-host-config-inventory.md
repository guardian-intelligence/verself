# Package-Owned Host Configuration Inventory

Centralized workload and platform-component configuration is being removed from `src/host-configuration/ansible/group_vars/all/topology/*.yml`. The post-bootstrap boundary should move authored declarations to the system that applies them:

- Product API service code, migrations, generated API contracts, artifact targets, and authored Nomad jobspecs stay in `src/services/<service>/`.
- Frontend deployment files stay in their app packages under `src/frontends/`.
- Non-service platform components such as ClickHouse, Electric, Zitadel, OpenBao, SpiceDB, Temporal, HAProxy, Stalwart, Grafana, and Pomerium live in `src/host-configuration/components/<component>/`.
- Host configuration, including component-local Ansible roles, nftables, DNS, HAProxy edge config, SPIRE registrations, Postgres roles, systemd units, users, and filesystem paths, stays in `src/host-configuration/`.

There is no cross-directory rendering layer. A platform component under `src/host-configuration/components/<component>/` may be a standard Ansible role and may also own a `nomad.hcl` file. Product services and frontends do not contain runnable Ansible; when they need durable host prerequisites, those prerequisites live in host-configuration component roles or shared host roles.

## Source Files

| file | centralized contents | target ownership |
|---|---|---|
| `routes.yml` | Public/browser/operator/protocol/guest routes, body limits, WAF mode, CORS mode, route path allowlists | Direct HAProxy/DNS host config under `src/host-configuration/`; platform component routes live with their host-configuration component role where practical. |
| `dns.yml` | DNS records duplicating route hostnames | Direct DNS reconciler inputs under `src/host-configuration/`. |
| site `topology_endpoints` | Ports, bind addresses, exposure class, protocol, interface auth, path prefixes, health probes | Component role defaults for host-owned listeners; Nomad component descriptors for runtime services. |
| `postgres.yml` | Per-workload or platform-component database, owner, role connection limit | Component-local Ansible roles for component databases; shared PostgreSQL server invariants remain under the PostgreSQL host role. Service migrations remain with service code. |
| `spire.yml` | Workload-to-workload SPIFFE authorization edges | Component-local Ansible roles or shared SPIRE role inputs under `src/host-configuration/`. |
| `components.yml` | Deployment supervisor, identities, nftables files, component order, workload auth/bootstrap, ClickHouse grants, credstore dirs, secret refs, runtime users/units | Split by executor: Nomad deployment specs in owner packages; durable host concerns in `src/host-configuration/`. |
| `ops.yml` | Subdomains/domains, Electric source/publication instances, Temporal namespaces/role bindings | Platform component roles plus direct host config files. Site domain, WireGuard, artifact storage, bare-metal host facts stay site-level. |
| `clusters.yml` | Temporal port topology; Garage cluster topology | Platform component roles plus direct host config files. Physical disk and node facts stay site-level. |

## Ingress Routes

All `topology_routes` entries belong to the package that owns the upstream workload or platform component. `public_haproxy`, `direct_smtp`, and `firecracker_host` are gateway resources and should remain substrate-global.

| component | gateway | host | interface | route kind | details |
|---|---|---|---|---|---|
| `iam_service` | `public_haproxy` | `@` | `public_api` | `browser_origin` | Auth callback/session paths, same-origin CORS, 64 KiB body, WAF detection. |
| `verself_web` | `public_haproxy` | `@` | `frontend` | `browser_origin` | Product apex frontend, same-origin CORS, WAF detection. |
| `company` | `public_haproxy` | `@` | `frontend` | `browser_origin` | Company-zone apex frontend, same-origin CORS, WAF detection. |
| `billing` | `public_haproxy` | `billing.api` | `public_api` | `public_api_origin` | `/api/v1`, 1 MiB body, WAF blocking. |
| `sandbox_rental` | `public_haproxy` | `sandbox.api` | `public_api` | `public_api_origin` | `/api/v1`, 1 MiB body, WAF blocking. |
| `iam_service` | `public_haproxy` | `iam.api` | `public_api` | `public_api_origin` | `/api/v1`, 1 MiB body, WAF blocking. |
| `profile_service` | `public_haproxy` | `profile.api` | `public_api` | `public_api_origin` | `/api/v1`, 16 KiB body, WAF blocking. |
| `notifications_service` | `public_haproxy` | `notifications.api` | `public_api` | `public_api_origin` | `/api/v1`, 16 KiB body, WAF blocking. |
| `projects_service` | `public_haproxy` | `projects.api` | `public_api` | `public_api_origin` | `/api/v1`, 64 KiB body, WAF blocking. |
| `source_code_hosting_service` | `public_haproxy` | `source.api` | `public_api` | `public_api_origin` | `/api/v1`, 1 MiB body, WAF blocking. |
| `governance_service` | `public_haproxy` | `governance.api` | `public_api` | `public_api_origin` | `/api/v1`, 1 MiB body, WAF blocking. |
| `secrets_service` | `public_haproxy` | `secrets.api` | `public_api` | `public_api_origin` | `/api/v1`, 1 MiB body, WAF blocking. |
| `mailbox_service` | `public_haproxy` | `mail.api` | `public_api` | `public_api_origin` | `/api/v1`, 1 MiB body, WAF blocking. |
| `grafana` | `public_haproxy` | `dashboard` | `operator_ui` | `operator_origin` | Operator UI, WAF detection. |
| `pomerium` | `public_haproxy` | `access` | `access_portal` | `operator_origin` | Operator access portal, WAF detection. |
| `source_code_hosting_service` | `public_haproxy` | `git` | `git_smart_http` | `protocol_origin` | Git smart HTTP, 1 MiB body, WAF detection. |
| `zitadel` | `public_haproxy` | `auth` | `oidc` | `protocol_origin` | OIDC provider route, WAF detection. |
| `stalwart` | `public_haproxy` | `mail` | `jmap` | `protocol_origin` | JMAP/HTTP mail route, WAF detection. |
| `stalwart` | `direct_smtp` | `mail` | `smtp` | `protocol_origin` | Public SMTP, WAF off. |
| `sandbox_rental` | `firecracker_host` | `10.255.0.1` | `public_api` | `guest_host_route` | Guest access to GitHub runner JIT, runner bootstrap, stickydisk, checkout bundle paths. |
| `forgejo` | `firecracker_host` | `10.255.0.1` | `forgejo_http` | `guest_host_route` | Guest access to Forgejo actions and Git smart HTTP paths. |

## Runtime Interfaces

These `topology_endpoints` entries describe owner-package ports, bindings, protocols, auth mode, path prefixes, and probes.

| component | endpoints | interfaces |
|---|---|---|
| `billing` | `internal_https:4255`, `public_http:4242` | `internal_api` SPIFFE mTLS `/internal`, `public_api` Zitadel JWT `/api/v1` |
| `company` | `http:4252` | `frontend` unauthenticated HTTP |
| `forgejo` | `http:3000` | `forgejo_http` operator protocol |
| `governance_service` | `internal_https:4254`, `public_http:4250` | `internal_api` SPIFFE mTLS `/internal`, `public_api` Zitadel JWT `/api/v1` |
| `grafana` | `http:4300` | `operator_ui` operator frontend |
| `iam_service` | `internal_https:4241`, `public_http:4248` | `internal_api` SPIFFE mTLS `/internal`, `public_api` Zitadel JWT `/api/v1` |
| `mailbox_service` | `public_http:4246` | `public_api` Zitadel JWT `/api/v1` |
| `notifications_service` | `public_http:4260` | `public_api` Zitadel JWT `/api/v1` |
| `object_storage_service` | `admin_http:4257`, `public_http:4256` | `admin_api` SPIFFE mTLS, `public_api` Zitadel JWT `/api/v1` |
| `profile_service` | `internal_https:4249`, `public_http:4245` | `internal_api` SPIFFE mTLS `/internal`, `public_api` Zitadel JWT `/api/v1` |
| `projects_service` | `internal_https:4265`, `public_http:4264` | `internal_api` SPIFFE mTLS `/internal`, `public_api` Zitadel JWT `/api/v1` |
| `sandbox_rental` | `internal_https:4263`, `public_http:4243` | `internal_api` SPIFFE mTLS `/internal`, `public_api` Zitadel JWT `/api/v1` |
| `secrets_service` | `internal_https:4253`, `public_http:4251` | `internal_api` SPIFFE mTLS `/internal`, `public_api` Zitadel JWT `/api/v1` |
| `source_code_hosting_service` | `internal_https:4262`, `public_http:4261` | `internal_api` SPIFFE mTLS `/internal`, `public_api` Zitadel JWT `/api/v1`, `git_smart_http` Zitadel JWT `/` |
| `spicedb` | `grpc:50051`, `metrics:9090` | `authorization_api` shared-secret gRPC, `metrics` unauthenticated |
| `stalwart` | `http:8090`, `smtp:25` | `admin` operator API, `jmap` Zitadel JWT, `smtp` unauthenticated |
| `temporal` | `frontend_grpc:7233`, `frontend_http:7243`, membership/grpc ports, `metrics:9001`, `pprof:7936` | `frontend` resource protocol, `metrics` operator |
| `verself_web` | `http:4244` | `frontend` unauthenticated HTTP |
| `zitadel` | `http:8085` | `oidc` unauthenticated protocol |

## Databases

Move database requirements and connection limits into component-local Ansible roles or the shared PostgreSQL host role. `postgresql_max_connections` and reserved-superuser settings remain PostgreSQL substrate configuration.

| component | database | owner | connection limit |
|---|---|---|---|
| `billing` | `billing` | `billing` | `30` |
| `electric` | source databases `sandbox_rental`, `notifications_service` | `electric_sandbox_rental`, `electric_notifications` | `25` + `15` source budgets |
| `governance_service` | `governance_service` | `governance_service` | `15` |
| `grafana` | `grafana` | `grafana` | `10` |
| `iam_service` | `iam_service` | `iam_service` | `10` |
| `mailbox_service` | `mailbox_service` | `mailbox_service` | `10` |
| `notifications_service` | `notifications_service` | `notifications_service` | `10` |
| `object_storage_service` | `object_storage_service` | `object_storage_service` | `10` |
| `pomerium` | `pomerium` | `pomerium` | `20` |
| `profile_service` | `profile` | `profile_service` | `10` |
| `projects_service` | `projects_service` | `projects_service` | `10` |
| `sandbox_rental` | `sandbox_rental` | `sandbox_rental` | `30` |
| `source_code_hosting_service` | `source_code_hosting` | `source_code_hosting_service` | `10` |
| `spicedb` | `spicedb` | `spicedb` | `20` |
| `stalwart` | `stalwart` | `stalwart` | `10` |
| `temporal` | `temporal` | `temporal` | `80` |
| `zitadel` | `zitadel` | `zitadel` | `15` |

## SPIFFE And Service Edges

Move workload identities and edges into component-local Ansible roles or the shared SPIRE host role. Service packages can comment/link to the SPIRE entries that authorize their runtime identity.

| owner | current centralized declaration |
|---|---|
| `billing` | Identity `/svc/billing-service`; inbound edge from `sandbox_rental` to `internal_api`. |
| `clickhouse` | Operator identity `/svc/clickhouse-operator`; server identity `/svc/clickhouse-server`. Move into `src/host-configuration/components/clickhouse`. |
| `governance_service` | Identity `/svc/governance-service`; inbound edge from `sandbox_rental` to `internal_api`. |
| `grafana` | Identity `/svc/grafana`; restart `grafana-clickhouse-spiffe-helper`, `grafana`. |
| `iam_service` | Identity `/svc/iam-service`; inbound edge from `source_code_hosting_service` to `internal_api`. |
| `nats` | Identity `/svc/nats`. Move into `src/host-configuration/components/nats`. |
| `object_storage_service` | Identity `/svc/object-storage-service`; outbound edge to Garage S3. |
| `otelcol` | Identity `/svc/otelcol`. Substrate-owned. |
| `projects_service` | Identity `/svc/projects-service`; inbound edge from `source_code_hosting_service` to `internal_api`. |
| `sandbox_rental` | Identity `/svc/sandbox-rental-service`; outbound edges to `billing`, `governance_service`, `secrets_service`. |
| `secrets_service` | Identity `/svc/secrets-service`; outbound edge to OpenBao API; inbound edge from `sandbox_rental`. |
| `source_code_hosting_service` | Identity `/svc/source-code-hosting-service`; outbound edges to `projects_service` and `iam_service`. |
| `temporal` | Identity `/svc/temporal-server`; namespace role bindings for billing and sandbox rental workloads. |

## Workload Substrate Requirements

These are durable host prerequisites currently embedded under `topology_components[].workload`.

| component | current centralized requirements |
|---|---|
| `billing` | Zitadel-owned project, Stripe webhook bootstrap, ClickHouse grants on `verself.billing_events` and `verself.metering`, `/etc/credstore/billing`, Stripe secret refs, ClickHouse CA ref, runtime unit `billing-service`. |
| `company` | `/var/lib/company`, runtime unit `company`. |
| `governance_service` | IAM project audience, ClickHouse grants on `verself.audit_events`, `/etc/credstore/governance-service`, `/var/lib/governance-service`, audit HMAC key, PG password, ClickHouse CA, runtime unit `governance-service`. |
| `iam_service` | Owned IAM project roles, Zitadel Actions/bootstrap browser OIDC app, ClickHouse grants on `verself.domain_update_ledger`, `/etc/credstore/iam-service`, SpiceDB preshared key, ClickHouse CA, runtime unit `iam-service`. |
| `mailbox_service` | Owned mailbox project role, `/etc/credstore/mailbox-service`, `/var/lib/mailbox-service`, Stalwart operator password refs, runtime unit `mailbox-service`. |
| `notifications_service` | IAM project audience, ClickHouse grants on `verself.notification_events`, `/etc/credstore/notifications-service`, PG password, ClickHouse CA, runtime unit `notifications-service`. |
| `object_storage_service` | Owned object-storage project, `/etc/credstore/object-storage-service`, Garage credential refs, admin/runtime units. |
| `profile_service` | IAM project audience, `/etc/credstore/profile-service`, PG password, runtime unit `profile-service`. |
| `projects_service` | IAM project audience, `/etc/credstore/projects-service`, PG password, runtime unit `projects-service`. |
| `sandbox_rental` | Owned sandbox-rental project roles, sandbox VM socket ACL audit, GitHub app bootstrap config, ClickHouse grants on job/cache tables, `/etc/credstore/sandbox-rental`, checkout state dir, Forgejo token/webhook/bootstrap secret refs, runtime units `sandbox-rental-service` and `sandbox-rental-recurring-worker`. |
| `secrets_service` | Owned secrets-service project roles, `/etc/credstore/secrets-service`, OpenBao CA ref, runtime unit `secrets-service`. |
| `source_code_hosting_service` | IAM project audience, `/etc/credstore/source-code-hosting-service`, PG password, Forgejo token, webhook secret, runtime unit `source-code-hosting-service`. |
| `verself_web` | `/etc/credstore/verself-web`, `/var/lib/verself-web`, Electric API secret refs, runtime unit `verself-web`. |

## Network Policy

`nftables_rulesets` in `components.yml` move to authored nftables files under component role `files/` directories. Host-global firewall policy remains under `src/host-configuration/ansible/host-files/etc/nftables.d/`.

| component | ruleset |
|---|---|
| `billing` | `/etc/nftables.d/billing.nft`, table `verself_billing` |
| `company` | `/etc/nftables.d/company.nft`, table `verself_company` |
| `electric` | `/etc/nftables.d/electric.nft`, table `verself_electric` |
| `electric_notifications` | `/etc/nftables.d/electric-notifications.nft`, table `verself_electric_notifications` |
| `garage` | `/etc/nftables.d/garage.nft`, table `verself_garage` |
| `governance_service` | `/etc/nftables.d/governance-service.nft`, table `verself_governance_service` |
| `iam_service` | `/etc/nftables.d/iam-service.nft`, table `verself_iam_service` |
| `mailbox_service` | `/etc/nftables.d/mailbox-service.nft`, table `verself_mailbox_service` |
| `nats` | `/etc/nftables.d/nats.nft`, table `verself_nats` |
| `nomad` | `/etc/nftables.d/nomad.nft`, table `verself_nomad` |
| `notifications_service` | `/etc/nftables.d/notifications-service.nft`, table `verself_notifications_service` |
| `object_storage_service` | `/etc/nftables.d/object-storage-service.nft`, table `verself_object_storage_service` |
| `openbao` | `/etc/nftables.d/openbao.nft`, table `verself_openbao` |
| `profile_service` | `/etc/nftables.d/profile-service.nft`, table `verself_profile_service` |
| `projects_service` | `/etc/nftables.d/projects-service.nft`, table `verself_projects_service` |
| `sandbox_rental` | `/etc/nftables.d/sandbox-rental.nft`, table `verself_sandbox_rental` |
| `secrets_service` | `/etc/nftables.d/secrets-service.nft`, table `verself_secrets_service` |
| `source_code_hosting_service` | `/etc/nftables.d/source-code-hosting-service.nft`, table `verself_source_code_hosting_service` |
| `stalwart` | `/etc/nftables.d/stalwart.nft`, table `verself_stalwart` |
| `verself_web` | `/etc/nftables.d/verself-web.nft`, table `verself_web` |

## Domain And DNS Inputs

Move subdomains and route DNS records into direct host-configuration DNS and HAProxy inputs. `verself_domain`, `company_domain`, operator device records, and physical host IPs remain site-level.

| component | current centralized DNS/domain values |
|---|---|
| `billing` | `billing.api.verself.sh` |
| `forgejo` | `git.verself.sh` |
| `governance_service` | `governance.api.verself.sh` |
| `iam_service` | `iam.api.verself.sh` plus apex auth callback paths |
| `mailbox_service` | `mail.api.verself.sh` |
| `notifications_service` | `notifications.api.verself.sh` |
| `pomerium` | `access.verself.sh` |
| `profile_service` | `profile.api.verself.sh` |
| `projects_service` | `projects.api.verself.sh` |
| `sandbox_rental` | `sandbox.api.verself.sh` |
| `secrets_service` | `secrets.api.verself.sh` |
| `source_code_hosting_service` | `source.api.verself.sh`, `git.verself.sh` |
| `stalwart` | `mail.verself.sh`, MX/SPF/PTR-related mail DNS |
| `verself_web` | `verself.sh` |
| `zitadel` | `auth.verself.sh` |

## Shared Platform Components

| resource | current centralized contents | target package and host config |
|---|---|---|
| `clickhouse` | Native TLS interface, server/operator identities, grants, schema bootstrap inputs, CA distribution, nftables exposure | Host component: `src/host-configuration/components/clickhouse`. Component role owns ClickHouse-specific users, config, migrations, schema bootstrap, grants, and nftables. |
| `electric` and `electric_notifications` | Publication names/tables, DB reader roles, connection pools, storage dirs, credstore dirs, service ports, nftables tables | Host component: `src/host-configuration/components/electric`. Component role owns PostgreSQL publications, reader roles, pool budgets, storage, secrets, and nftables. |
| `openbao` | API/cluster listeners, SPIFFE JWT mount, workload audience, policies, secret bindings, CA distribution, nftables exposure | Host component: `src/host-configuration/components/openbao`. Component role owns OpenBao policies, mounts, bindings, secret inputs, and nftables. |
| `temporal` | Bootstrap namespaces `sandbox-rental-service` and `billing-service`, 24h retention, SPIFFE role bindings, service port topology | Host component: `src/host-configuration/components/temporal-platform`. Component role owns namespaces, retention, role bindings, and listener exposure. |
| `spicedb` | Nomad deployment, Postgres database, gRPC/metrics interfaces | Host component: `src/host-configuration/components/spicedb`. Component role owns Postgres role/database, credentials, and nftables; `nomad.hcl` owns runtime scheduling. |
| `grafana` | Operator route, Postgres database, SPIFFE identity, endpoint | Host component: `src/host-configuration/components/grafana`. Component role owns route, database, identity, plugins, and credentials; `nomad.hcl` owns runtime scheduling if Grafana is Nomad-managed. |
| `pomerium` | Operator route, Postgres database, endpoint | Host component: `src/host-configuration/components/pomerium`. Component role owns route, database, access policy, and identity-provider integration; `nomad.hcl` owns runtime scheduling if Pomerium is Nomad-managed. |
| `stalwart` | Mail routes, SMTP/JMAP endpoints, Postgres database, nftables, DNS records | Host component: `src/host-configuration/components/stalwart`. Component role owns mail protocol DNS, routes, database, credentials, and nftables; `nomad.hcl` owns runtime scheduling if Stalwart is Nomad-managed. |
| `zitadel` | OIDC route, endpoint, Postgres database | Host component: `src/host-configuration/components/zitadel`. Component role owns route, database, bootstrap secrets, actions, and host service files; `nomad.hcl` owns runtime scheduling if Zitadel is Nomad-managed. |
| `forgejo` | Git smart HTTP route, guest-host route, Postgres/database state, webhook/bootstrap resource bindings | Host component: `src/host-configuration/components/forgejo`. Component role owns Git routes, guest host exposure, database, credentials, and webhook/bootstrap bindings. |
| `garage` | Artifact/storage origin, S3 endpoint contracts, service accounts, bucket bindings | Host component: `src/host-configuration/components/garage`. Component role owns storage layout, service accounts, buckets, S3 endpoints, and nftables. |
| `haproxy` | Public edge routes, upstream templates, TLS renewal unit integration, route security headers, body limits, WAF mode | Host component: `src/host-configuration/components/haproxy`. Component role owns certificates, listeners, route files, reload policy, and privileged paths. |
| `nats` | JetStream listener topology, accounts, stream defaults, service identity, nftables exposure | Host component: `src/host-configuration/components/nats`. Component role owns accounts, streams, listeners, identity, and nftables; `nomad.hcl` owns runtime scheduling if NATS is Nomad-managed. |

## Nomad Jobspec API

The public deploy contract is the checked-in Nomad jobspec plus the Bazel target that makes it deployable. A deployable package that Nomad runs owns one `nomad.hcl` file. Nomad's documented jobspec language is HCL, and Nomad parses that HCL into the API JSON payload used for plan, register, inspect, and submit operations:

- <https://developer.hashicorp.com/nomad/docs/job-specification>
- <https://developer.hashicorp.com/nomad/docs/reference/hcl2>
- <https://developer.hashicorp.com/nomad/api-docs/json-jobs>
- <https://developer.hashicorp.com/nomad/api-docs/jobs#parse-job>

Bazel rule attributes describe deployment metadata that does not belong inside the Nomad job schema: component key, Nomad job ID, artifact-producing labels, and submission dependencies.

```starlark
nomad_component(
    name = "nomad_component",
    component = "profile-service",
    job_id = "profile-service",
    job_spec = "nomad.hcl",
    artifacts = {
        "//src/services/profile-service/cmd/profile-service:profile-service_nomad_artifact": "profile-service",
    },
)
```

`nomad.hcl` is the authored source of truth. Artifact stanzas may use `verself-artifact://<output>` as the source. The deploy runner parses HCL with Nomad's HCL parser, resolves that URI after Bazel builds the declared artifact target, uploads the artifact to Garage under a content-addressed key, and sets Nomad getter checksum options before submission.

The resolved Nomad API payload is an ephemeral submit input. It may stamp `Job.Meta` with the resolved artifact digest and Nomad spec digest for no-op detection and evidence. It is not checked in and is not published as a release manifest.

The authored HCL surface stays hermetic and owner-local. HCL variables, var files, shared partials, local environment reads, and cross-directory includes are outside the deploy contract until they are represented as explicit Bazel inputs. Extensibility comes from Nomad's native HCL blocks and the owner package's Bazel target, not from a repository-wide renderer.

The Bazel rule validates direct contracts:

- `job_id` matches `Job.ID`.
- Every `verself-artifact://<output>` reference is declared in `artifacts`.
- Every declared artifact output is referenced by the job.
- `depends_on` references existing `nomad_component` job IDs.
- Dependency ordering is acyclic.

## Host Configuration Ownership

Runnable Ansible for component-specific host state lives under `src/host-configuration/components/<component>/`. Each directory uses standard Ansible role layout and may contain a `nomad.hcl` file when the platform component is Nomad-managed:

```text
src/host-configuration/components/spicedb/
  BUILD.bazel
  nomad.hcl
  defaults/
    main.yml
  files/
    spicedb.nft
  handlers/
    main.yml
  tasks/
    main.yml
  templates/
```

The site playbook imports component roles explicitly. There is no directory scanning, host bundle generation, component manifest compiler, or Ansible included from `src/services/**` or `src/frontends/**`.

Product service packages can include `HOST_CONFIGURATION.md` or comments in `BUILD.bazel` that point to their host-configuration role when they need durable host concerns. The host-configuration files remain the source of truth for Ansible-owned behavior.

```starlark
# Durable host config for this component lives in:
# - //src/host-configuration/components/profile-service
nomad_component(
    name = "nomad_component",
    component = "profile-service",
    job_id = "profile-service",
    job_spec = "nomad.hcl",
)
```

The comments are navigation aids. For example, `src/host-configuration/components/spicedb/nomad.hcl` owns the Nomad job for SpiceDB, and the same directory's Ansible role owns the privileged host mutation for SpiceDB.

## Ansible Boundary

Ansible-owned concerns are authored where Ansible consumes them:

| concern | source of truth |
|---|---|
| component-specific host state | `src/host-configuration/components/<component>/` standard Ansible role layout |
| shared host substrate | `src/host-configuration/ansible/roles/<role>/` |
| nftables exposure | component role `files/*.nft` copied by the role, or shared `src/host-configuration/ansible/host-files/etc/nftables.d/*.nft` for host-global policy |
| HAProxy public edge config | `src/host-configuration/components/haproxy/` and shared HAProxy host inputs |
| DNS reconciliation inputs | `src/host-configuration/` DNS reconciler inputs |
| Postgres roles, databases, and connection limits | component roles for component-owned databases; shared PostgreSQL role for server-wide invariants |
| SPIRE registrations | component roles for component identities and edges; shared SPIRE role for server-wide daemon config |
| ClickHouse schemas and grants | `src/host-configuration/components/clickhouse/migrations/` and the ClickHouse component role |
| OpenBao policies, mounts, and bindings | `src/host-configuration/components/openbao/` |
| Electric PostgreSQL publications and reader roles | `src/host-configuration/components/electric/` |
| runtime users, directories, and systemd units | the component or shared Ansible role that creates or manages them |

There is no generated host bundle and no component-to-host compiler. Ansible is modeled as a deployable executor, not as a side-channel path classifier. Bazel models each Ansible deployable unit's inputs and produces a desired unit digest. The deploy runner compares that digest to the last successful applied unit evidence for the site and runs the unit only when the digest changes.

A Postgres connection-limit change for `profile-service` is edited in the profile-service host-configuration role. That change is reviewed as host configuration, applied by Ansible, and remains independent from the service's Nomad job file.

ClickHouse migrations are owned by `src/host-configuration/components/clickhouse/migrations/`. They are host-configuration deployable inputs because the ClickHouse component role or migration executor applies them and records migration state. Service-owned ClickHouse tables that are part of a product API contract should remain with the service only if the service applies those migrations at deploy time through its own migration target.

## Deployable Units

There is no release bundle API. The deploy runner uses the repository checkout for the requested SHA, Bazel for build inputs and outputs, Garage for immutable artifact storage, ClickHouse for deploy evidence, and executor-specific APIs for apply operations.

The deployment graph is executor-neutral. A deployable unit has:

| field | meaning |
|---|---|
| `unit_id` | Stable deploy evidence key. |
| `executor` | Apply mechanism such as `nomad`, `ansible`, `migration`, or `security_patch`. |
| `desired_digest` | Bazel-produced digest of the desired deployable output and declared inputs. |
| `dependencies` | Unit IDs that must be decided or applied first. |
| `payload` | Executor-specific apply input: resolved Nomad API job, Ansible playbook/tag and vars, migration plan, or security patch policy. |
| `evidence` | ClickHouse rows and OTel spans for decided, skipped, applied, failed, and terminal states. |

Bazel is the only component that decides whether a deployable output has changed. The deploy runner never uses hand-maintained Git pathspecs to decide which component changed. Executors decide whether the live system is already at the desired digest:

- Nomad compares desired `spec_sha256` and `artifact_sha256` against the registered job's `Job.Meta`.
- Ansible compares desired unit digests against successful host-convergence evidence in ClickHouse.
- Security patching uses a freshness policy because upstream package availability changes without a Git commit.
- Migration executors compare migration-set digests against applied migration evidence.

## Artifact Publish And Nomad Submit

`aspect deploy --site=<site> --sha=<sha>` resolves the SHA, enters a clean worktree for that commit when needed, and discovers deployable units with Bazel query over deployment rules. Bazel builds the discovered targets and their declared artifacts. Bazel's action cache determines whether build outputs are reused or rebuilt.

Artifact publication is content-addressed. If the artifact digest already exists in Garage, the publisher verifies the remote object and records evidence. If it is missing, the publisher uploads the object and verifies it before Nomad submission.

Nomad submission uses the resolved per-job API payload:

1. Replace `verself-artifact://<output>` with the Garage getter source for the artifact digest.
2. Set the Nomad getter checksum for each artifact stanza.
3. Compute the artifact digest set and the resolved Nomad spec digest.
4. Stamp the digests in `Job.Meta`.
5. Read the currently registered Nomad job.
6. Skip the job when the registered job is not stopped and the digests match.
7. Run Nomad plan for operator-visible diff.
8. Register the job with Nomad's CAS fence using the prior `JobModifyIndex`.
9. Monitor the resulting deployment and record terminal evidence.

## Deploy Algorithm

The deploy controller records evidence for every unit decision:

1. Resolve the requested Git ref to a commit SHA.
2. Enter a worktree for that exact commit if the requested SHA is not the current clean checkout.
3. Discover deployable targets and build their declared outputs.
4. Compute desired unit digests from Bazel outputs and executor payloads.
5. Evaluate units in Bazel-declared dependency order.
6. Skip units whose desired digest already matches executor evidence or live executor metadata.
7. Apply changed Ansible, security-patch, migration, and Nomad units through their executor APIs.
8. Publish missing Nomad artifacts to Garage under content-addressed keys before Nomad registration.
9. Submit Nomad jobs whose registered `Job.Meta` digests differ from the desired resolved payload.
10. Record unit decisions, artifact publication, executor apply events, terminal health, and deploy outcome evidence in ClickHouse.

Most service deploys touch service code, artifacts, migrations, or `nomad.hcl` only. Those deploys skip unrelated Ansible units because their Bazel-produced digests do not change.

## Cutover Plan

1. Keep checked-in Nomad source as owner-local `nomad.hcl` and parse it to Nomad API JSON during deploy.
2. Use Bazel rule attributes for deployment metadata: executor, unit ID, component name, job ID, artifact labels, migration labels, and dependency ordering.
3. Delete the release bundle API, proposed component manifest, custom deployment JSON, convergence surface, and host-bundle compiler design.
4. Move non-service platform component packages into `src/host-configuration/components/<component>/`.
5. Move durable host concerns from centralized topology into component-local Ansible roles or shared host roles.
6. Add `HOST_CONFIGURATION.md` or `BUILD.bazel` comments in product service packages that point to the host-configuration role for that service.
7. Replace topology-driven Ansible loops with explicit site playbook imports, role-owned vars/files, and direct Ansible tasks.
8. Replace deploy path gating with Bazel-modeled deployable unit digests and executor-specific no-op checks.
9. Delete generated Nomad-spec/profile plans. Standardization happens by copyable examples and review, not rendering.
10. Keep runnable Ansible under `src/host-configuration/`; product service and frontend packages contain links to host configuration rather than Ansible snippets.

## Additional Cleanup

After the component role cutover, the remaining cleanup is concentrated in topology deletion:

1. Leave shared substrate roles in `src/host-configuration/ansible/roles/`: base OS, server tool admission, Nomad agent, PostgreSQL server, SPIRE server, Firecracker, WireGuard, ZFS, operator SSH access, and nftables host baseline.
2. Move component-specific nftables files into each component role's `files/` directory; keep `host-firewall.nft`, `firecracker.nft`, and `nomad.nft` host-global.
3. Replace `topology_*` loops with explicit site playbook imports of component roles. The playbook remains the composition graph.
4. Convert shared helper roles such as SPIRE registration, Zitadel project role binding, OpenBao runtime secret binding, and PostgreSQL database creation into small reusable task APIs invoked by component roles, rather than centralized registries.
5. Delete topology filters, component convergence Python helpers, generated nftables payloads, and generated Nomad profile plans after the direct role sources exist.
6. Keep service-owned PostgreSQL migrations and OpenAPI generation in service packages; host-configuration owns database creation, credentials, grants, network policy, and daemon placement.

## Bespoke Surface Cleanup

| current surface | cleanup target | disposition |
|---|---|---|
| Central topology in `ansible/group_vars/all/topology/{components,endpoints,routes,dns,postgres,spire,ops}.yml` | Component-local Ansible roles or direct files under the host subsystem that consumes the data | Delete once each subsystem has an authored source of truth. |
| `ansible/plugins/filter/topology.py` | Direct role vars/files | Delete after topology files are removed. |
| `playbooks/tasks/component-substrate.yml` and `tasks/component/*.yml` | Role-owned tasks and direct files | Delete per-component orchestration loops. |
| `ansible/files/component_filesystem_convergence.py` | Ansible role tasks for users/directories | Delete unless a role demonstrably needs a small typed helper. |
| `ansible/files/component_postgres_convergence.py` | Postgres role vars and tasks | Delete if native SQL/tasks can express the desired state clearly. |
| `ansible/files/runtime_accounts_convergence.py` | Ansible account tasks | Delete after account state is direct role data. |
| `roles/spire_registrations/files/spire_registration_convergence.py` | SPIRE registration role inputs | Delete if direct role data can drive registration. |
| Generated nftables payloads | Authored nftables files under component role `files/` directories or host-global `ansible/host-files/etc/nftables.d/` | Delete generation. |
| `topology_electric_instances` | Electric role vars/files under `src/host-configuration/components/electric/` | Collapse per-source pseudo-components into direct Electric host config. |
| Generated Nomad specs from profiles | Authored `nomad.hcl` plus Bazel `nomad_component` attributes | Delete Nomad spec generation. |
| Pathspec-to-bundle host detection | Bazel-modeled Ansible deployable units | Removed from the deploy runner. |
| Cloudflare DNS reconciler reading generated topology | Direct DNS input files | Keep the reconciler engine; replace topology loading. |
| HAProxy upstream atomic apply | Authored HAProxy/Nomad service template inputs | Keep atomic apply, validation, rollback, and reload behavior. |

Routine OpenAPI generation, sqlc, generated service clients, and Nomad deployment monitoring remain. They are contract generation or scheduler integration, not host-configuration rendering.

## Target Directory Structure

After the cutover, `src/host-configuration/components` contains non-service platform component packages. Product services and frontends keep application code and authored Nomad jobspecs in their owning packages; their durable host prerequisites live in host-configuration component roles when needed.

`src/host-configuration` owns every durable host concern in the form consumed by Ansible or the host reconciler binary. A platform component directory can be a standard Ansible role and a Bazel package at the same time.

```text
src/host-configuration/
  BUILD.bazel
  binaries/
    BUILD.bazel
    server_tools.bzl
  cmd/
    haproxy-lego-renew/
    haproxy-upstreams-apply/
    reconcile-cloudflare-dns/
    zot-htpasswd/
  components/
    BUILD.bazel
    clickhouse/
      BUILD.bazel
      defaults/main.yml
      files/
      handlers/main.yml
      migrations/
        001_initial_schema.up.sql
      tasks/main.yml
      templates/
    electric/
      BUILD.bazel
      nomad.hcl
      defaults/main.yml
      files/
      handlers/main.yml
      tasks/main.yml
      templates/
    forgejo/
      BUILD.bazel
      nomad.hcl
      defaults/main.yml
      files/
      handlers/main.yml
      tasks/main.yml
      templates/
    garage/
      BUILD.bazel
      defaults/main.yml
      files/
      handlers/main.yml
      tasks/main.yml
      templates/
    grafana/
      BUILD.bazel
      nomad.hcl
      defaults/main.yml
      files/
      handlers/main.yml
      tasks/main.yml
      templates/
    haproxy/
      BUILD.bazel
      nomad.hcl
      defaults/main.yml
      files/
      handlers/main.yml
      tasks/main.yml
      templates/
    nats/
      BUILD.bazel
      nomad.hcl
      defaults/main.yml
      files/
      handlers/main.yml
      tasks/main.yml
      templates/
    openbao/
      BUILD.bazel
      defaults/main.yml
      files/
      handlers/main.yml
      tasks/main.yml
      templates/
    pomerium/
      BUILD.bazel
      nomad.hcl
      defaults/main.yml
      files/
      handlers/main.yml
      tasks/main.yml
      templates/
    spicedb/
      BUILD.bazel
      nomad.hcl
      defaults/main.yml
      files/
      handlers/main.yml
      tasks/main.yml
      templates/
    stalwart/
      BUILD.bazel
      nomad.hcl
      defaults/main.yml
      files/
      handlers/main.yml
      tasks/main.yml
      templates/
    temporal-platform/
      BUILD.bazel
      nomad.hcl
      defaults/main.yml
      files/
      handlers/main.yml
      tasks/main.yml
      templates/
    zitadel/
      BUILD.bazel
      nomad.hcl
      defaults/main.yml
      files/
      handlers/main.yml
      tasks/main.yml
      templates/
  ansible/
    ansible.cfg
    requirements.yml
    prod.ini.example
    group_vars/
      all/
        catalog.yml
        main.yml
        secrets.example.yml
        secrets.sops.yml
        site.yml
        postgres/
          server.yml
        spire/
          server.yml
    host-files/
      etc/
        nftables.conf
        nftables.d/
          firecracker.nft
          host-firewall.nft
          nomad.nft
    playbooks/
      site.yml
      security-patch.yml
      setup-dev.yml
      setup-sops.yml
      operator mutation tooling
    roles/
      base/
      firecracker/
      nomad/
      operator_ssh_access/
      postgresql/
      server_tools/
      spire/
      wireguard/
      zfs/
    rules/
      no_execstart_secrets.py
      no_masterkey_cli.py
      no_psql_object_state.py
      no_secret_without_no_log.py
      no_validate_certs_false.py
      site_orchestrates_only.py
```

Host-configuration directory rules:

- Files are grouped by the Ansible role or host binary that consumes them.
- Platform component directories under `components/` are explicit Ansible roles and Bazel packages.
- Product service and frontend packages do not own runnable Ansible inputs.
- `nomad.hcl` under a platform component is a Nomad executor input, not an Ansible executor input.
- Ansible remains the authority for admitted binary installation, host security, nftables, host users/directories, systemd, Postgres roles/databases, SPIRE registrations, DNS inputs, HAProxy edge config, ClickHouse grants, OpenBao policy, and Electric PostgreSQL publications.
- Nomad remains the authority for running deployable services and Nomad-managed platform components from authored `nomad.hcl` specs.
