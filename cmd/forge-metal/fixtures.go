package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/spf13/cobra"

	ci "github.com/forge-metal/forge-metal/internal/ci"
)

func fixturesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "fixtures",
		Short: "Seed and verify controlled Next.js fixture repos in Forgejo",
	}
	cmd.AddCommand(fixturesE2ECmd())
	return cmd
}

func fixturesE2ECmd() *cobra.Command {
	var (
		fixturesRoot  string
		forgejoURL    string
		owner         string
		token         string
		username      string
		password      string
		email         string
		pool          string
		goldenZvol    string
		kernelPath    string
		fcBin         string
		jailerBin     string
		vcpus         int
		memoryMiB     int
		timeout       string
		hostInterface string
	)

	cmd := &cobra.Command{
		Use:   "e2e",
		Short: "Seed the fixture repos, warm their goldens, open PRs, and wait for CI success",
		RunE: func(cmd *cobra.Command, args []string) error {
			if owner == "" {
				return fmt.Errorf("owner is required")
			}
			cfg, err := ciFirecrackerConfig(pool, goldenZvol, kernelPath, fcBin, jailerBin, vcpus, memoryMiB, hostInterface)
			if err != nil {
				return err
			}
			dur, err := time.ParseDuration(timeout)
			if err != nil {
				return err
			}

			logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
			manager := ci.NewManager(cfg, logger)

			ctx, cancel := context.WithTimeout(context.Background(), dur)
			defer cancel()
			handleSignals(cancel, logger)

			var client *ci.ForgejoClient
			if token != "" {
				client = ci.NewForgejoTokenClient(forgejoURL, token)
			} else {
				if username == "" || password == "" {
					return fmt.Errorf("either --token or both --username and --password are required")
				}
				basicClient := ci.NewForgejoBasicClient(forgejoURL, username, password)
				createdToken, err := basicClient.CreateToken(ctx, fmt.Sprintf("forge-metal-fixtures-%d", time.Now().Unix()))
				if err != nil {
					return err
				}
				token = createdToken
				client = ci.NewForgejoTokenClient(forgejoURL, token)
			}

			return ci.RunFixturesE2E(ctx, logger, manager, client, ci.E2EOptions{
				FixturesRoot: fixturesRoot,
				ForgejoURL:   forgejoURL,
				Owner:        owner,
				Token:        token,
				Username:     username,
				Email:        email,
			})
		},
	}

	cmd.Flags().StringVar(&fixturesRoot, "fixtures-root", "test/fixtures", "Local fixture repository root")
	cmd.Flags().StringVar(&forgejoURL, "forgejo-url", "http://127.0.0.1:3000", "Forgejo base URL")
	cmd.Flags().StringVar(&owner, "owner", "", "Forgejo owner/user that should own the fixture repos")
	cmd.Flags().StringVar(&token, "token", "", "Forgejo API token")
	cmd.Flags().StringVar(&username, "username", "", "Forgejo username for token creation and git pushes")
	cmd.Flags().StringVar(&password, "password", "", "Forgejo password for token creation")
	cmd.Flags().StringVar(&email, "email", "forge-metal-fixtures@local", "Git author email for seeded commits")
	addFirecrackerFlags(cmd, &pool, &goldenZvol, &kernelPath, &fcBin, &jailerBin, &vcpus, &memoryMiB, &timeout, &hostInterface)
	return cmd
}
