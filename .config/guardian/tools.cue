tools: bazel: platforms: "linux/amd64": {
	ref:        "oci.verself.sh/tools/bazel@sha256:a667454f3f4f8878df8199136b82c199f6ada8477b337fae3b1ef854f01e4e2f"
	executable: "bazel"
	admission:  "admitted"
	mirrors: [
		"https://releases.bazel.build/9.1.0/release/bazel-9.1.0-linux-x86_64",
	]
}
