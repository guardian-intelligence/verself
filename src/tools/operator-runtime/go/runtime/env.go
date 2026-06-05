package runtime

import (
	"os"
	"strings"
)

// UseRecoveryFromEnv reports whether operator commands should reach the host
// over the out-of-band recovery SSH transport.
func UseRecoveryFromEnv() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("VERSELF_OPERATOR_SSH_TRANSPORT"))) {
	case "recovery", "wireguard", "wg":
		return true
	default:
		return false
	}
}
