// Package bazelnftables emits the Starlark manifest the cue-renderer
// BUILD.bazel reads to wire one genrule per nftables ruleset and one
// `write_source_files` entry per emitted file.
//
// The list is the join of:
//
//   - Three static entries: the host firewall ruleset, the nftables main
//     config, and the verself-firewall.target unit. These have no CUE
//     correspondence — they're invariants of the platform and live in
//     the nftables package's path constants.
//   - One entry per CUE topology.nftables.rulesets key.
//
// CUE owns the dynamic set; adding a ruleset to topology.cue regenerates
// this file via `aspect codegen run --kind=topology`, and BUILD.bazel picks up the
// new entry on next build with no hand-edit.
package bazelnftables

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/verself/cue-renderer/internal/load"
	"github.com/verself/cue-renderer/internal/render"
	"github.com/verself/cue-renderer/internal/render/nftables"
	"github.com/verself/cue-renderer/internal/render/projection"
)

const outputPath = "src/cue-renderer/nftables_files.bzl"

type Renderer struct{}

func (Renderer) Name() string { return "bazel_nftables" }

// entry is one row in the emitted NFTABLES_RENDERED list.
type entry struct {
	// slug is the genrule label suffix and the Bazel-friendly identifier
	// — e.g. `billing` produces `:nftables_billing_render`.
	slug string

	// renderedPath is the repo-relative path the nftables renderer
	// writes to (and the value passed to `cue-renderer render --path`).
	renderedPath string

	// outPath is the package-relative path the genrule produces under
	// //src/cue-renderer; write_source_files copies this into the
	// rendered tree under //src/platform.
	outPath string
}

func (Renderer) Render(_ context.Context, loaded load.Loaded, out render.WritableFS) error {
	entries := []entry{
		{slug: "nftables_conf", renderedPath: nftables.ConfPath},
		{slug: "host_firewall", renderedPath: nftables.HostFirewallPath},
		{slug: "verself_firewall_target", renderedPath: nftables.FirewallTargetPath},
	}
	if loaded.Topology.Nftables.Firecracker.Table != "" {
		entries = append(entries, entry{slug: "firecracker_forward", renderedPath: nftables.FirecrackerChainPath})
	}
	for _, name := range sortedKeys(loaded.Topology.Nftables.Rulesets) {
		ruleset := loaded.Topology.Nftables.Rulesets[name]
		entries = append(entries, entry{slug: name, renderedPath: nftables.RulesetPath(ruleset.Target)})
	}
	for i := range entries {
		entries[i].outPath = packageOutPath(entries[i].renderedPath)
	}

	var b strings.Builder
	b.WriteString(projection.Header)
	b.WriteString("\n")
	b.WriteString("# (genrule slug, repo-relative rendered path, package-relative genrule out)\n")
	b.WriteString("NFTABLES_RENDERED = [\n")
	for _, e := range entries {
		fmt.Fprintf(&b, "    (%q, %q, %q),\n", e.slug, e.renderedPath, e.outPath)
	}
	b.WriteString("]\n")
	return out.WriteFile(outputPath, []byte(b.String()))
}

// packageOutPath strips the cue-renderer package's known sibling prefix
// off a rendered path. The genrule output for
// `src/platform/ansible/share/rendered/etc/nftables.d/billing.nft`
// lands inside //src/cue-renderer at `rendered/etc/nftables.d/billing.nft`,
// keeping the genrule outputs scoped to the renderer's own package.
func packageOutPath(renderedPath string) string {
	const prefix = "src/platform/ansible/share/"
	return strings.TrimPrefix(renderedPath, prefix)
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
