package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var version = "dev"

func main() {
	root := newRootCmd()
	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:     "forge-metal",
		Short:   "forge-metal: bare-metal CI platform",
		Version: version,
	}

	root.AddCommand(provisionCmd())
	root.AddCommand(setupDomainCmd())
	root.AddCommand(ciCmd())
	root.AddCommand(fixturesCmd())

	return root
}
