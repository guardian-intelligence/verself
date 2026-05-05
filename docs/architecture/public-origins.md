# Public Origins

Public origins are owned by the service, frontend, or component that exposes
them. The edge component consumes those owner-local route declarations plus
Nomad service registration; public routes are no longer centralized in host
Ansible topology.

The product apex (`<domain>`) serves the authenticated console, docs, and policy from the TanStack Start app. Public service APIs use service subdomains under `<service>.api.<domain>`, including `billing.api.<domain>`, `sandbox.api.<domain>`, and `iam.api.<domain>`. Browser code uses same-origin server functions for product workflows; server functions attach service credentials when calling public service APIs.

HAProxy reaches Nomad-supervised public origins through `/etc/haproxy/maps/upstreams.map`. The map is reconciled from Nomad native service registrations after deploy health checks complete. The map key is mechanical: a Nomad service named `<jobid>-<endpoint>` maps to `VERSELF_UPSTREAM_<JOBID>_<ENDPOINT>` after uppercasing and replacing dashes with underscores. Topology routes use the same component/endpoint key shape through the target interface, so HAProxy route definitions do not depend on committed static application ports.

Public edge behavior is authored in owner-local route metadata, service-owned
Nomad jobs, and HAProxy templates. Deploy consumes those authored inputs and
Nomad's native service catalog.
