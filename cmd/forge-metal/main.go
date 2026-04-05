package main

import (
	"fmt"
	"os"

	"github.com/forge-metal/forge-metal/internal/config"
	"github.com/spf13/cobra"
)

var version = "dev"

func main() {
	paths, err := config.DefaultPaths()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	root := newRootCmd(paths)
	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func newRootCmd(paths config.Paths) *cobra.Command {
	root := &cobra.Command{
		Use:     "forge-metal",
		Short:   "forge-metal: bare-metal CI platform",
		Version: version,
	}

	root.AddCommand(doctorCmd())
	root.AddCommand(provisionCmd())
	root.AddCommand(setupDomainCmd())
	root.AddCommand(configCmd(paths))
	root.AddCommand(firecrackerTestCmd())
	root.AddCommand(ciCmd())
	root.AddCommand(fixturesCmd())
	root.AddCommand(billingCmd())

	return root
}
