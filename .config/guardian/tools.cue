// Guardian tool catalog. Each entry names the executable `guardian run <tool>`
// resolves from the tool root (<root>/<name>/bin/<executable>): the golden image
// closure at $GUARDIAN_TOOLS_ROOT, or the Bazel-materialized dev root under
// .verself/tools/root. The upstream pin (url + sha256) lives once in the Bazel
// module graph (//src/guardian-specification:guardian_tools.MODULE.bazel), never
// restated here.
tools: bazel: platforms: {
	"linux/amd64": executable:  "bazel"
	"darwin/arm64": executable: "bazel"
}
