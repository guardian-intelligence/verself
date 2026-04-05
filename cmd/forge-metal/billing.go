package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	_ "github.com/lib/pq"
	"github.com/spf13/cobra"
	"github.com/stripe/stripe-go/v85"
	tb "github.com/tigerbeetle/tigerbeetle-go"
	tbtypes "github.com/tigerbeetle/tigerbeetle-go/pkg/types"

	"github.com/forge-metal/forge-metal/internal/billing"
)

func billingCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "billing",
		Short: "Billing subsystem commands",
	}

	cmd.AddCommand(billingServeCmd())
	cmd.AddCommand(billingDepositCreditsCmd())
	cmd.AddCommand(billingExpireCreditsCmd())
	cmd.AddCommand(billingReconcileCmd())
	cmd.AddCommand(billingTrustTierEvaluateCmd())
	cmd.AddCommand(billingCanaryCmd())
	cmd.AddCommand(billingDlqCmd())

	return cmd
}

func billingServeCmd() *cobra.Command {
	var addr string

	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Start the billing HTTP server on localhost",
		Long: `Starts the billing HTTP server and task worker.

The server binds to 127.0.0.1:4242 by default (loopback only).
Caddy proxies /webhooks/stripe from the public side.

Required environment variables:
  FORGE_METAL_STRIPE_SECRET_KEY       Stripe secret key
  FORGE_METAL_STRIPE_WEBHOOK_SECRET   Stripe webhook endpoint secret
  FORGE_METAL_BILLING_PG_DSN          PostgreSQL connection string
  FORGE_METAL_BILLING_TB_ADDRESS      TigerBeetle address (default: 127.0.0.1:3320)
  FORGE_METAL_BILLING_TB_CLUSTER_ID   TigerBeetle cluster ID (default: 0)`,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, cleanup, err := newBillingClient()
			if err != nil {
				return err
			}
			defer cleanup()

			ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
			defer stop()

			srv := billing.NewServer(client, addr)
			return srv.ListenAndServe(ctx)
		},
	}

	cmd.Flags().StringVar(&addr, "addr", "127.0.0.1:4242", "Listen address")

	return cmd
}

// newBillingClient constructs a billing.Client from environment variables.
// Returns the client, a cleanup function, and any error.
func newBillingClient() (*billing.Client, func(), error) {
	stripeKey := os.Getenv("FORGE_METAL_STRIPE_SECRET_KEY")
	if stripeKey == "" {
		return nil, nil, fmt.Errorf("FORGE_METAL_STRIPE_SECRET_KEY is required")
	}
	webhookSecret := os.Getenv("FORGE_METAL_STRIPE_WEBHOOK_SECRET")
	if webhookSecret == "" {
		return nil, nil, fmt.Errorf("FORGE_METAL_STRIPE_WEBHOOK_SECRET is required")
	}
	pgDSN := os.Getenv("FORGE_METAL_BILLING_PG_DSN")
	if pgDSN == "" {
		return nil, nil, fmt.Errorf("FORGE_METAL_BILLING_PG_DSN is required")
	}

	tbAddress := os.Getenv("FORGE_METAL_BILLING_TB_ADDRESS")
	if tbAddress == "" {
		tbAddress = "127.0.0.1:3320"
	}
	var tbClusterID uint64
	if s := os.Getenv("FORGE_METAL_BILLING_TB_CLUSTER_ID"); s != "" {
		var err error
		tbClusterID, err = strconv.ParseUint(s, 10, 64)
		if err != nil {
			return nil, nil, fmt.Errorf("parse FORGE_METAL_BILLING_TB_CLUSTER_ID: %w", err)
		}
	}

	pg, err := sql.Open("postgres", pgDSN)
	if err != nil {
		return nil, nil, fmt.Errorf("open postgres: %w", err)
	}
	if err := pg.Ping(); err != nil {
		pg.Close()
		return nil, nil, fmt.Errorf("ping postgres: %w", err)
	}

	tbAddresses := strings.Split(tbAddress, ",")
	tbClient, err := tb.NewClient(tbtypes.ToUint128(tbClusterID), tbAddresses)
	if err != nil {
		pg.Close()
		return nil, nil, fmt.Errorf("create tigerbeetle client: %w", err)
	}

	sc := stripe.NewClient(stripeKey)

	cfg := billing.DefaultConfig()
	cfg.StripeSecretKey = stripeKey
	cfg.StripeWebhookSecret = webhookSecret
	cfg.PgDSN = pgDSN
	cfg.TigerBeetleAddresses = tbAddresses
	cfg.TigerBeetleClusterID = tbClusterID

	client, err := billing.NewClient(tbClient, pg, sc, cfg)
	if err != nil {
		tbClient.Close()
		pg.Close()
		return nil, nil, fmt.Errorf("create billing client: %w", err)
	}

	cleanup := func() {
		tbClient.Close()
		pg.Close()
	}

	return client, cleanup, nil
}

// Stub CLI subcommands per spec §8.3 — Phase 7 implementations.

func billingDepositCreditsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "deposit-credits",
		Short: "Deposit subscription credits for the current period",
		RunE: func(cmd *cobra.Command, args []string) error {
			log.Println("billing deposit-credits: not yet implemented (Phase 7)")
			return nil
		},
	}
}

func billingExpireCreditsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "expire-credits",
		Short: "Expire credit grants past their expiry date",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, cleanup, err := newBillingClient()
			if err != nil {
				return err
			}
			defer cleanup()

			result, err := client.ExpireCredits(cmd.Context())
			if err != nil {
				return fmt.Errorf("expire credits: %w", err)
			}
			log.Printf("expire-credits: checked=%d expired=%d failed=%d units=%d",
				result.GrantsChecked, result.GrantsExpired, result.GrantsFailed, result.UnitsExpired)
			if result.GrantsFailed > 0 {
				for _, e := range result.Errors {
					log.Printf("  error: %v", e)
				}
			}
			return nil
		},
	}
}

func billingReconcileCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "reconcile",
		Short: "Reconcile TigerBeetle balances against PostgreSQL",
		RunE: func(cmd *cobra.Command, args []string) error {
			log.Println("billing reconcile: not yet implemented (Phase 7)")
			return nil
		},
	}
}

func billingTrustTierEvaluateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "trust-tier-evaluate",
		Short: "Evaluate and update org trust tiers",
		RunE: func(cmd *cobra.Command, args []string) error {
			log.Println("billing trust-tier-evaluate: not yet implemented (Phase 7)")
			return nil
		},
	}
}

func billingCanaryCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "canary",
		Short: "Run billing canary: reserve → sleep → settle → verify",
		RunE: func(cmd *cobra.Command, args []string) error {
			log.Println("billing canary: not yet implemented (Phase 7)")
			return nil
		},
	}
}

func billingDlqCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "dlq",
		Short: "List, retry, or acknowledge dead-letter tasks",
		RunE: func(cmd *cobra.Command, args []string) error {
			log.Println("billing dlq: not yet implemented (Phase 7)")
			return nil
		},
	}
}
