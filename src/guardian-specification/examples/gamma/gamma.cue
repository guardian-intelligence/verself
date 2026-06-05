package gamma

import guardian "guardianintelligence.org/guardian-specification/cue/guardian/v1alpha1"

guardian.#FlyProcedure

kind: "FlyProcedure"

staticConfig: {
	baseURL:        "https://gamma.guardianintelligence.org"
	credentialsRef: "gamma-credentials"
}

board: {
	access: argv: [
		"ssh",
		"-T",
		"-o", "BatchMode=yes",
		"-o", "StrictHostKeyChecking=yes",
		"-o", "UserKnownHostsFile=/home/ubuntu/.ssh/known_hosts",
		"ubuntu@206.223.228.87",
		"true",
	]
	upload: {
		run: argv: [
			"sh",
			"-c",
			"""
				set -eu
				remote_dir=/home/ubuntu/.local/state/guardian/uploads/current
				ssh -T -o BatchMode=yes -o StrictHostKeyChecking=yes -o UserKnownHostsFile=/home/ubuntu/.ssh/known_hosts ubuntu@206.223.228.87 "mkdir -p $remote_dir"
				rsync -a -- "$GUARDIAN_UPLOAD_BUNDLE" "ubuntu@206.223.228.87:$remote_dir/upload.tar.zst"
				""",
		]
		verify: argv: [
			"ssh",
			"-T",
			"-o", "BatchMode=yes",
			"-o", "StrictHostKeyChecking=yes",
			"-o", "UserKnownHostsFile=/home/ubuntu/.ssh/known_hosts",
			"ubuntu@206.223.228.87",
			"sha256sum /home/ubuntu/.local/state/guardian/uploads/current/upload.tar.zst",
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
