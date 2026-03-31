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
			results, summary := doctor.CheckAll(&doctor.SystemResolver{})

			if autoFix {
				fixable := summary.Missing + summary.Installable + summary.Upgradable
				if fixable > 0 {
					fixed, errs := doctor.Fix(results)
					for _, name := range fixed {
						fmt.Printf("  + installed %s\n", name)
					}
					for _, err := range errs {
						fmt.Printf("  ✗ %s\n", err)
					}
					if len(fixed) > 0 {
						fmt.Println()
					}
				}
				// Re-check after fix to show current state
				results, summary = doctor.CheckAll(&doctor.SystemResolver{})
			}

			fmt.Println("    Tool          Have       Want")
			for _, r := range results {
				printResult(r)
			}
			fmt.Println()

			printSummary(summary)

			if summary.Conflict > 0 {
				fmt.Println("  hint: remove system versions or ensure ~/.nix-profile/bin is first in PATH")
			}

			issues := summary.Missing + summary.Installable + summary.Upgradable + summary.Conflict
			if issues > 0 {
				return fmt.Errorf("%d issues found", issues)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&autoFix, "fix", false, "Auto-install/upgrade fixable tools via nix profile")
	return cmd
}

func printSummary(s doctor.Summary) {
	fmt.Printf("%d ok", s.OK)
	if s.Installable > 0 {
		fmt.Printf(", %d installable", s.Installable)
	}
	if s.Upgradable > 0 {
		fmt.Printf(", %d upgradable", s.Upgradable)
	}
	if s.Missing > 0 {
		fmt.Printf(", %d missing", s.Missing)
	}
	if s.Conflict > 0 {
		fmt.Printf(", %d conflict", s.Conflict)
	}
	fmt.Println()
}

func printResult(r doctor.CheckResult) {
	var icon, have, note string
	switch r.Status {
	case doctor.OK:
		icon, have = "✓", r.ActualVer
	case doctor.Missing, doctor.Installable:
		icon, have = "✗", "—"
	case doctor.Upgradable:
		icon, have = "⚠", r.ActualVer
	case doctor.Conflict:
		icon, have, note = "⚠", r.ActualVer, r.BinPath
	}
	if note != "" {
		fmt.Printf("  %s %-12s  %-8s   %-8s   %s\n", icon, r.Spec.Name, have, r.Spec.Expected, note)
	} else {
		fmt.Printf("  %s %-12s  %-8s   %s\n", icon, r.Spec.Name, have, r.Spec.Expected)
	}
}
