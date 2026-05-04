# Component-Owned Host Configuration Inventory

Centralized component configuration remains in `src/host-configuration/ansible/group_vars/all/topology/*.yml`. The post-bootstrap boundary should move component-owned declarations into `src/components/*` package manifests and leave host configuration with global substrate primitives only.

## Source Files

| file | component-specific contents | target ownership |
|---|---|---|
| `routes.yml` | Public/browser/operator/protocol/guest routes, body limits, WAF mode, CORS mode, route path allowlists | Component ingress declarations. Gateway definitions stay substrate-global. |
| `dns.yml` | DNS records duplicating route hostnames | Derived from component ingress declarations. |
| `endpoints.yml` | Ports, bind addresses, exposure class, protocol, interface auth, path prefixes, health probes | Component runtime/interface declarations. |
| `postgres.yml` | Per-component database, owner, role connection limit | Component data dependency declarations. Cluster max/reserved connections stay substrate-global. |
| `spire.yml` | Workload-to-workload SPIFFE authorization edges | Caller-owned service dependency declarations, emitted into SPIRE registration. SPIRE daemon paths and trust-domain stay substrate-global. |
| `components.yml` | Deployment supervisor, identities, nftables files, component order, workload auth/bootstrap, ClickHouse grants, credstore dirs, secret refs, runtime users/units | Component manifests plus typed substrate requirement providers. |
| `ops.yml` | Component subdomains/domains, Electric publication instances, Temporal namespaces/role bindings | Component manifests. Site domain, WireGuard, artifact storage, bare-metal host facts stay substrate-global. |
| `clusters.yml` | Temporal port topology; Garage cluster topology | Temporal moves with component if Nomad-owned. Garage stays substrate-global. |

## Ingress Routes

All `topology_routes` entries are component-owned. `public_haproxy`, `direct_smtp`, and `firecracker_host` are gateway resources and should remain substrate-global.

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

## Component Runtime Interfaces

These `topology_endpoints` entries describe component-owned ports, bindings, protocols, auth mode, path prefixes, and probes.

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

Move component database requirements and connection limits into component manifests. `postgresql_max_connections` and reserved-superuser settings remain PostgreSQL substrate configuration.

| component | database | owner | connection limit |
|---|---|---|---|
| `billing` | `billing` | `billing` | `30` |
| `electric` | attached to `sandbox_rental` publication | `electric` | `25` |
| `electric_notifications` | attached to `notifications_service` publication | `electric_notifications` | `15` |
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

Move workload identities into component manifests. Move edges to caller-owned dependency declarations so service-to-service authorization is declared where the call is made.

| owner | current centralized declaration |
|---|---|
| `billing` | Identity `/svc/billing-service`; inbound edge from `sandbox_rental` to `internal_api`. |
| `clickhouse` | Operator identity `/svc/clickhouse-operator`; server identity `/svc/clickhouse-server`. Substrate-owned. |
| `governance_service` | Identity `/svc/governance-service`; inbound edge from `sandbox_rental` to `internal_api`. |
| `grafana` | Identity `/svc/grafana`; restart `grafana-clickhouse-spiffe-helper`, `grafana`. |
| `iam_service` | Identity `/svc/iam-service`; inbound edge from `source_code_hosting_service` to `internal_api`. |
| `nats` | Identity `/svc/nats`. Substrate-owned resource. |
| `object_storage_service` | Identity `/svc/object-storage-service`; outbound edge to Garage S3. |
| `otelcol` | Identity `/svc/otelcol`. Substrate-owned. |
| `projects_service` | Identity `/svc/projects-service`; inbound edge from `source_code_hosting_service` to `internal_api`. |
| `sandbox_rental` | Identity `/svc/sandbox-rental-service`; outbound edges to `billing`, `governance_service`, `secrets_service`. |
| `secrets_service` | Identity `/svc/secrets-service`; outbound edge to OpenBao API; inbound edge from `sandbox_rental`. |
| `source_code_hosting_service` | Identity `/svc/source-code-hosting-service`; outbound edges to `projects_service` and `iam_service`. |
| `temporal` | Identity `/svc/temporal-server`; namespace role bindings for billing and sandbox rental workloads. |

## Workload Substrate Requirements

These are the component-owned host prerequisites currently embedded under `topology_components[].workload`.

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

## Component Network Policy

`nftables_rulesets` in `components.yml` are component-owned exposure declarations. The substrate nftables role should consume generated rules from component manifests.

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

Move subdomains and route DNS records into component ingress declarations. `verself_domain`, `company_domain`, operator device records, and physical host IPs remain site-level.

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

## Service-Adjacent Resources

| resource | current centralized contents | target ownership |
|---|---|---|
| `electric` | Publication name/tables for `sandbox_rental`, DB role, connection pool, storage dir, credstore dir, service port, nftables table | Move to `sandbox_rental` as a projection/sync dependency unless Electric becomes its own component manifest consumed by dependents. |
| `electric_notifications` | Publication name/tables for `notifications_service`, DB role, connection pool, storage dir, credstore dir, service port, nftables table | Move to `notifications_service` as a projection/sync dependency unless Electric becomes its own component manifest consumed by dependents. |
| `temporal` | Bootstrap namespaces `sandbox-rental-service` and `billing-service`, 24h retention, SPIFFE role bindings, service port topology | Move namespaces and role bindings to `sandbox_rental` and `billing`; keep Temporal server cluster mechanics in `src/components/temporal-platform`. |
| `spicedb` | Nomad deployment, Postgres database, gRPC/metrics interfaces | Move fully into `src/components/spicedb`. |
| `grafana` | Operator route, Postgres database, SPIFFE identity, endpoint | Move into `src/components/grafana` if it remains Nomad/component-managed; otherwise classify as substrate operator app. |
| `pomerium` | Operator route, Postgres database, endpoint | Move into `src/components/pomerium` if it remains component-managed; otherwise classify as substrate operator app. |
| `stalwart` | Mail routes, SMTP/JMAP endpoints, Postgres database, nftables, DNS records | Move mail protocol declarations into `src/components/stalwart` if Stalwart becomes component-managed; leave bare SMTP gateway primitive substrate-global. |
| `zitadel` | OIDC route, endpoint, Postgres database | Move public OIDC origin and database requirement into `src/components/zitadel` if Zitadel becomes component-managed; leave host bootstrap credentials in substrate. |

## Recommended Manifest Shape

Each component package should own one typed manifest exported through a Bazel provider:

```json
{
  "component": "billing",
  "deployment": {"supervisor": "nomad", "job_id": "billing-service"},
  "interfaces": {},
  "ingress": [],
  "postgres": {},
  "spiffe": {},
  "clickhouse": {},
  "runtime": {},
  "secrets": [],
  "bootstrap": []
}
```

Host configuration should consume the union of these providers as data and converge substrate state. It should stop authoring component routes, component ports, component databases, SPIFFE edges, runtime users, service-specific credstore paths, and service-specific firewall files.
