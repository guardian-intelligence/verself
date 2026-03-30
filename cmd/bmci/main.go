package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var version = "dev"

func main() {
	root := &cobra.Command{
		Use:     "bmci",
		Short:   "forge-metal: bare-metal CI platform",
		Version: version,
	}

	root.AddCommand(doctorCmd())
	root.AddCommand(provisionCmd())
	root.AddCommand(setupDomainCmd())
	root.AddCommand(benchmarkCmd())

	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
