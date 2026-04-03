package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	ci "github.com/forge-metal/forge-metal/internal/ci"
)

func fixturesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "fixtures",
		Short: "Seed and verify controlled fixture suites in Forgejo",
	}
	cmd.AddCommand(fixturesRunCmd())
	return cmd
}

func fixturesRunCmd() *cobra.Command {
	var (
		fixturesRoot    string
		suites          []string
		forgejoURL      string
		owner           string
		token           string
		username        string
		password        string
		email           string
		pool            string
		goldenZvol      string
		kernelPath      string
		fcBin           string
		jailerBin       string
		vcpus           int
		memoryMiB       int
		timeout         string
		hostInterface   string
		guestPoolCIDR   string
		networkLeaseDir string
	)

	cmd := &cobra.Command{
		Use:   "run",
		Short: "Seed fixture suites, warm their goldens, open PRs, and verify expected CI outcomes",
		RunE: func(cmd *cobra.Command, args []string) error {
			if owner == "" {
				return fmt.Errorf("owner is required")
			}
			if token == "" {
				token = strings.TrimSpace(os.Getenv("FORGE_METAL_FIXTURES_TOKEN"))
			}
			cfg, err := ciFirecrackerConfig(pool, goldenZvol, kernelPath, fcBin, jailerBin, vcpus, memoryMiB, hostInterface, guestPoolCIDR, networkLeaseDir)
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

			return ci.RunFixtureSuites(ctx, logger, manager, client, ci.FixtureRunOptions{
				FixturesRoot: fixturesRoot,
				Suites:       suites,
				ForgejoURL:   forgejoURL,
				Owner:        owner,
				Token:        token,
				Username:     username,
				Email:        email,
			})
		},
	}

	cmd.Flags().StringVar(&fixturesRoot, "fixtures-root", "test/fixtures", "Local fixture repository root")
	cmd.Flags().StringSliceVar(&suites, "suite", []string{"pass"}, "Fixture suite(s) to run")
	cmd.Flags().StringVar(&forgejoURL, "forgejo-url", "http://127.0.0.1:3000", "Forgejo base URL")
	cmd.Flags().StringVar(&owner, "owner", "", "Forgejo owner/user that should own the fixture repos")
	cmd.Flags().StringVar(&token, "token", "", "Forgejo API token (or FORGE_METAL_FIXTURES_TOKEN)")
	cmd.Flags().StringVar(&username, "username", "", "Forgejo username for token creation and git pushes")
	cmd.Flags().StringVar(&password, "password", "", "Forgejo password for token creation")
	cmd.Flags().StringVar(&email, "email", "forge-metal-fixtures@local", "Git author email for seeded commits")
	addFirecrackerFlags(cmd, &pool, &goldenZvol, &kernelPath, &fcBin, &jailerBin, &vcpus, &memoryMiB, &timeout, &hostInterface, &guestPoolCIDR, &networkLeaseDir)
	return cmd
}
