# Component-Owned Deployment Inventory

- `src/host` is reserved for non-deployable bootstrap substrate: OS baseline, break-glass SSH, base nftables policy, ZFS/storage pools, WireGuard host networking, containerd, Nomad agent, SPIRE server/agent bootstrap, Firecracker host plumbing, and the minimum database/artifact transport needed to recover or first-start the box.

- Beyond `src/host/sites/<environment>`, components do not know which environment they are in.

- Normal CI deploys should not mutate `src/host` state. Host bootstrap is a manual operator action used for first boot, reprovisioning, or deliberate substrate maintenance.

- Platform daemons live under `src/components/<name>`, even when they are foundational to the product. ClickHouse, Garage, OpenBao, Zitadel, Pomerium, Grafana, Otelcol, NATS, Forgejo, Verdaccio, Zot, Stalwart, TigerBeetle, Electric, HAProxy, SpiceDB, and Temporal are deployable platform components, not host bootstrap.

- Product services live under `src/services/<name>`, and frontends live under `src/frontends/<name>`. Their runtime users, directories, secrets, firewall snippets, SPIRE workload identities, database ownership, ClickHouse grants, migrations, and route metadata are owned locally by that service or frontend.

- Third-party SaaS reconciliation lives under `src/integrations/<vendor>/<purpose>`. Cloudflare DNS, Resend domain setup, Zitadel project/application bootstrap, Stripe webhooks, and GitHub App setup are deployable reconcilers, not host Ansible tasks.

- The long-term unit abstraction is `deployable_unit(...)`: one Bazel-owned descriptor with an executor, payload, digest inputs, logical `requires`, logical `provides`, and optional site scope.

- Executors are implementation details behind the same unit contract. Expected executors include `nomad`, `ansible`, `migration`, `external_saas`, and `provision_terraform`.

- `requires` and `provides` form the deployment graph. The graph should describe resources such as `nomad:job:<name>`, `postgres:db:<name>`, `spire:identity:<name>`, `clickhouse:user:<name>`, `dns:cloudflare:<host>`, or `zitadel:project:<name>`.

- Bazel validates and materializes descriptors. The deploy controller walks the descriptor graph, asks each executor whether the unit is already at the desired digest, applies only changed units, and records evidence per unit.

- Nomad owns service and component port allocation. Service-to-service and edge routing should consume Nomad service registration instead of static endpoint maps.

- Host Ansible may reserve only substrate ports: SSH, Nomad, SPIRE, WireGuard, base Postgres if host-managed, and other true bootstrap listeners. Product and platform component listeners belong in Nomad jobs.

- HAProxy and Pomerium should be deployed components. Their route configuration should be generated from component/service route metadata plus Nomad service discovery.

- SPIRE has two layers: host bootstrap installs and starts the SPIRE server/agent and establishes node identity; each component or service owns its workload identities in its own Bazel metadata.

- Postgres has two layers: host bootstrap may install and configure the server; each component or service owns its databases, roles, connection limits, grants, and migrations.

- ClickHouse is a platform component. It should not be required before the deploy controller can deploy components; deploy evidence must support local buffering or delayed flush until ClickHouse is healthy.

- Garage is a platform component. Because Garage currently backs Nomad artifact delivery, first deploy needs a bootstrap artifact transport that does not depend on Garage already running. After Garage is healthy, artifact delivery can move to Garage.

- OpenBao is a platform component with a bootstrap edge. Manual host state should cover only the minimum root/unseal/recovery path; runtime secret seeding and workload policies are deployable component/integration units.

- The centralized topology directory is gone. Former topology data is dissolved into site bootstrap config, component-local metadata, service-local metadata, frontend-local metadata, and integration units.

- The retired deployment topology variable API includes `topology_endpoints`, `topology_routes`, `topology_clusters`, `topology_wireguard`, and `topology_artifacts`. New code must not recreate those names as compatibility shims.

- Endpoint facts are represented in three places: host bootstrap listeners in `src/host/sites/<site>/vars.yml`, component-local listener defaults in `src/components/<name>/`, and dynamic workload endpoints in Nomad service registrations.

- `components.yml` becomes component/service-local deployment metadata.

- `postgres.yml` splits into host Postgres server tuning plus owner-local database/role/grant declarations.

- `spire.yml` splits into host SPIRE server/agent config plus owner-local workload identity declarations.

- `dns.yml` becomes Cloudflare integration unit inputs.

- `routes.yml` becomes route metadata owned by the service/frontend/component that exposes the route.

- `clusters.yml` becomes component-local config for Garage, Temporal, or any other multi-process component.

- `ops.yml` splits into site-level bootstrap facts, operator break-glass config, and component-local settings.

- `catalog.yml` should disappear once Bazel-owned tool and artifact metadata fully replace generated Ansible catalog data.

- The target reader experience is that changing a component or service requires editing only that owner directory and, when necessary, its explicitly referenced integration directory. No central topology loop should need a coordinated edit.
