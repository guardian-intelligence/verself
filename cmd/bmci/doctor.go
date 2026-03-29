package main

import (
	"fmt"

	"github.com/forge-metal/forge-metal/internal/doctor"
	"github.com/spf13/cobra"
)

func doctorCmd() *cobra.Command {
	var autoFix bool
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Check that all required dev tools are installed and at the correct version",
		RunE: func(cmd *cobra.Command, args []string) error {
			results, summary := doctor.CheckAll(doctor.SystemResolver{})

			for _, r := range results {
				printResult(r)
			}
			fmt.Println()

			fmt.Printf("%d ok", summary.OK)
			if summary.Installable > 0 {
				fmt.Printf(", %d installable", summary.Installable)
			}
			if summary.Upgradable > 0 {
				fmt.Printf(", %d upgradable", summary.Upgradable)
			}
			if summary.Missing > 0 {
				fmt.Printf(", %d missing", summary.Missing)
			}
			if summary.Conflict > 0 {
				fmt.Printf(", %d conflict", summary.Conflict)
			}
			fmt.Println()

			fixable := summary.Missing + summary.Installable + summary.Upgradable
			unfixable := summary.Conflict

			if autoFix && fixable > 0 {
				fmt.Println()
				fixed, errs := doctor.Fix(results)
				for _, name := range fixed {
					fmt.Printf("  + installed %s\n", name)
				}
				for _, err := range errs {
					fmt.Printf("  ✗ %s\n", err)
				}
				// Adjust counts: successfully fixed tools are now OK
				fixable -= len(fixed)
			}

			for _, r := range results {
				if r.Status == doctor.Conflict {
					fmt.Printf("  ⚠ %s %s from %s conflicts — remove it or add nix profile to PATH first\n",
						r.Spec.Name, r.ActualVer, r.BinPath)
				}
			}

			issues := fixable + unfixable
			if issues > 0 {
				return fmt.Errorf("%d issues found", issues)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&autoFix, "fix", false, "Auto-install/upgrade fixable tools via nix profile")
	return cmd
}

func printResult(r doctor.CheckResult) {
	switch r.Status {
	case doctor.OK:
		fmt.Printf("  ✓ %-12s %s\n", r.Spec.Name, r.ActualVer)
	case doctor.Missing:
		fmt.Printf("  ✗ %-12s missing (run: bmci doctor --fix)\n", r.Spec.Name)
	case doctor.Installable:
		fmt.Printf("  ✗ %-12s not in PATH (run: bmci doctor --fix)\n", r.Spec.Name)
	case doctor.Upgradable:
		fmt.Printf("  ⚠ %-12s %s (want %s, nix-managed)\n", r.Spec.Name, r.ActualVer, r.Spec.Expected)
	case doctor.Conflict:
		fmt.Printf("  ⚠ %-12s %s (want %s, from %s)\n", r.Spec.Name, r.ActualVer, r.Spec.Expected, r.BinPath)
	}
}
