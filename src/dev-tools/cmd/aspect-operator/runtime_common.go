package main

import (
	"context"
	"errors"
	"os"
	"os/signal"
	"syscall"
	"time"

	opch "github.com/verself/operator-runtime/clickhouse"
	"github.com/verself/operator-runtime/evidence"
	opruntime "github.com/verself/operator-runtime/runtime"
)

const operatorCommandBudget = 30 * time.Second

type operatorRuntimeOptions struct {
	site     string
	repoRoot string
}

func addOperatorRuntimeFlags(opts *operatorRuntimeOptions) {
	if opts.site == "" {
		opts.site = envOr("VERSELF_SITE", opruntime.DefaultSite)
	}
}

func runOperatorRuntime(command string, opts operatorRuntimeOptions, interactive bool, chConfig opch.Config, fn func(*opruntime.Runtime, *opch.Client) error) error {
	parentCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx := parentCtx
	var cancel context.CancelFunc
	if !interactive {
		ctx, cancel = context.WithTimeout(parentCtx, operatorCommandBudget)
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
		chClient, err := opch.OpenOperator(rt.Ctx, rt, chConfig)
		if err != nil {
			return err
		}
		defer func() { _ = chClient.Close() }()
		runErr := fn(rt, chClient)
		recordErr := evidence.Recorder{ClickHouse: chClient}.RecordCommandRun(rt.Ctx, rt, started, command, runErr)
		if runErr != nil {
			return errors.Join(runErr, recordErr)
		}
		return recordErr
	})
}

func contextWithoutCancel(ctx context.Context) context.Context {
	return context.WithoutCancel(ctx)
}
