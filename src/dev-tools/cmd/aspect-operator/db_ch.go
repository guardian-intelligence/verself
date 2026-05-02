package main

import (
	"errors"
	"flag"
	"fmt"
	"os"

	opch "github.com/verself/operator-runtime/clickhouse"
	opruntime "github.com/verself/operator-runtime/runtime"
)

type dbCHOptions struct {
	dbRuntimeOptions
	database string
}

func cmdDBCH(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("db ch: missing subcommand (try `query` or `schemas`)")
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "query":
		return cmdDBCHQuery(rest)
	case "schemas":
		return cmdDBCHSchemas(rest)
	default:
		return fmt.Errorf("db ch: unknown subcommand: %s", sub)
	}
}

func cmdDBCHQuery(args []string) error {
	fs := flagSet("db ch query")
	opts := addDBCHFlags(fs)
	query := fs.String("query", "", "SQL to execute")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *query == "" {
		return errors.New("db ch query: --query is required")
	}
	return runDBRuntimeWithClickHouse("db.ch.query", opts.dbRuntimeOptions, false, opch.Config{Database: opts.database}, func(rt *opruntime.Runtime, ch *opch.Client) error {
		table, err := ch.QueryTable(rt.Ctx, *query)
		if err != nil {
			return err
		}
		return opruntime.PrintTable(os.Stdout, table)
	})
}

func cmdDBCHSchemas(args []string) error {
	fs := flagSet("db ch schemas")
	opts := addDBCHFlags(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	q := "SELECT concat(database, '.', name, '\n', create_table_query, '\n') FROM system.tables WHERE database IN ('verself', 'default') AND name NOT LIKE '.%' ORDER BY database, name"
	return runDBRuntimeWithClickHouse("db.ch.schemas", opts.dbRuntimeOptions, false, opch.Config{Database: opts.database}, func(rt *opruntime.Runtime, ch *opch.Client) error {
		lines, err := ch.QuerySingleStringColumn(rt.Ctx, q)
		if err != nil {
			return err
		}
		for _, line := range lines {
			fmt.Fprintln(os.Stdout, line)
		}
		return nil
	})
}

func addDBCHFlags(fs *flag.FlagSet) *dbCHOptions {
	opts := &dbCHOptions{}
	addDBRuntimeFlags(&opts.dbRuntimeOptions)
	fs.StringVar(&opts.site, "site", opts.site, "Deploy site")
	fs.StringVar(&opts.repoRoot, "repo-root", "", "verself-sh checkout root (defaults to cwd)")
	fs.StringVar(&opts.device, "device", "", "Operator device name (defaults to the single onboarded device)")
	fs.StringVar(&opts.database, "database", opch.DefaultDatabase, "ClickHouse database")
	return opts
}
