package bazelbuild

import (
	"os"
	"strings"
)

// Env is the environment for every subprocess that evaluates or executes
// repo-controlled code (bazelisk query/build/run). The OpenBao workload token
// grants transit signing authority; leaking it into build actions would let
// committed code forge deployment provenance, so it never crosses this
// boundary.
func Env() []string {
	env := os.Environ()
	out := make([]string, 0, len(env))
	for _, kv := range env {
		if strings.HasPrefix(kv, "VAULT_TOKEN=") || strings.HasPrefix(kv, "BAO_TOKEN=") {
			continue
		}
		out = append(out, kv)
	}
	return out
}
