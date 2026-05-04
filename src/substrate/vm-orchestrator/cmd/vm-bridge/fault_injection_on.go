//go:build verself_fault_injection

package main

import (
	"fmt"
	"strings"
)

// parseBridgeFaultMode for verification builds: parses the fault-injection
// env var and lets the host drive deterministic protocol violations.
func parseBridgeFaultMode(raw string) (bridgeFaultMode, error) {
	mode := bridgeFaultMode(strings.TrimSpace(raw))
	switch mode {
	case bridgeFaultNone, bridgeFaultResultSeqZero:
		return mode, nil
	default:
		return bridgeFaultNone, fmt.Errorf("unsupported vm-bridge fault mode: %q", raw)
	}
}
