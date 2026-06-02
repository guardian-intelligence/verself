package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/verself/deployment-service/controlplane"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "substrate-control-plane-apply: "+err.Error())
		os.Exit(1)
	}
}

func run(args []string) error {
	cfg := controlplane.ApplyConfig{}
	fs := flag.NewFlagSet("substrate-control-plane-apply", flag.ContinueOnError)
	fs.StringVar(&cfg.BundlePath, "bundle", "", "Control-plane JSON or gzip-compressed JSON bundle.")
	fs.StringVar(&cfg.RuntimeSeedPath, "openbao-runtime-seed", "", "Filtered OpenBao runtime seed JSON.")
	fs.StringVar(&cfg.OpenBaoAddr, "openbao-addr", "", "OpenBao API address.")
	fs.StringVar(&cfg.OpenBaoCACert, "openbao-ca-cert", "", "OpenBao CA certificate path.")
	fs.StringVar(&cfg.PostgresRuntime, "postgres-runtime", "", "PostgreSQL runtime root.")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected positional args: %v", fs.Args())
	}
	if cfg.BundlePath == "" {
		cfg.BundlePath = "local/bundle.json"
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	result, err := controlplane.ApplyBundleFile(ctx, cfg)
	if err != nil {
		return err
	}
	fmt.Printf(
		"substrate-control-plane: runtime_secrets=%d site_secrets=%d produced_secrets=%d nomad_roles=%d postgres_roles=%d databases=%d peer_mappings=%d publications=%d\n",
		result.RuntimeSecrets,
		result.SiteSecrets,
		result.ProducedSecrets,
		result.NomadRoles,
		result.PostgresRoles,
		result.Databases,
		result.PeerMappings,
		result.Publications,
	)
	return nil
}
