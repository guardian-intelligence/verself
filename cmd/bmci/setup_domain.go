package main

import (
	"os"

	"github.com/forge-metal/forge-metal/internal/domain"
	"github.com/spf13/cobra"
)

func setupDomainCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "setup-domain [domain]",
		Short: "Configure Cloudflare DNS for your forge-metal deployment",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := domain.Config{
				AnsibleVars: "ansible/group_vars/all/main.yml",
				SecretsFile: "ansible/group_vars/all/secrets.sops.yml",
			}
			if len(args) > 0 {
				cfg.Domain = args[0]
			}
			p := &domain.TTYPrompter{In: os.Stdin, Out: os.Stdout}
			return domain.Run(cfg, p, os.Stdout)
		},
	}
	return cmd
}
