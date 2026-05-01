package main

import (
	"strings"
	"testing"
)

// rewriteSSHConfigStripLegacy edits the operator's ~/.ssh/config; a
// regression here can lock the user out of every host they use SSH
// for. Each subtest fixes one shape we have seen in operator configs
// (cutover-era and historical) and verifies the rewrite removes
// exactly the legacy hook plus its explanatory header — never an
// unrelated Host block, never an unrelated comment paragraph.
func TestRewriteSSHConfigStripLegacy(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "post-cutover config with header paragraph and Host block",
			in: `# Verself bare-metal worker — cert auth via OpenBao SSH CA.
# Public :22 is closed; reach the box only over wg-ops. Each connection
# runs ` + "`aspect ssh cert --if-needed`" + ` first; that exits 0 immediately
# when the cert has more than 5 min remaining, re-signs silently using
# the cached Vault token when the cert is expiring, and falls back to
# OIDC (browser) only when the token is also expired.
Match host fm-dev-w0,10.66.66.1 exec "/home/ubuntu/Projects/verself-sh/src/platform/scripts/ssh-cert.sh --if-needed"
    # Match-with-exec is a no-op-when-true ssh hook; the directives below
    # apply on its match. Always exit 0 from the hook so ssh proceeds.

Host fm-dev-w0 10.66.66.1
    HostName 10.66.66.1
    User ubuntu
    IdentityFile ~/.ssh/id_verself
    CertificateFile ~/.ssh/id_verself-cert.pub
    IdentitiesOnly yes
    ControlMaster auto
    ControlPath ~/.ssh/sockets/%r@%h-%p
    ControlPersist 1h
    ServerAliveInterval 30
    ServerAliveCountMax 3
    ConnectTimeout 10
`,
			want: `Host fm-dev-w0 10.66.66.1
    HostName 10.66.66.1
    User ubuntu
    IdentityFile ~/.ssh/id_verself
    CertificateFile ~/.ssh/id_verself-cert.pub
    IdentitiesOnly yes
    ControlMaster auto
    ControlPath ~/.ssh/sockets/%r@%h-%p
    ControlPersist 1h
    ServerAliveInterval 30
    ServerAliveCountMax 3
    ConnectTimeout 10
`,
		},
		{
			name: "Match line alone, no header",
			in: `Match host fm-dev-w0 exec "/path/to/ssh-cert.sh --if-needed"
    User ubuntu

Host other
    HostName other.example
`,
			want: `Host other
    HostName other.example
`,
		},
		{
			name: "no legacy hook present is a no-op",
			in: `Host github.com
    User git
    IdentityFile ~/.ssh/github
`,
			want: `Host github.com
    User git
    IdentityFile ~/.ssh/github
`,
		},
		{
			name: "unrelated comment paragraph above is NOT swallowed (separated by blank line)",
			in: `# This block documents the github connection.
# Has nothing to do with the legacy hook.

# header for the legacy hook
Match host fm-dev-w0 exec "/x/ssh-cert.sh --if-needed"
    User ubuntu

Host github.com
    User git
`,
			want: `# This block documents the github connection.
# Has nothing to do with the legacy hook.

Host github.com
    User git
`,
		},
		{
			name: "legacy hook surrounded by other Host blocks — only the hook strips",
			in: `Host github.com
    User git

# legacy
Match host fm-dev-w0 exec "/x/ssh-cert.sh"
    User ubuntu

Host gitlab.com
    User git
`,
			want: `Host github.com
    User git

Host gitlab.com
    User git
`,
		},
		{
			name: "Match host without legacy reference is preserved",
			in: `Match host secure exec "/usr/bin/some-other-tool"
    ProxyJump bastion

Host secure
    HostName secure.internal
`,
			want: `Match host secure exec "/usr/bin/some-other-tool"
    ProxyJump bastion

Host secure
    HostName secure.internal
`,
		},
		{
			name: "two legacy variants: only the first is stripped (we don't expect more than one)",
			// If a second hook ever appears, the rewrite leaves it for
			// the operator to notice — silently stripping multiple
			// blocks risks compounding a user config we did not
			// understand.
			in: `Match host a exec "/x/ssh-cert.sh"
    User a

Match host b exec "/y/ssh-cert.sh"
    User b
`,
			want: `Match host b exec "/y/ssh-cert.sh"
    User b
`,
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got := rewriteSSHConfigStripLegacy(tc.in)
			if got != tc.want {
				t.Fatalf("mismatch\n--- got\n%s\n--- want\n%s\n--- diff\n%s",
					got, tc.want, simpleDiff(tc.want, got))
			}
		})
	}
}

func simpleDiff(want, got string) string {
	wl := strings.Split(want, "\n")
	gl := strings.Split(got, "\n")
	var b strings.Builder
	maxLen := len(wl)
	if len(gl) > maxLen {
		maxLen = len(gl)
	}
	for i := 0; i < maxLen; i++ {
		w := ""
		g := ""
		if i < len(wl) {
			w = wl[i]
		}
		if i < len(gl) {
			g = gl[i]
		}
		if w == g {
			continue
		}
		b.WriteString("line ")
		b.WriteString(itoa(i + 1))
		b.WriteString(":\n  -want: ")
		b.WriteString(w)
		b.WriteString("\n  +got:  ")
		b.WriteString(g)
		b.WriteString("\n")
	}
	return b.String()
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var out []byte
	for n > 0 {
		out = append([]byte{byte('0' + n%10)}, out...)
		n /= 10
	}
	return string(out)
}
