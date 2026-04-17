// forge-metal-policy-check loads the machine-readable policy source and
// emits a ClickHouse-searchable trace span summarizing the declared retention
// windows and subprocessors. It exits non-zero on any schema violation or
// cross-file inconsistency, so it doubles as a CI gate.
//
// Usage:
//
//	forge-metal-policy-check [--source-dir PATH]
//
// The span is named forge_metal.policy.check and carries one attribute per
// (window_id, lifecycle_state) pair, so a single ClickHouse query can pull
// every declared window from the most recent check.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	fmotel "github.com/forge-metal/otel"
	"github.com/forge-metal/platform-policyspec"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "forge-metal-policy-check:", err)
		os.Exit(1)
	}
}

func run() error {
	var sourceDir string
	flag.StringVar(&sourceDir, "source-dir", "src/platform/policies", "directory containing the policy YAML files")
	flag.Parse()

	spec, err := policyspec.Load(sourceDir)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	shutdown, _, err := fmotel.Init(ctx, fmotel.Config{
		ServiceName:    "forge-metal-policy-check",
		ServiceVersion: spec.Retention.EffectiveAt,
	})
	if err != nil {
		return fmt.Errorf("initialize OTel: %w", err)
	}
	defer func() {
		shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancelShutdown()
		_ = shutdown(shutdownCtx)
	}()

	spec.EmitBootSpan(ctx)
	fmt.Printf(
		"policy check OK: retention windows=%d, subprocessors=%d, ropa activities=%d, versions=%d\n",
		len(spec.Retention.Windows),
		len(spec.Subprocessors.Subprocessors),
		len(spec.ROPA.ProcessingActivities),
		len(spec.Versions.Entries),
	)
	return nil
}
