package main

import (
	"context"
	"os"

	"github.com/forge-metal/forge-metal/internal/config"
	"github.com/forge-metal/forge-metal/internal/prompt"
	"github.com/forge-metal/forge-metal/internal/provision"
	"github.com/spf13/cobra"
)

func provisionCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "provision",
		Short: "Interactively configure and provision bare metal servers",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load("")
			if err != nil {
				return err
			}

			w := &provision.Wizard{
				Cfg:      cfg,
				Prompter: &prompt.TTYPrompter{In: os.Stdin, Out: os.Stdout},
				Out:      os.Stdout,
			}
			return w.Run(context.Background())
		},
	}
	return cmd
}
