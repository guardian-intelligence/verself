package main

import (
	"errors"
	"flag"
	"fmt"
	"os"

	tb "github.com/tigerbeetle/tigerbeetle-go"
	opch "github.com/verself/operator-runtime/clickhouse"
	opruntime "github.com/verself/operator-runtime/runtime"
	optb "github.com/verself/operator-runtime/tigerbeetle"
)

type dbTBOptions struct {
	dbRuntimeOptions
	remotePath string
	runAsUser  string
	clusterID  string
	addresses  string
}

func cmdDBTB(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("db tb: missing subcommand (try `shell`, `query-accounts`, or `lookup-account`)")
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "shell":
		return cmdDBTBShell(rest)
	case "query-accounts":
		return cmdDBTBQueryAccounts(rest)
	case "lookup-account":
		return cmdDBTBLookupAccount(rest)
	default:
		return fmt.Errorf("db tb: unknown subcommand: %s", sub)
	}
}

func cmdDBTBShell(args []string) error {
	fs := flagSet("db tb shell")
	opts := addDBTBShellFlags(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	return runDBRuntime("db.tb.shell", opts.dbRuntimeOptions, true, func(rt *opruntime.Runtime, _ *opch.Client) error {
		return optb.RunShell(rt.Ctx, rt, tigerBeetleConfig(opts), os.Stdin, os.Stdout, os.Stderr)
	})
}

func cmdDBTBQueryAccounts(args []string) error {
	fs := flagSet("db tb query-accounts")
	opts := addDBTBFlags(fs)
	limit := fs.Uint("limit", 10, "Maximum accounts to return")
	ledger := fs.Uint("ledger", 0, "TigerBeetle ledger filter (0 disables the filter)")
	code := fs.Uint("code", 0, "TigerBeetle code filter (0 disables the filter)")
	userData128 := fs.String("user-data-128", "", "Uint128 user_data_128 filter (decimal or 0x-prefixed hex)")
	userData64 := fs.Uint64("user-data-64", 0, "user_data_64 filter (0 disables the filter)")
	userData32 := fs.Uint("user-data-32", 0, "user_data_32 filter (0 disables the filter)")
	timestampMin := fs.Uint64("timestamp-min", 0, "Minimum TigerBeetle timestamp filter")
	timestampMax := fs.Uint64("timestamp-max", 0, "Maximum TigerBeetle timestamp filter")
	reversed := fs.Bool("reversed", false, "Return newest accounts first")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *limit == 0 || *limit > 8189 {
		return fmt.Errorf("db tb query-accounts: --limit must be between 1 and 8189 (got %d)", *limit)
	}
	if *ledger > uint(^uint32(0)) {
		return fmt.Errorf("db tb query-accounts: --ledger exceeds uint32: %d", *ledger)
	}
	if *code > uint(^uint16(0)) {
		return fmt.Errorf("db tb query-accounts: --code exceeds uint16: %d", *code)
	}
	if *userData32 > uint(^uint32(0)) {
		return fmt.Errorf("db tb query-accounts: --user-data-32 exceeds uint32: %d", *userData32)
	}
	var parsedUserData128 tb.Uint128
	var err error
	if *userData128 != "" {
		parsedUserData128, err = optb.ParseUint128(*userData128)
		if err != nil {
			return err
		}
	}
	filter := tb.QueryFilter{
		UserData128:  parsedUserData128,
		UserData64:   *userData64,
		UserData32:   uint32(*userData32),
		Ledger:       uint32(*ledger),
		Code:         uint16(*code),
		TimestampMin: *timestampMin,
		TimestampMax: *timestampMax,
		Limit:        uint32(*limit),
		Flags:        optb.QueryFilterFlags(*reversed),
	}
	return runDBRuntime("db.tb.query_accounts", opts.dbRuntimeOptions, false, func(rt *opruntime.Runtime, _ *opch.Client) error {
		client, err := optb.OpenClient(rt.Ctx, rt, tigerBeetleConfig(opts))
		if err != nil {
			return err
		}
		defer func() { _ = client.Close() }()
		accounts, err := client.QueryAccounts(filter)
		if err != nil {
			return err
		}
		return opruntime.PrintTable(os.Stdout, optb.AccountsTable(accounts))
	})
}

func cmdDBTBLookupAccount(args []string) error {
	fs := flagSet("db tb lookup-account")
	opts := addDBTBFlags(fs)
	id := fs.String("id", "", "Account ID as decimal or 0x-prefixed Uint128 hex")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *id == "" {
		return errors.New("db tb lookup-account: --id is required")
	}
	parsedID, err := optb.ParseUint128(*id)
	if err != nil {
		return err
	}
	return runDBRuntime("db.tb.lookup_account", opts.dbRuntimeOptions, false, func(rt *opruntime.Runtime, _ *opch.Client) error {
		client, err := optb.OpenClient(rt.Ctx, rt, tigerBeetleConfig(opts))
		if err != nil {
			return err
		}
		defer func() { _ = client.Close() }()
		accounts, err := client.LookupAccounts([]tb.Uint128{parsedID})
		if err != nil {
			return err
		}
		return opruntime.PrintTable(os.Stdout, optb.AccountsTable(accounts))
	})
}

func addDBTBFlags(fs *flag.FlagSet) *dbTBOptions {
	opts := &dbTBOptions{}
	addDBRuntimeFlags(&opts.dbRuntimeOptions)
	fs.StringVar(&opts.site, "site", opts.site, "Deploy site")
	fs.StringVar(&opts.repoRoot, "repo-root", "", "verself-sh checkout root (defaults to cwd)")
	fs.StringVar(&opts.device, "device", "", "Operator device name (defaults to the single onboarded device)")
	fs.StringVar(&opts.clusterID, "cluster", envOr("TIGERBEETLE_CLUSTER_ID", optb.DefaultClusterID), "TigerBeetle cluster ID")
	fs.StringVar(&opts.addresses, "addresses", envOr("TIGERBEETLE_ADDRESSES", optb.DefaultAddresses), "TigerBeetle addresses visible from the worker")
	return opts
}

func addDBTBShellFlags(fs *flag.FlagSet) *dbTBOptions {
	opts := addDBTBFlags(fs)
	fs.StringVar(&opts.remotePath, "remote-path", envOr("TIGERBEETLE_PATH", optb.DefaultRemotePath), "Remote tigerbeetle binary path")
	fs.StringVar(&opts.runAsUser, "run-as-user", envOr("TIGERBEETLE_RUN_AS_USER", optb.DefaultRunAsUser), "Remote Unix user for the TigerBeetle REPL")
	return opts
}

func tigerBeetleConfig(opts *dbTBOptions) optb.Config {
	return optb.Config{
		RemotePath: opts.remotePath,
		RunAsUser:  opts.runAsUser,
		ClusterID:  opts.clusterID,
		Addresses:  opts.addresses,
	}
}
