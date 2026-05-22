package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	verself "github.com/verself/verself-go"
)

func (c CLI) runRuns(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return errors.New("runs command is required")
	}
	switch args[0] {
	case "list", "ls":
		return c.runsList(ctx, args[1:])
	case "get":
		return c.runsGet(ctx, args[1:])
	case "get-run":
		return c.runsGetRun(ctx, args[1:])
	case "logs":
		return c.runsLogs(ctx, args[1:])
	case "search-logs":
		return c.runsSearchLogs(ctx, args[1:])
	case "analytics":
		return c.runsAnalytics(ctx, args[1:])
	default:
		return fmt.Errorf("unknown runs command %q", args[0])
	}
}

func (c CLI) runsList(ctx context.Context, args []string) error {
	fs, serviceFlags := serviceFlagSet("runs list", c.err)
	jsonOut := fs.Bool("json", false, "json output")
	limit := fs.Int64("limit", 50, "page size")
	cursor := fs.String("cursor", "", "pagination cursor")
	sourceKind := fs.String("source-kind", "", "source kind")
	status := fs.String("status", "", "run status")
	repository := fs.String("repository", "", "repository filter")
	workflow := fs.String("workflow", "", "workflow filter")
	branch := fs.String("branch", "", "branch filter")
	runnerClass := fs.String("runner-class", "", "runner class")
	if err := parseInterspersed(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("usage: runs list [--status STATUS] [--json]")
	}
	client, err := c.serviceClient(*serviceFlags)
	if err != nil {
		return err
	}
	page, err := client.Sandbox.ListRuns(ctx, verself.ListSandboxRunsOptions{
		Limit:       *limit,
		Cursor:      *cursor,
		SourceKind:  *sourceKind,
		Status:      *status,
		Repository:  *repository,
		Workflow:    *workflow,
		Branch:      *branch,
		RunnerClass: *runnerClass,
	})
	if err != nil {
		return err
	}
	if *jsonOut {
		return writeJSON(c.out, page)
	}
	for _, run := range page.Runs {
		if err := writeSandboxRun(c.out, run); err != nil {
			return err
		}
	}
	if page.NextCursor != "" {
		return writef(c.out, "next_cursor\t%s\n", page.NextCursor)
	}
	return nil
}

func (c CLI) runsGet(ctx context.Context, args []string) error {
	fs, serviceFlags := serviceFlagSet("runs get", c.err)
	jsonOut := fs.Bool("json", false, "json output")
	if err := parseInterspersed(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("usage: runs get <execution-id> [--json]")
	}
	client, err := c.serviceClient(*serviceFlags)
	if err != nil {
		return err
	}
	run, err := client.Sandbox.GetExecution(ctx, fs.Arg(0))
	if err != nil {
		return err
	}
	if *jsonOut {
		return writeJSON(c.out, run)
	}
	return writeSandboxRun(c.out, run)
}

func (c CLI) runsGetRun(ctx context.Context, args []string) error {
	fs, serviceFlags := serviceFlagSet("runs get-run", c.err)
	jsonOut := fs.Bool("json", false, "json output")
	if err := parseInterspersed(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("usage: runs get-run <run-id> [--json]")
	}
	client, err := c.serviceClient(*serviceFlags)
	if err != nil {
		return err
	}
	run, err := client.Sandbox.GetRun(ctx, fs.Arg(0))
	if err != nil {
		return err
	}
	if *jsonOut {
		return writeJSON(c.out, run)
	}
	return writeSandboxRun(c.out, run)
}

func (c CLI) runsLogs(ctx context.Context, args []string) error {
	fs, serviceFlags := serviceFlagSet("runs logs", c.err)
	jsonOut := fs.Bool("json", false, "json output")
	if err := parseInterspersed(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("usage: runs logs <execution-id> [--json]")
	}
	client, err := c.serviceClient(*serviceFlags)
	if err != nil {
		return err
	}
	logs, err := client.Sandbox.GetExecutionLogs(ctx, fs.Arg(0))
	if err != nil {
		return err
	}
	if *jsonOut {
		return writeJSON(c.out, logs)
	}
	return writef(c.out, "%s", logs.Logs)
}

func (c CLI) runsSearchLogs(ctx context.Context, args []string) error {
	fs, serviceFlags := serviceFlagSet("runs search-logs", c.err)
	jsonOut := fs.Bool("json", false, "json output")
	limit := fs.Int64("limit", 50, "page size")
	cursor := fs.String("cursor", "", "pagination cursor")
	query := fs.String("query", "", "substring search")
	runID := fs.String("run-id", "", "run UUID")
	attemptID := fs.String("attempt-id", "", "attempt UUID")
	sourceKind := fs.String("source-kind", "", "source kind")
	repository := fs.String("repository", "", "repository filter")
	workflow := fs.String("workflow", "", "workflow filter")
	branch := fs.String("branch", "", "branch filter")
	runnerClass := fs.String("runner-class", "", "runner class")
	if err := parseInterspersed(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("usage: runs search-logs [--query TEXT] [--run-id RUN_ID] [--json]")
	}
	client, err := c.serviceClient(*serviceFlags)
	if err != nil {
		return err
	}
	page, err := client.Sandbox.SearchRunLogs(ctx, verself.SearchSandboxRunLogsOptions{
		Limit:       *limit,
		Cursor:      *cursor,
		Query:       *query,
		RunID:       *runID,
		AttemptID:   *attemptID,
		SourceKind:  *sourceKind,
		Repository:  *repository,
		Workflow:    *workflow,
		Branch:      *branch,
		RunnerClass: *runnerClass,
	})
	if err != nil {
		return err
	}
	if *jsonOut {
		return writeJSON(c.out, page)
	}
	for _, result := range page.Results {
		if err := writef(c.out, "%s\t%s\t%d\t%s\t%s", result.ExecutionID, result.AttemptID, result.Seq, result.Stream, result.Chunk); err != nil {
			return err
		}
		if !strings.HasSuffix(result.Chunk, "\n") {
			if err := writef(c.out, "\n"); err != nil {
				return err
			}
		}
	}
	if page.NextCursor != "" {
		return writef(c.out, "next_cursor\t%s\n", page.NextCursor)
	}
	return nil
}

func (c CLI) runsAnalytics(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return errors.New("runs analytics command is required")
	}
	command := args[0]
	fs, serviceFlags := serviceFlagSet("runs analytics "+command, c.err)
	jsonOut := fs.Bool("json", false, "json output")
	start := fs.String("start", "", "RFC3339 inclusive window start")
	end := fs.String("end", "", "RFC3339 inclusive window end")
	if err := parseInterspersed(fs, args[1:]); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("usage: runs analytics jobs|costs|runner-sizing [--json]")
	}
	options, err := sandboxAnalyticsOptions(*start, *end)
	if err != nil {
		return err
	}
	client, err := c.serviceClient(*serviceFlags)
	if err != nil {
		return err
	}
	switch command {
	case "jobs":
		analytics, err := client.Sandbox.GetJobsAnalytics(ctx, options)
		if err != nil {
			return err
		}
		if *jsonOut {
			return writeJSON(c.out, analytics)
		}
		return writef(c.out, "total_runs\t%s\nsucceeded_runs\t%s\nfailed_runs\t%s\n", analytics.TotalRuns, analytics.SucceededRuns, analytics.FailedRuns)
	case "costs":
		analytics, err := client.Sandbox.GetCostsAnalytics(ctx, options)
		if err != nil {
			return err
		}
		if *jsonOut {
			return writeJSON(c.out, analytics)
		}
		return writef(c.out, "reserved_charge_units\t%s\nbilled_charge_units\t%s\nwriteoff_charge_units\t%s\n", analytics.ReservedChargeUnits, analytics.BilledChargeUnits, analytics.WriteoffChargeUnits)
	case "runner-sizing":
		analytics, err := client.Sandbox.GetRunnerSizingAnalytics(ctx, options)
		if err != nil {
			return err
		}
		if *jsonOut {
			return writeJSON(c.out, analytics)
		}
		for _, sample := range analytics.ByRunnerClass {
			if err := writef(c.out, "%s\t%s\t%s\n", sample.RunnerClass, sample.RunCount, sample.P95DurationMs); err != nil {
				return err
			}
		}
		return nil
	default:
		return fmt.Errorf("unknown runs analytics command %q", command)
	}
}

func (c CLI) runSchedules(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return errors.New("schedules command is required")
	}
	switch args[0] {
	case "list", "ls":
		return c.schedulesList(ctx, args[1:])
	case "create":
		return c.schedulesCreate(ctx, args[1:])
	case "get":
		return c.schedulesGet(ctx, args[1:])
	case "pause":
		return c.schedulesLifecycle(ctx, "schedules pause", "pause", args[1:])
	case "resume":
		return c.schedulesLifecycle(ctx, "schedules resume", "resume", args[1:])
	default:
		return fmt.Errorf("unknown schedules command %q", args[0])
	}
}

func (c CLI) schedulesList(ctx context.Context, args []string) error {
	fs, serviceFlags := serviceFlagSet("schedules list", c.err)
	jsonOut := fs.Bool("json", false, "json output")
	limit := fs.Int64("limit", 0, "maximum schedules to return")
	cursor := fs.String("cursor", "", "pagination cursor")
	if err := parseInterspersed(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("usage: schedules list [--json] [--limit N] [--cursor CURSOR]")
	}
	client, err := c.serviceClient(*serviceFlags)
	if err != nil {
		return err
	}
	schedules, err := client.Sandbox.ListSchedules(ctx, verself.ListSandboxExecutionSchedulesOptions{
		Limit:  *limit,
		Cursor: *cursor,
	})
	if err != nil {
		return err
	}
	if *jsonOut {
		return writeJSON(c.out, schedules)
	}
	for _, schedule := range schedules.Schedules {
		if err := writeSandboxSchedule(c.out, schedule); err != nil {
			return err
		}
	}
	if schedules.NextCursor != "" {
		return writef(c.out, "next_cursor\t%s\n", schedules.NextCursor)
	}
	return nil
}

func (c CLI) schedulesCreate(ctx context.Context, args []string) error {
	fs, serviceFlags := serviceFlagSet("schedules create", c.err)
	jsonOut := fs.Bool("json", false, "json output")
	projectID := fs.String("project-id", "", "project UUID")
	sourceRepositoryID := fs.String("source-repository-id", "", "source repository UUID")
	workflowPath := fs.String("workflow-path", "", "workflow path")
	intervalSeconds := fs.String("interval-seconds", "", "interval in seconds")
	displayName := fs.String("display-name", "", "schedule display name")
	ref := fs.String("ref", "", "source ref")
	paused := fs.Bool("paused", false, "create paused")
	idempotencyKey := fs.String("idempotency-key", "", "stable mutation key")
	var inputs keyValueFlag
	fs.Var(&inputs, "input", "workflow input key=value")
	if err := parseInterspersed(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("usage: schedules create --project-id PROJECT_ID --source-repository-id REPO_ID --workflow-path PATH --interval-seconds SECONDS [--json]")
	}
	parsedIntervalSeconds, err := parseInt32Flag(*intervalSeconds, "interval-seconds")
	if err != nil {
		return err
	}
	client, err := c.serviceClient(*serviceFlags)
	if err != nil {
		return err
	}
	schedule, err := client.Sandbox.CreateSchedule(ctx, verself.CreateSandboxExecutionScheduleInput{
		ProjectID:          *projectID,
		SourceRepositoryID: *sourceRepositoryID,
		WorkflowPath:       *workflowPath,
		IntervalSeconds:    parsedIntervalSeconds,
		DisplayName:        *displayName,
		Ref:                *ref,
		Paused:             *paused,
		Inputs:             inputs.mapOrNil(),
		IdempotencyKey:     *idempotencyKey,
	})
	if err != nil {
		return err
	}
	if *jsonOut {
		return writeJSON(c.out, schedule)
	}
	return writeSandboxSchedule(c.out, schedule)
}

func (c CLI) schedulesGet(ctx context.Context, args []string) error {
	fs, serviceFlags := serviceFlagSet("schedules get", c.err)
	jsonOut := fs.Bool("json", false, "json output")
	if err := parseInterspersed(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("usage: schedules get <schedule-id> [--json]")
	}
	client, err := c.serviceClient(*serviceFlags)
	if err != nil {
		return err
	}
	schedule, err := client.Sandbox.GetSchedule(ctx, fs.Arg(0))
	if err != nil {
		return err
	}
	if *jsonOut {
		return writeJSON(c.out, schedule)
	}
	return writeSandboxSchedule(c.out, schedule)
}

func (c CLI) schedulesLifecycle(ctx context.Context, command, action string, args []string) error {
	fs, serviceFlags := serviceFlagSet(command, c.err)
	jsonOut := fs.Bool("json", false, "json output")
	idempotencyKey := fs.String("idempotency-key", "", "stable mutation key")
	if err := parseInterspersed(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: schedules %s <schedule-id> [--json]", action)
	}
	client, err := c.serviceClient(*serviceFlags)
	if err != nil {
		return err
	}
	options := verself.SandboxMutationOptions{IdempotencyKey: *idempotencyKey}
	var schedule verself.SandboxExecutionSchedule
	switch action {
	case "pause":
		schedule, err = client.Sandbox.PauseSchedule(ctx, fs.Arg(0), options)
	case "resume":
		schedule, err = client.Sandbox.ResumeSchedule(ctx, fs.Arg(0), options)
	default:
		return fmt.Errorf("unknown schedule lifecycle action %q", action)
	}
	if err != nil {
		return err
	}
	if *jsonOut {
		return writeJSON(c.out, schedule)
	}
	return writeSandboxSchedule(c.out, schedule)
}

func (c CLI) runBilling(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return errors.New("billing command is required")
	}
	switch args[0] {
	case "entitlements":
		return c.billingEntitlements(ctx, args[1:])
	case "contracts":
		return c.billingContracts(ctx, args[1:])
	case "plans":
		return c.billingPlans(ctx, args[1:])
	case "statement":
		return c.billingStatement(ctx, args[1:])
	default:
		return fmt.Errorf("unknown billing command %q", args[0])
	}
}

func (c CLI) billingEntitlements(ctx context.Context, args []string) error {
	fs, serviceFlags := serviceFlagSet("billing entitlements", c.err)
	jsonOut := fs.Bool("json", false, "json output")
	if err := parseInterspersed(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("usage: billing entitlements [--json]")
	}
	client, err := c.serviceClient(*serviceFlags)
	if err != nil {
		return err
	}
	entitlements, err := client.Billing.GetEntitlements(ctx)
	if err != nil {
		return err
	}
	if *jsonOut {
		return writeJSON(c.out, entitlements)
	}
	return writef(c.out, "%s\t%s\n", entitlements.OrgID, entitlements.Universal.AvailableUnits)
}

func (c CLI) billingContracts(ctx context.Context, args []string) error {
	fs, serviceFlags := serviceFlagSet("billing contracts", c.err)
	jsonOut := fs.Bool("json", false, "json output")
	if err := parseInterspersed(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("usage: billing contracts [--json]")
	}
	client, err := c.serviceClient(*serviceFlags)
	if err != nil {
		return err
	}
	contracts, err := client.Billing.ListContracts(ctx)
	if err != nil {
		return err
	}
	if *jsonOut {
		return writeJSON(c.out, contracts)
	}
	for _, contract := range contracts.Contracts {
		if err := writef(c.out, "%s\t%s\t%s\t%s\n", contract.ContractID, contract.ProductID, contract.PlanID, contract.Status); err != nil {
			return err
		}
	}
	return nil
}

func (c CLI) billingPlans(ctx context.Context, args []string) error {
	fs, serviceFlags := serviceFlagSet("billing plans", c.err)
	jsonOut := fs.Bool("json", false, "json output")
	productID := fs.String("product-id", "", "product id")
	if err := parseInterspersed(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("usage: billing plans --product-id PRODUCT_ID [--json]")
	}
	client, err := c.serviceClient(*serviceFlags)
	if err != nil {
		return err
	}
	plans, err := client.Billing.ListPlans(ctx, verself.BillingListPlansOptions{ProductID: *productID})
	if err != nil {
		return err
	}
	if *jsonOut {
		return writeJSON(c.out, plans)
	}
	for _, plan := range plans.Plans {
		if err := writef(c.out, "%s\t%s\t%s\t%s\n", plan.ProductID, plan.PlanID, plan.Tier, plan.MonthlyAmountCents); err != nil {
			return err
		}
	}
	return nil
}

func (c CLI) billingStatement(ctx context.Context, args []string) error {
	fs, serviceFlags := serviceFlagSet("billing statement", c.err)
	jsonOut := fs.Bool("json", false, "json output")
	productID := fs.String("product-id", "", "product id")
	if err := parseInterspersed(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("usage: billing statement --product-id PRODUCT_ID [--json]")
	}
	client, err := c.serviceClient(*serviceFlags)
	if err != nil {
		return err
	}
	statement, err := client.Billing.GetStatement(ctx, verself.BillingStatementOptions{ProductID: *productID})
	if err != nil {
		return err
	}
	if *jsonOut {
		return writeJSON(c.out, statement)
	}
	return writef(c.out, "%s\t%s\t%s\t%s\n", statement.OrgID, statement.ProductID, statement.PeriodSource, statement.Totals.TotalDueUnits)
}

func writeSandboxRun(w io.Writer, run verself.SandboxExecution) error {
	sourceKind := stringValue(run.SourceKind)
	runnerClass := stringValue(run.RunnerClass)
	return writef(w, "%s\t%s\t%s\t%s\t%s\n", run.ExecutionID, run.RunID, run.Status, sourceKind, runnerClass)
}

func writeSandboxSchedule(w io.Writer, schedule verself.SandboxExecutionSchedule) error {
	displayName := stringValue(schedule.DisplayName)
	return writef(w, "%s\t%s\t%s\t%d\t%s\t%s\n", schedule.ScheduleID, schedule.ProjectID, schedule.State, schedule.IntervalSeconds, schedule.WorkflowPath, displayName)
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

func sandboxAnalyticsOptions(startValue, endValue string) (verself.SandboxAnalyticsOptions, error) {
	var options verself.SandboxAnalyticsOptions
	if strings.TrimSpace(startValue) != "" {
		start, err := time.Parse(time.RFC3339, strings.TrimSpace(startValue))
		if err != nil {
			return options, fmt.Errorf("parse analytics start: %w", err)
		}
		options.Start = start
	}
	if strings.TrimSpace(endValue) != "" {
		end, err := time.Parse(time.RFC3339, strings.TrimSpace(endValue))
		if err != nil {
			return options, fmt.Errorf("parse analytics end: %w", err)
		}
		options.End = end
	}
	return options, nil
}
