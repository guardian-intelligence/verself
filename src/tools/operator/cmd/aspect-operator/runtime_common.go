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

const operatorCommandBudget = 90 * time.Second

type operatorRuntimeOptions struct {
	site     string
	repoRoot string
}

func addOperatorRuntimeFlags(opts *operatorRuntimeOptions) {
	if opts.site == "" {
		opts.site = envOr("VERSELF_SITE", opruntime.DefaultSite)
	}
}

// runOperatorRuntime runs an automated (non-interactive) operator command bounded
// by commandBudget. A zero budget falls back to the default operatorCommandBudget.
// Operator commands never drive an interactive TTY/sign-in: they assume a valid
// Pomerium session (re-established out of band via `aspect operator device`) and
// fail loud otherwise. Long automated commands (e.g. the service-discovery canary)
// pass an explicit budget >= their duration rather than flipping into interactive
// mode to dodge the default cap under non-TTY automation.
func runOperatorRuntime(command string, opts operatorRuntimeOptions, commandBudget time.Duration, chConfig opch.Config, fn func(*opruntime.Runtime, *opch.Client) error) error {
	parentCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if commandBudget <= 0 {
		commandBudget = operatorCommandBudget
	}
	ctx, cancel := context.WithTimeout(parentCtx, commandBudget)
	defer cancel()
	started := time.Now()
	return opruntime.Run(ctx, opruntime.Options{
		ServiceName:    serviceName,
		ServiceVersion: serviceVersion,
		Command:        command,
		RepoRoot:       opts.repoRoot,
		Site:           opts.site,
		NeedSSH:        true,
		NeedOTel:       true,
		Interactive:    false,
		UseRecovery:    opruntime.UseRecoveryFromEnv(),
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
