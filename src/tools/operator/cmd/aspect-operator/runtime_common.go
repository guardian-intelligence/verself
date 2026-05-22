package main

import (
	"context"
	"errors"
	"os"
	"os/signal"
	"strings"
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
		Interactive:    interactive,
		UseRecovery:    operatorUseRecovery(),
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

// operatorUseRecovery reports whether commands should reach the host over the
// out-of-band WireGuard recovery SSH transport instead of the Pomerium native
// SSH path. This is the unattended o11y path: it skips the interactive device
// sign-in at the cost of bypassing Pomerium identity-aware access.
func operatorUseRecovery() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("VERSELF_OPERATOR_SSH_TRANSPORT"))) {
	case "recovery", "wireguard", "wg":
		return true
	default:
		return false
	}
}
