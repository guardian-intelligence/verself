package main

import (
	"os"

	"github.com/forge-metal/forge-metal/internal/domain"
	"github.com/forge-metal/forge-metal/internal/prompt"
	"github.com/spf13/cobra"
)

func setupDomainCmd() *cobra.Command {
	var (
		domainFlag string
		tokenFlag  string
	)

	cmd := &cobra.Command{
		Use:   "setup-domain [domain]",
		Short: "Configure Cloudflare DNS for your forge-metal deployment",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := domain.Config{
				AnsibleVars: "ansible/group_vars/all/main.yml",
				SecretsFile: "ansible/group_vars/all/secrets.sops.yml",
			}

			// Positional arg takes precedence over --domain flag.
			if len(args) > 0 {
				cfg.Domain = args[0]
			} else if domainFlag != "" {
				cfg.Domain = domainFlag
			}

			cfg.Token = tokenFlag

			p := &prompt.TTYPrompter{In: os.Stdin, Out: os.Stdout}
			return domain.Run(cfg, p, os.Stdout)
		},
	}

	cmd.Flags().StringVar(&domainFlag, "domain", "", "Cloudflare-managed domain (e.g. anveio.com)")
	cmd.Flags().StringVar(&tokenFlag, "token", "", "Cloudflare API token with Zone:DNS:Edit permission")

	return cmd
}
