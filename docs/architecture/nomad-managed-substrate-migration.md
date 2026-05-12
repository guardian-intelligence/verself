# Nomad-Managed Platform Substrate

## Target Boundary

Host bootstrap owns the minimal machine state required to start Nomad and keep
operator recovery available:

- Ubuntu host baseline, admitted server-tool catalog, host users, directories,
  and base package state.
- Break-glass SSH, WireGuard recovery networking, nftables foundation, and site
  SOPS materialization.
- ZFS pools, datasets, and privileged Firecracker host substrate.
- SPIRE server/agent bootstrap and node identity.
- HAProxy process, public TLS material, and the static edge configuration needed
  before workload allocations exist.
- ClickHouse server plus the initial deploy-evidence schema.
- Nomad server/client.
- Devtools installed through the controller/host dev-tool path.

Nomad owns platform daemons, product services, frontends, platform reconcilers,
database/schema convergence after bootstrap, and runtime endpoint allocation.
PostgreSQL, Garage, OpenBao, Zitadel, NATS, TigerBeetle, Verdaccio, Zot,
Stalwart, Forgejo, Grafana, OTel collector, Electric, SpiceDB, Temporal, product
services, and frontends are deployable Nomad components.

Pomerium remains a manual operator-access handoff outside regular deploy until
the SSH listener boundary has a stronger lockout recovery model.

## ClickHouse Split

ClickHouse server remains host bootstrap because deploy evidence must exist
before the first Nomad rollout can be considered observable. Bootstrap applies
only `001_initial_schema.up.sql`, which creates the minimum tables needed for
deploy evidence, logs, traces, metrics, and host observation.

Later ClickHouse migrations are Nomad-managed batch jobs. Components and
services that write ClickHouse require the migration resource published by the
ClickHouse migration component.

## PostgreSQL

PostgreSQL can be fully Nomad-managed. Nomad and the deploy controller do not
require PostgreSQL before job submission. The host substrate prepares the
postgres operating-system user, data directories, log directories, and socket
directory. A Nomad `postgres` job owns the server process and runs a prestart
initializer when the data directory has no `PG_VERSION`.

Database, role, grant, and connection-limit convergence move from host Ansible
into a Nomad batch component generated from owner-local metadata. Service-owned
PostgreSQL migrations run as owner-local Nomad batch or prestart tasks before
the service allocation that consumes the schema.

## Artifact Origin

Garage backs private Nomad artifact delivery. Moving Garage to Nomad requires a
pre-artifact deploy wave. The deploy controller first submits no-artifact
substrate jobs that run from admitted host binaries, waits for Garage health and
artifact credential convergence, then publishes content-addressed artifacts and
submits normal artifact-backed jobs.

This wave is the hard dependency for removing Garage from host bootstrap. It is
also the natural place for early infrastructure jobs that can run from the
server-tool catalog and should exist before product services, such as
PostgreSQL and OTel collector.

## Deploy Waves

1. Host bootstrap via Ansible:
   host foundation, recovery access, nftables, WireGuard, ZFS, Firecracker host
   substrate, SPIRE, HAProxy, ClickHouse initial schema, Nomad, and devtools.

2. Nomad pre-artifact substrate:
   Garage, PostgreSQL, OTel collector, and other no-artifact components that
   run from the admitted server-tool catalog.

3. Nomad platform substrate:
   OpenBao, Zitadel, NATS, TigerBeetle, Zot, Verdaccio, Stalwart, Forgejo,
   Grafana, Electric, SpiceDB, Temporal, ClickHouse migrations, and
   component-owned reconcilers.

4. Nomad product surface:
   Go services, TanStack frontends, background workers, and public route
   helpers.

5. Nomad edge reconciliation:
   HAProxy upstream-map reconciliation from Nomad service discovery.

## Directory Structure

- `src/host/` owns host bootstrap roles, site facts, SOPS bootstrap material,
  host binary admission, bootstrap ClickHouse schema, and Nomad agent
  convergence.
- `src/infrastructure-components/<name>/` owns each platform component's `BUILD.bazel`,
  `nomad.hcl`, runtime users, directories, SPIRE identities, endpoint exports,
  credential bindings, migrations, and reconcilers.
- `src/services/<name>/` owns product service `nomad.hcl`, PostgreSQL
  migrations, ClickHouse projections, service-to-service clients, and public
  route metadata. Canonical API contracts live under `src/contracts`.
- `src/websites/apps/<name>/` owns frontend `nomad.hcl`,
  server-function bindings, route metadata, browser canaries, and static/runtime
  assets.
- `src/tools/deployment/` owns deploy graph resolution, artifact publication,
  pre-artifact wave submission, Nomad rollout monitoring, and ClickHouse deploy
  evidence.
- `.aspect/` exposes the typed operator commands for bootstrap, deploy,
  evidence checks, database queries, and observability.

## File Migration Map

- `src/host/ansible/playbooks/site.yml`
  removes platform daemon systemd roles from the bootstrap play. It keeps host
  foundation, recovery networking, SPIRE, HAProxy, ClickHouse initial schema,
  Nomad, devtools, and privileged substrate.

- `src/infrastructure-components/postgresql/`
  owns PostgreSQL host-state preparation while the server process is supervised
  by the component Nomad job. Initialization and readiness checks converge
  toward component-local Nomad lifecycle tasks.

- `src/host/ansible/playbooks/tasks/component-substrate.yml`
  shrinks as runtime accounts, PostgreSQL bindings, ClickHouse grants, OpenBao
  policies, and endpoint files move into owner-local Nomad components.

- `src/infrastructure-components/postgresql/`
  becomes the PostgreSQL Nomad component: server job, init prestart task,
  role/database convergence batch job, endpoint metadata, and component
  descriptor provides for `postgres:server` and `postgres:bindings`.

- `src/infrastructure-components/garage/`
  becomes the artifact-origin Nomad component. It must run in the pre-artifact
  wave from host-admitted binaries and publish `artifact-origin` before normal
  artifact publication.

- `src/infrastructure-components/openbao/`
  owns the OpenBao server job, unseal/recovery ceremony hooks, SPIRE JWT auth
  roles, runtime secret seeding, and credential-store reconciliation.

- `src/infrastructure-components/zitadel/`
  owns the Zitadel Nomad job, setup/init tasks, project/application
  reconciliation, OAuth application exports, and service discovery metadata.

- `src/infrastructure-components/nats/`, `src/infrastructure-components/tigerbeetle/`,
  `src/infrastructure-components/zot/`, `src/infrastructure-components/verdaccio/`,
  `src/infrastructure-components/stalwart/`, `src/infrastructure-components/forgejo/`,
  `src/infrastructure-components/grafana/`, and `src/infrastructure-components/electric/`
  each receive a component-local Nomad descriptor, host-volume declarations,
  endpoint metadata, and migration/reconcile jobs specific to the component.

- `src/infrastructure-components/spicedb/` and `src/infrastructure-components/temporal-platform/`
  already have Nomad component shape. The migration removes remaining host
  systemd ownership and leaves their database/schema prerequisites as explicit
  Nomad graph requirements.

- `src/infrastructure-components/clickhouse/`
  keeps the server in bootstrap and owns post-initial migration jobs. The
  package continues to exclude `001_initial_schema.up.sql` from the Nomad
  migration bundle.

- `src/infrastructure-components/haproxy/`
  keeps the HAProxy process in bootstrap and owns deployable upstream-map
  reconciliation from Nomad service discovery.

- `src/tools/deployment/`
  owns deploy-wave semantics, dependency validation across wave ordering,
  health checks for the Garage S3 artifact origin before post-bootstrap artifact
  publication, and ClickHouse evidence for each wave.

## Endpoint Allocation

Nomad-managed components publish endpoints through Nomad service registration
or owner-local endpoint files rendered from Nomad allocation data. Consumers of
Nomad-managed components use `nomadService` or a named credential-store binding.
Committed `127.0.0.1:<port>` conventions remain only for bootstrap listeners
owned by the bootstrap component.

## Manual E2E Verification

The migration is accepted through live host and deploy evidence rather than
unit-test coverage:

- Fresh or wiped single-node host bootstrap completes with only bootstrap-ring
  systemd units active.
- `aspect deploy --site=prod --sha=HEAD` submits the pre-artifact wave, observes
  Garage health, publishes artifacts, and completes the remaining Nomad graph.
- ClickHouse contains deploy evidence for each wave, artifact publication, Nomad
  job submission, and health decision under a single deploy correlation key.
- HAProxy serves public routes through Nomad-discovered upstreams.
- Product browser flows exercise Zitadel login, Electric-backed console reads,
  service writes to PostgreSQL, ClickHouse evidence writes, Forgejo access,
  Stalwart mailbox paths, and billing flows that touch TigerBeetle.
