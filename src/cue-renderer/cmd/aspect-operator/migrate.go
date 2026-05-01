package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// adoptLegacySSHKey copies the operator's pre-existing ed25519 SSH
// keypair at ~/.ssh/id_verself{,.pub} into the canonical device path
// when the canonical path is empty. Returns true when adoption
// happened — callers use that to skip ssh-keygen. Idempotent across
// re-runs.
//
// We copy rather than symlink so that deleting the legacy file later
// does not strand the canonical path. The cert symlink (the inverse
// direction — see linkLegacyCertPath) is what keeps the two paths in
// sync going forward.
func adoptLegacySSHKey(canonicalPriv string) (bool, error) {
	if _, err := os.Stat(canonicalPriv); err == nil {
		return false, nil
	} else if !os.IsNotExist(err) {
		return false, err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return false, err
	}
	legacyPriv := filepath.Join(home, ".ssh", "id_verself")
	legacyPub := legacyPriv + ".pub"
	if _, err := os.Stat(legacyPriv); err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	priv, err := os.ReadFile(legacyPriv)
	if err != nil {
		return false, err
	}
	pub, err := os.ReadFile(legacyPub)
	if err != nil {
		return false, err
	}
	if err := os.MkdirAll(filepath.Dir(canonicalPriv), 0o700); err != nil {
		return false, err
	}
	if err := os.WriteFile(canonicalPriv, priv, 0o600); err != nil {
		return false, err
	}
	if err := os.WriteFile(canonicalPriv+".pub", pub, 0o600); err != nil {
		return false, err
	}
	return true, nil
}

// detectExistingWGOps returns the operator's wg-ops IPv4 when the
// system already has a wg-ops interface up (typically wg-quick@wg-ops
// configured outside this binary's scope). The binary then skips its
// own per-user `verself` tunnel — adding a second interface would
// either collide on the same address or, worse, accept traffic on a
// pubkey not registered in CUE.
//
// Uses `ip` rather than `wg show` because the latter requires CAP_NET_ADMIN
// (sudo); `ip -4 -o addr show wg-ops` is unprivileged.
func detectExistingWGOps() (string, bool) {
	out, err := exec.Command("ip", "-4", "-o", "addr", "show", "wg-ops").Output()
	if err != nil {
		return "", false
	}
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		for i, f := range fields {
			if f == "inet" && i+1 < len(fields) {
				return strings.SplitN(fields[i+1], "/", 2)[0], true
			}
		}
	}
	return "", false
}

// linkLegacyCertPath ensures ~/.ssh/id_verself-cert.pub points at the
// canonical cert. Replaces whatever is currently there (regular file
// from the pre-cutover ssh-cert.sh world, or a stale symlink). Future
// `aspect operator refresh` calls write to the canonical path; the
// symlink keeps any consumer that still references the legacy path
// (Ansible inventories, hand-rolled ssh configs) automatically fresh.
func linkLegacyCertPath(canonical string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	legacy := filepath.Join(home, ".ssh", "id_verself-cert.pub")
	if existing, err := os.Readlink(legacy); err == nil && existing == canonical {
		return nil
	}
	if err := os.Remove(legacy); err != nil && !os.IsNotExist(err) {
		return err
	}
	return os.Symlink(canonical, legacy)
}

// stripLegacyMatchExec removes any `Match host ... exec ".../ssh-cert.sh ..."`
// block from ~/.ssh/config along with its indented continuation lines
// and the explanatory comment header above it. Idempotent; a no-op
// when the block is absent. Atomic write via tmp+rename so a partial
// write cannot corrupt the operator's ssh config on crash.
//
// The header detection looks back for a contiguous block of
// `# ...ssh-cert.sh...` or `# ...aspect ssh cert...` comment lines
// immediately preceding the Match line, with no blank line between —
// matches the shape `aspect ssh cert` legacy commits dropped into
// ~/.ssh/config without removing it from the host's main config.
func stripLegacyMatchExec() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	cfgPath := filepath.Join(home, ".ssh", "config")
	body, err := os.ReadFile(cfgPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	rewritten := rewriteSSHConfigStripLegacy(string(body))
	if rewritten == string(body) {
		return nil
	}
	tmp := cfgPath + ".tmp"
	if err := os.WriteFile(tmp, []byte(rewritten), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, cfgPath)
}

// rewriteSSHConfigStripLegacy is stripLegacyMatchExec's pure
// transformation, lifted into a function to keep the unit test off
// the filesystem. Removes:
//
//   - The `Match host ... exec ".../ssh-cert.sh ..."` line
//   - Whitespace-indented continuation lines (comments and directives)
//     immediately below it, up to the next blank line
//   - Comment header lines (`# ...`) immediately above the Match line
//     that mention ssh-cert.sh or `aspect ssh cert`
//   - Any single blank line directly below the stripped block, to keep
//     the output free of double-blank scars
//
// Anything else — including unrelated Host blocks, even ones that
// reference id_verself paths — stays intact. The new drop-in writes
// `Include config.d/*` at the top of the file; ssh's first-match-wins
// rule means the new drop-in's Host block shadows any legacy Host
// block without us having to delete it.
func rewriteSSHConfigStripLegacy(body string) string {
	lines := strings.Split(body, "\n")
	matchIdx := -1
	for i, line := range lines {
		trimmed := strings.TrimLeft(line, " \t")
		if !strings.HasPrefix(trimmed, "Match") {
			continue
		}
		if !strings.Contains(line, "ssh-cert.sh") && !strings.Contains(line, "aspect ssh cert") {
			continue
		}
		matchIdx = i
		break
	}
	if matchIdx < 0 {
		return body
	}

	// Find the lower bound: include the Match line and any indented
	// continuation directly below it.
	lo := matchIdx
	hi := matchIdx + 1
	for hi < len(lines) {
		line := lines[hi]
		if line == "" {
			hi++ // collapse one trailing blank line into the strip range
			break
		}
		if strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t") {
			hi++
			continue
		}
		break
	}

	// Walk back through any contiguous comment-header block above the
	// Match line, stopping at a blank line or a non-comment line. SSH
	// config has no syntactic notion of "header comment for the next
	// directive" — operators write a paragraph of `# ...` lines above
	// the directive they explain, with a blank line separating it from
	// the previous block. Stripping the Match line without its
	// explanatory paragraph leaves orphaned comments referencing a
	// hook that no longer exists. We only walk back across an
	// uninterrupted comment block (no blank-line break), so unrelated
	// comments separated by even one blank line stay intact.
	for lo > 0 {
		prev := strings.TrimSpace(lines[lo-1])
		if !strings.HasPrefix(prev, "#") {
			break
		}
		lo--
	}

	out := make([]string, 0, len(lines))
	out = append(out, lines[:lo]...)
	out = append(out, lines[hi:]...)
	return strings.Join(out, "\n")
}
