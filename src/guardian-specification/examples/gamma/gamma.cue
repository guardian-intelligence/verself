package gamma

import guardian "guardianintelligence.org/guardian-specification/cue/guardian/v1alpha1"

guardian.#FlyProcedure

kind: "FlyProcedure"

staticConfig: {
	baseURL:        "https://gamma.guardianintelligence.org"
	credentialsRef: "gamma-credentials"
}

board: {
	substrate: {
		stateDir: "/var/lib/guardian"
	}
	access: ssh: {
		host:           "206.223.228.87"
		port:           22
		user:           "ubuntu"
		knownHostsFile: "~/.ssh/known_hosts"
		wireguardFallback: {
			host:      "10.66.67.1"
			port:      2222
			interface: "wg-ops"
		}
	}
	seed: {
		targetRoot: "/var/lib/guardian/seeds"
		paths: [
			{
				source: "bazel-bin/src/guardian-specification/cli/cmd/guardian/guardian_/guardian"
				target: "bin/guardian"
				mode:   "0755"
			},
			{
				source: "src/infrastructure-components/openbao/nomad.hcl"
				target: "jobs/openbao.nomad.hcl"
				mode:   "0644"
			},
			{
				source: "src/services/deployment-service/nomad.hcl"
				target: "jobs/deployment-service.nomad.hcl"
				mode:   "0644"
			},
		]
	}
}

nomad: {
	address:   "http://127.0.0.1:4646"
	namespace: "default"
	jobs: [
		{
			path: "src/infrastructure-components/openbao/nomad.hcl"
			requiredFor: ["recovery"]
		},
		{
			path: "src/services/deployment-service/nomad.hcl"
			requiredFor: ["deploy"]
		},
	]
}
