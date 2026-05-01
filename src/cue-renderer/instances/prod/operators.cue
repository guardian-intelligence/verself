package topology

// operators declares the trusted set of operator devices that can
// authenticate to the bare-metal node over wg-ops + OpenBao-issued SSH
// certs. New devices are added via PR by an already-onboarded operator;
// see docs/architecture/onboarding-device-or-vm.md for the flow.
//
// Adding an operator:
//   1. New device runs `aspect operator onboard --device=<name>` and
//      generates ed25519 SSH + WireGuard keypairs locally.
//   2. Trusted operator opens a PR that adds the device entry below.
//   3. PR merges + deploy lands; the wg-ops peer list reconciles via
//      the CUE projection in instances/prod/config.cue.
//   4. New device fetches /.well-known/verself-* and signs its first
//      cert via OIDC.
//
// Removing a device: delete the entry, deploy. The wg peer disappears
// from the kernel on the next substrate convergence.
//
// `wg_address` allocations are explicit (not auto-derived) so that
// removing a device never silently shifts an unrelated address.
config: operators: {
	founder: {
		// Single platform-org operator today. Project-role-based access
		// gating happens at the OpenBao OIDC role level.
		zitadel_user_id: ""
		devices: {
			"shovon-mbp": {
				name:       "shovon-mbp"
				wg_pubkey:  "AoVgh4aWFK5Gi7HBdqIzTea37aa5SaemU4Pyk92Nglc="
				wg_address: "10.66.66.2"
			}
		}
	}
}

// Workload pool. Slot priv keys live in OpenBao KV; the wireguard role
// reads slot pubkeys from a credstore file written by the openbao role
// on first deploy. Bumping slot_count grows the pool on the next deploy.
config: workloads: pool: {
	slot_count:                   4
	slot_address_base:            "10.66.66.100"
	enroll_secret_id_ttl_seconds: 900
	workload_token_ttl_seconds:   86400
}
