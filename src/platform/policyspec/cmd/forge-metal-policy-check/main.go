// forge-metal-policy-check loads the machine-readable policy source, verifies
// that the viteplus platform app's generated copy is byte-identical to it,
// and emits a ClickHouse-searchable trace span summarizing the declared
// retention windows and subprocessors. It exits non-zero on any schema
// violation, cross-file inconsistency, or __generated drift — so it doubles
// as a CI gate.
//
// Usage:
//
//	forge-metal-policy-check [--source-dir PATH] [--generated-dir PATH]
//
// The span is named forge_metal.policy.check and carries one attribute per
// (window_id, lifecycle_state) pair, so a single ClickHouse query can pull
// every declared window from the most recent check.
package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
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
	var sourceDir, generatedDir string
	flag.StringVar(&sourceDir, "source-dir", "src/platform/policies", "directory containing the policy YAML files")
	flag.StringVar(&generatedDir, "generated-dir", "", "directory containing the frontend's copy of the policy YAML files (drift-checked; defaults to the sibling viteplus platform app's __generated/policies; skipped if missing)")
	flag.Parse()

	spec, err := policyspec.Load(sourceDir)
	if err != nil {
		return err
	}

	if generatedDir == "" {
		abs, err := filepath.Abs(sourceDir)
		if err != nil {
			return fmt.Errorf("resolve source-dir: %w", err)
		}
		// sourceDir is canonically .../src/platform/policies; the sibling
		// viteplus platform app lives at .../src/viteplus-monorepo/...
		generatedDir = filepath.Join(abs, "..", "..", "viteplus-monorepo", "apps", "platform", "src", "__generated", "policies")
	}

	if err := checkGeneratedCopy(sourceDir, generatedDir); err != nil {
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

// checkGeneratedCopy enforces byte-identity between the canonical policy YAML
// and the copy the viteplus platform app builds against. Skipped cleanly when
// generatedDir is absent — the Ansible worker rsyncs only the monorepo, so
// the canonical dir doesn't exist there and neither side of the comparison is
// reachable; the check only needs to run where both sides do (developer
// machines and CI).
func checkGeneratedCopy(sourceDir, generatedDir string) error {
	if _, err := os.Stat(generatedDir); errors.Is(err, os.ErrNotExist) {
		return nil
	}
	files := []string{"retention.yml", "subprocessors.yml", "ropa.yml", "contacts.yml", "versions.yml"}
	for _, name := range files {
		source, err := os.ReadFile(filepath.Join(sourceDir, name))
		if err != nil {
			return fmt.Errorf("read canonical %s: %w", name, err)
		}
		generated, err := os.ReadFile(filepath.Join(generatedDir, name))
		if err != nil {
			return fmt.Errorf("read generated %s: %w", name, err)
		}
		if !bytes.Equal(source, generated) {
			return fmt.Errorf(
				"%s in %s diverges from canonical — run `cd src/viteplus-monorepo/apps/platform && pnpm generate` and commit the result",
				name, generatedDir,
			)
		}
	}
	return nil
}
