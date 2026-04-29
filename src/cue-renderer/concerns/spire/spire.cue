// Package spire owns every SPIRE-flavoured concept the platform models:
// the per-deployment server/agent/bundle config, the catalog of auth
// kinds that need (or don't need) a SPIRE registration, and the
// derived values (agent SPIFFE ID, bundle endpoint URL) that other
// renderers consume.
//
// schema.cue knows none of this. The instance binding in
// instances/local/topology.cue imports this package and pins
// #Edge.auth values to the catalog keys; instances/local/config.cue
// imports it to bind config.spire to #Config.
package spire

import s "github.com/verself/cue-renderer/schema"

// #Config is the per-deployment SPIRE configuration. Computed fields
// (agent_id, bundle_endpoint_url) live alongside the inputs so the
// renderer reads them directly instead of re-deriving in Go.
#Config: {
	trust_domain:        string & !=""
	server_bind_address: s.#Host
	server_socket_path:  string & =~"^/"
	agent_socket_path:   string & =~"^/"
	workload_group:      string & !=""
	agent_id_path:       string & =~"^/"

	// Computed: the agent's SPIFFE ID. The renderer reads this verbatim
	// instead of re-doing the concatenation. Declared as a defaulted
	// string so gengotypes types it cleanly; the default is the only
	// admissible value because the right-hand side is concrete.
	agent_id: string | *"spiffe://\(trust_domain)\(agent_id_path)"

	// Bundle endpoint coordinates. The component + endpoint names point
	// at where SPIRE actually listens in topology.cue; the renderer uses
	// them to look up the bound port. Scheme defaults to https; emitted
	// as a field so the renderer doesn't sprintf a literal "https://".
	bundle_endpoint_bind_address: s.#Host
	bundle_endpoint_scheme:       "https" | "http" | *"https"
	bundle_endpoint_component:    string & !=""
	bundle_endpoint_endpoint:     string & !=""

	// Server endpoint coordinates, same shape as the bundle endpoint.
	server_component: string & !=""
	server_endpoint:  string & !=""
}

// #AuthKind is the per-kind metadata each entry in the spiffe_auth_kinds
// catalog carries. spiffe_bearing flags edges that need a SPIRE
// registration (mTLS using x509-SVIDs); other auth modes (Zitadel JWT,
// shared secrets, operator credentials) do not.
#AuthKind: {
	name:           string & !=""
	spiffe_bearing: bool
}

// kinds is the catalog of every auth value an #Edge or #Interface may
// take in this codebase. Adding a new auth mode means adding an entry
// here — the topology binding pins #Edge.auth to one of these keys, so
// an unknown value fails CUE evaluation. The renderer reads
// kinds[edge.auth].spiffe_bearing to decide whether to emit a SPIRE
// registration entry.
kinds: [name=string]: #AuthKind & {"name": name}
kinds: {
	none: {spiffe_bearing:          false}
	zitadel_jwt: {spiffe_bearing:   false}
	spiffe_mtls: {spiffe_bearing:   true}
	shared_secret: {spiffe_bearing: false}
	operator: {spiffe_bearing:      false}
}
