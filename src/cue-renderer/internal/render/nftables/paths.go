package nftables

// Repo-relative paths for the static files the nftables renderer writes
// outside of topology.nftables.rulesets. These are deliberately hard-coded
// here rather than discovered at render time: the bazelnftables renderer
// imports them so the generated Bazel manifest captures all 25 entries
// (3 static + 22 dynamic per ruleset) without re-deriving paths from the
// nftables package's render flow.
const (
	ConfPath           = "src/platform/ansible/share/rendered/etc/nftables.conf"
	HostFirewallPath   = "src/platform/ansible/share/rendered/etc/nftables.d/host-firewall.nft"
	FirewallTargetPath = "src/platform/ansible/share/rendered/etc/systemd/system/verself-firewall.target"
)

// RulesetPath returns the repo-relative output path for a CUE
// topology.nftables.rulesets entry's `target`. The CUE target is
// always rooted at /etc/...; we anchor it under the rendered tree.
func RulesetPath(target string) string {
	for len(target) > 0 && target[0] == '/' {
		target = target[1:]
	}
	return "src/platform/ansible/share/rendered/" + target
}
