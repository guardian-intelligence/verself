# Public Origins

Public origins are declared in `src/host-configuration/ansible/group_vars/all/topology/routes.yml`. `public_haproxy` routes are rendered into HAProxy host/path dispatch rules by the host-configuration role.

The product apex (`<domain>`) serves the authenticated console, docs, and policy from the TanStack Start app. Public service APIs use service subdomains under `<service>.api.<domain>`, including `billing.api.<domain>`, `sandbox.api.<domain>`, and `identity.api.<domain>`. Browser code uses same-origin server functions for product workflows; server functions attach service credentials when calling public service APIs.

HAProxy reaches Nomad-supervised public origins through `/etc/haproxy/maps/upstreams.map`. The map is reconciled from Nomad native service registrations after deploy health checks complete. The map key is mechanical: a Nomad service named `<jobid>-<endpoint>` maps to `VERSELF_UPSTREAM_<JOBID>_<ENDPOINT>` after uppercasing and replacing dashes with underscores. Topology routes use the same component/endpoint key shape through the target interface, so HAProxy route definitions do not depend on committed static application ports.

`src/host-configuration/edgecontract` compiles authored route topology, endpoint topology, authored Nomad jobs, and HAProxy defaults into the public edge plan. `aspect operator edge --action=render` rewrites the HAProxy template and map artifacts from that plan. `aspect operator edge --action=check` validates the generated artifacts match the plan, and `aspect operator edge --action=manifest --format=json` emits route backends, Nomad upstream keys, and HAProxy GUID-bearing objects.
