package main

import (
	"context"
	"errors"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	opch "github.com/verself/operator-runtime/clickhouse"
	"github.com/verself/operator-runtime/evidence"
	opruntime "github.com/verself/operator-runtime/runtime"
)

const dbQueryBudget = 5 * time.Second

type dbRuntimeOptions struct {
	site     string
	repoRoot string
}

func addDBRuntimeFlags(opts *dbRuntimeOptions) {
	if opts.site == "" {
		opts.site = envOr("VERSELF_SITE", opruntime.DefaultSite)
	}
}

func runDBRuntime(command string, opts dbRuntimeOptions, interactive bool, fn func(*opruntime.Runtime, *opch.Client) error) error {
	return runDBRuntimeWithClickHouse(command, opts, interactive, opch.Config{}, fn)
}

func runDBRuntimeWithClickHouse(command string, opts dbRuntimeOptions, interactive bool, chConfig opch.Config, fn func(*opruntime.Runtime, *opch.Client) error) error {
	parentCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx := parentCtx
	var cancel context.CancelFunc
	if !interactive {
		ctx, cancel = context.WithTimeout(parentCtx, dbQueryBudget)
		defer cancel()
	}
	started := time.Now()
	return opruntime.Run(ctx, opruntime.Options{
		ServiceName:    serviceName,
		ServiceVersion: serviceVersion,
		Command:        command,
		RepoRoot:       opts.repoRoot,
		Site:           opts.site,
		NeedSSH:        true,
		NeedOTel:       true,
	}, func(rt *opruntime.Runtime) error {
		ch, err := opch.OpenOperator(rt.Ctx, rt, chConfig)
		if err != nil {
			return err
		}
		defer func() { _ = ch.Close() }()
		runErr := fn(rt, ch)
		recordErr := evidence.Recorder{ClickHouse: ch}.RecordCommandRun(rt.Ctx, rt, started, command, runErr)
		if runErr != nil {
			return errors.Join(runErr, recordErr)
		}
		return recordErr
	})
}

func envOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func envIntOr(name string, fallback int) int {
	value := os.Getenv(name)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return -1
	}
	return parsed
}
