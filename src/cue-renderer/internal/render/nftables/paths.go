package nftables

// Cache-relative paths for the static files the nftables renderer writes
// outside of topology.nftables.rulesets. The renderer's `--output-dir`
// flag anchors these under e.g. `.cache/render/prod/share/rendered/...`.
const (
	ConfPath             = "share/rendered/etc/nftables.conf"
	HostFirewallPath     = "share/rendered/etc/nftables.d/host-firewall.nft"
	FirecrackerChainPath = "share/rendered/etc/nftables.d/firecracker.nft"
	FirewallTargetPath   = "share/rendered/etc/systemd/system/verself-firewall.target"
)

// RulesetPath returns the cache-relative output path for a CUE
// topology.nftables.rulesets entry's `target`. The CUE target is
// always rooted at /etc/...; we anchor it under the rendered tree.
func RulesetPath(target string) string {
	for len(target) > 0 && target[0] == '/' {
		target = target[1:]
	}
	return "share/rendered/" + target
}
