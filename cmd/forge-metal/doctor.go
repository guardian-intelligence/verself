package main

import (
	"fmt"

	"github.com/forge-metal/forge-metal/internal/doctor"
	"github.com/spf13/cobra"
)

func doctorCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Check that all required dev tools are installed and at the correct version",
		RunE: func(cmd *cobra.Command, args []string) error {
			manifest, err := doctor.LoadManifest()
			if err != nil {
				return err
			}

			results, summary := doctor.CheckAll(manifest, &doctor.SystemResolver{})

			fmt.Println("    Tool              Have       Want")
			for _, r := range results {
				printResult(r)
			}
			fmt.Println()

			printSummary(summary)

			issues := summary.Missing + summary.VersionMismatch
			if issues > 0 {
				fmt.Println("  hint: run 'make setup-dev' to install pinned versions")
				return fmt.Errorf("%d issues found", issues)
			}
			return nil
		},
	}
	return cmd
}

func printSummary(s doctor.Summary) {
	fmt.Printf("%d ok", s.OK)
	if s.Missing > 0 {
		fmt.Printf(", %d missing", s.Missing)
	}
	if s.VersionMismatch > 0 {
		fmt.Printf(", %d version mismatch", s.VersionMismatch)
	}
	fmt.Println()
}

func printResult(r doctor.CheckResult) {
	var icon, have string
	switch r.Status {
	case doctor.OK:
		icon, have = "✓", r.ActualVer
	case doctor.Missing:
		icon, have = "✗", "—"
	case doctor.VersionMismatch:
		icon, have = "⚠", r.ActualVer
	}
	fmt.Printf("  %s %-16s  %-8s   %s\n", icon, r.Spec.Name, have, r.Spec.Expected)
}
