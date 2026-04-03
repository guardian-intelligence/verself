// Package supplychain implements a scanning pipeline for Verdaccio npm registry
// storage. Each Scanner inspects cached packages for a specific class of supply
// chain threat. The Gate orchestrates all scanners and produces an aggregate
// pass/fail decision that gates the ZFS snapshot promotion of the mirror.
package supplychain

import (
	"context"
	"time"
)

// Severity classifies a finding's impact on the gate decision.
type Severity int

const (
	SeverityInfo     Severity = iota // Informational, does not block.
	SeverityWarning                  // Suspicious but within allowlist tolerance.
	SeverityBlocking                 // Blocks snapshot promotion.
)

func (s Severity) String() string {
	switch s {
	case SeverityInfo:
		return "info"
	case SeverityWarning:
		return "warning"
	case SeverityBlocking:
		return "blocking"
	default:
		return "unknown"
	}
}

// Finding is a single observation from a scanner.
type Finding struct {
	Scanner  string   `json:"scanner"`
	Package  string   `json:"package"`
	Version  string   `json:"version,omitempty"`
	Rule     string   `json:"rule"`
	Severity Severity `json:"severity"`
	Detail   string   `json:"detail"`
}

// ScanResult aggregates findings from a single scanner.
type ScanResult struct {
	Scanner  string    `json:"scanner"`
	Findings []Finding `json:"findings"`
	Duration time.Duration
}

// BlockingCount returns the number of findings with Severity >= SeverityBlocking.
func (r ScanResult) BlockingCount() int {
	n := 0
	for i := range r.Findings {
		if r.Findings[i].Severity >= SeverityBlocking {
			n++
		}
	}
	return n
}

// Scanner inspects Verdaccio storage for a specific class of supply chain threat.
// storagePath is the root of Verdaccio's package cache (e.g. /var/lib/verdaccio/storage).
type Scanner interface {
	Name() string
	Scan(ctx context.Context, storagePath string) ([]Finding, error)
}
