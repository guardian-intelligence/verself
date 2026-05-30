package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/verself/deployment-tools/internal/deploymodel"
	"github.com/verself/deployment-tools/internal/identity"
	"github.com/verself/deployment-tools/internal/runtime"
)

const (
	canaryKindBrowser = "browser"
	canaryKindCLI     = "cli"

	canarySizeNone   = "none"
	canarySizeMedium = "medium"
	canarySizeLarge  = "large"
	canarySizeAll    = "all"

	canaryScopePostDeploy = "post_deploy"
)

var (
	errCanaryFailed = errors.New("post-deploy canary failed")

	canarySelections = []string{
		canarySizeNone,
		canarySizeMedium,
		canarySizeLarge,
		canarySizeAll,
	}
)

type canaryOptions struct {
	Site      string
	SHA       string
	RepoRoot  string
	Size      string
	Component string
	JobID     string
}

func runCanary(args []string) int {
	fs := flag.NewFlagSet("canary", flag.ContinueOnError)
	site := fs.String("site", "prod", "deployment site")
	sha := fs.String("sha", "", "git SHA used for canary correlation")
	repoRoot := fs.String("repo-root", "", "verself-sh checkout root")
	size := fs.String("size", canarySizeMedium, "canary size to run: medium, large, or all")
	component := fs.String("component", "", "optional component key filter")
	jobID := fs.String("job-id", "", "optional Nomad job id filter")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	rr := *repoRoot
	if rr == "" {
		cwd, err := os.Getwd()
		if err != nil {
			fmt.Fprintf(os.Stderr, "verself-deploy canary: cwd: %v\n", err)
			return 1
		}
		rr = cwd
	}
	if err := canary(context.Background(), canaryOptions{
		Site:      *site,
		SHA:       *sha,
		RepoRoot:  rr,
		Size:      *size,
		Component: *component,
		JobID:     *jobID,
	}); err != nil {
		fmt.Fprintf(os.Stderr, "verself-deploy canary: %v\n", err)
		return 1
	}
	return 0
}

func canary(ctx context.Context, opts canaryOptions) error {
	if opts.Site == "" {
		return fmt.Errorf("site is required")
	}
	if opts.RepoRoot == "" {
		return fmt.Errorf("repo root is required")
	}
	if opts.Size == canarySizeNone || !validCanarySelection(opts.Size) {
		return fmt.Errorf("size must be one of %s", canarySelectionUsageWithoutNone())
	}
	snap, err := identity.Generate(identity.GenerateOptions{
		Site:  opts.Site,
		Sha:   opts.SHA,
		Scope: canaryScopePostDeploy,
		Kind:  "post-deploy-canary",
	})
	if err != nil {
		return fmt.Errorf("derive identity: %w", err)
	}
	snap.ApplyEnv()
	resolvedSHA := snap.Get("VERSELF_DEPLOY_SHA")
	if resolvedSHA == "" {
		resolvedSHA = snap.Get("VERSELF_COMMIT_SHA")
	}

	rt, err := runtime.Init(ctx, runtime.Options{
		ServiceName:    serviceName,
		ServiceVersion: serviceVersion,
		Site:           opts.Site,
		RepoRoot:       opts.RepoRoot,
	})
	if err != nil {
		return fmt.Errorf("runtime init: %w", err)
	}
	defer rt.Close()

	runCtx, span := rt.Tracer.Start(rt.Ctx, "verself_deploy.canary.run",
		trace.WithSpanKind(trace.SpanKindInternal),
		trace.WithAttributes(
			attribute.String("verself.site", opts.Site),
			attribute.String("verself.deploy_scope", canaryScopePostDeploy),
			attribute.String("verself.canary_size", opts.Size),
			attribute.String("verself.component", opts.Component),
			attribute.String("nomad.job_id", opts.JobID),
		),
	)
	defer span.End()

	jobs, err := loadCanaryJobs(runCtx, opts.RepoRoot, opts.Site, opts.Component, opts.JobID)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	count := countSelectedCanaries(jobs, opts.Size)
	span.SetAttributes(
		attribute.Int("verself.nomad_job_count", len(jobs)),
		attribute.Int("verself.canary_count", count),
	)
	if count == 0 {
		err := fmt.Errorf("no %s post-deploy canaries declared for selected components", opts.Size)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}

	for _, job := range jobs {
		if err := runPostDeployCanaries(runCtx, rt, opts.RepoRoot, opts.Site, resolvedSHA, job, opts.Size); err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			return err
		}
	}
	span.SetStatus(codes.Ok, "")
	return nil
}

func loadCanaryJobs(ctx context.Context, repoRoot, site, componentFilter, jobFilter string) ([]deploymodel.NomadJob, error) {
	_, descriptorPaths, err := buildNomadComponentDescriptors(ctx, repoRoot, "--output_groups=nomad_descriptor")
	if err != nil {
		return nil, err
	}
	descriptors, err := loadNomadComponentDescriptors(site, descriptorPaths)
	if err != nil {
		return nil, err
	}
	ordered, err := orderNomadComponents(descriptors)
	if err != nil {
		return nil, err
	}
	jobs := make([]deploymodel.NomadJob, 0, len(ordered))
	for _, component := range ordered {
		if componentFilter != "" && component.Component != componentFilter {
			continue
		}
		if jobFilter != "" && component.JobID != jobFilter {
			continue
		}
		jobs = append(jobs, deploymodel.NomadJob{
			JobID:              component.JobID,
			Component:          component.Component,
			DependsOn:          append([]string(nil), component.Requires...),
			PostDeployCanaries: append([]deploymodel.PostDeployCanary(nil), component.PostDeployCanaries...),
		})
	}
	if len(jobs) == 0 {
		return nil, fmt.Errorf("no Nomad components matched site=%q component=%q job_id=%q", site, componentFilter, jobFilter)
	}
	return jobs, nil
}

func validatePostDeployCanary(canary deploymodel.PostDeployCanary) error {
	if canary.Label == "" || canary.Target == "" {
		return fmt.Errorf("label and target are required")
	}
	switch canary.Kind {
	case canaryKindBrowser, canaryKindCLI:
	default:
		return fmt.Errorf("kind must be %q or %q", canaryKindCLI, canaryKindBrowser)
	}
	switch canary.Size {
	case canarySizeMedium, canarySizeLarge:
	default:
		return fmt.Errorf("size must be %q or %q", canarySizeMedium, canarySizeLarge)
	}
	if canary.Timeout != "" {
		if _, err := time.ParseDuration(canary.Timeout); err != nil {
			return fmt.Errorf("timeout %q is not a valid duration: %w", canary.Timeout, err)
		}
	}
	return nil
}

func validCanarySelection(selection string) bool {
	for _, candidate := range canarySelections {
		if selection == candidate {
			return true
		}
	}
	return false
}

func canarySelectionUsage() string {
	return strings.Join(canarySelections, ", ")
}

func canarySelectionUsageWithoutNone() string {
	return strings.Join([]string{canarySizeMedium, canarySizeLarge, canarySizeAll}, ", ")
}

func runPostDeployCanaries(ctx context.Context, rt *runtime.Runtime, repoRoot, site, sha string, job deploymodel.NomadJob, selection string) error {
	selected := selectPostDeployCanaries(job.PostDeployCanaries, selection)
	if len(selected) == 0 {
		return nil
	}
	ctx, span := rt.Tracer.Start(ctx, "verself_deploy.canary.batch",
		trace.WithAttributes(
			attribute.String("verself.component", job.Component),
			attribute.String("nomad.job_id", job.JobID),
			attribute.String("verself.canary_selection", selection),
			attribute.Int("verself.canary_count", len(selected)),
		),
	)
	defer span.End()
	for _, canary := range selected {
		if err := runPostDeployCanary(ctx, rt, repoRoot, site, sha, job, canary); err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			return err
		}
	}
	span.SetStatus(codes.Ok, "")
	return nil
}

func runPostDeployCanary(ctx context.Context, rt *runtime.Runtime, repoRoot, site, sha string, job deploymodel.NomadJob, canary deploymodel.PostDeployCanary) error {
	timeout := defaultCanaryTimeout(canary.Size)
	if canary.Timeout != "" {
		parsed, err := time.ParseDuration(canary.Timeout)
		if err != nil {
			return fmt.Errorf("%s: invalid timeout: %w", canary.Label, err)
		}
		timeout = parsed
	}
	canaryCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	canaryCtx, span := rt.Tracer.Start(canaryCtx, "verself_deploy.canary.execute",
		trace.WithAttributes(
			attribute.String("verself.component", job.Component),
			attribute.String("nomad.job_id", job.JobID),
			attribute.String("verself.canary_label", canary.Label),
			attribute.String("verself.canary_kind", canary.Kind),
			attribute.String("verself.canary_size", canary.Size),
			attribute.String("bazel.target", canary.Target),
			attribute.Int64("verself.canary_timeout_ms", timeout.Milliseconds()),
		),
	)
	defer span.End()

	vars := canaryVariables(rt, repoRoot, site, sha, job, canary)
	args := []string{"run", canary.Target, "--"}
	args = append(args, expandCanaryValues(canary.Args, vars)...)
	cmd := exec.CommandContext(canaryCtx, "bazelisk", args...)
	cmd.Dir = repoRoot
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = canaryEnv(rt, canary, vars)

	fmt.Printf("verself-deploy: running %s %s canary %s for %s\n", canary.Size, canary.Kind, canary.Label, job.JobID)
	started := time.Now()
	if err := cmd.Run(); err != nil {
		elapsed := time.Since(started)
		span.SetAttributes(attribute.Int64("verself.duration_ms", elapsed.Milliseconds()))
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return fmt.Errorf("%w: %s target=%s duration=%s: %v", errCanaryFailed, canary.Label, canary.Target, elapsed.Round(time.Millisecond), err)
	}
	elapsed := time.Since(started)
	span.SetAttributes(attribute.Int64("verself.duration_ms", elapsed.Milliseconds()))
	span.SetStatus(codes.Ok, "")
	fmt.Printf("verself-deploy: canary %s passed in %s\n", canary.Label, elapsed.Round(time.Millisecond))
	return nil
}

func selectPostDeployCanaries(canaries []deploymodel.PostDeployCanary, selection string) []deploymodel.PostDeployCanary {
	if selection == canarySizeNone {
		return nil
	}
	out := []deploymodel.PostDeployCanary{}
	for _, canary := range canaries {
		if selection == canarySizeAll || canary.Size == selection {
			out = append(out, canary)
		}
	}
	return out
}

func countSelectedCanaries(jobs []deploymodel.NomadJob, selection string) int {
	count := 0
	for _, job := range jobs {
		count += len(selectPostDeployCanaries(job.PostDeployCanaries, selection))
	}
	return count
}

func defaultCanaryTimeout(size string) time.Duration {
	switch size {
	case canarySizeLarge:
		return 30 * time.Minute
	default:
		return 5 * time.Minute
	}
}

func canaryVariables(rt *runtime.Runtime, repoRoot, site, sha string, job deploymodel.NomadJob, canary deploymodel.PostDeployCanary) map[string]string {
	return map[string]string{
		"canary_kind":    canary.Kind,
		"canary_label":   canary.Label,
		"canary_size":    canary.Size,
		"component":      job.Component,
		"deploy_run_key": rt.Identity.RunKey(),
		"job_id":         job.JobID,
		"repo_root":      repoRoot,
		"sha":            sha,
		"site":           site,
	}
}

func expandCanaryValues(values []string, vars map[string]string) []string {
	if len(values) == 0 {
		return nil
	}
	replacer := canaryReplacer(vars)
	out := make([]string, 0, len(values))
	for _, value := range values {
		out = append(out, replacer.Replace(value))
	}
	return out
}

func canaryEnv(rt *runtime.Runtime, canary deploymodel.PostDeployCanary, vars map[string]string) []string {
	env := append([]string{}, os.Environ()...)
	env = append(env, rt.Identity.AllEnv()...)
	env = append(env,
		"VERSELF_CANARY_KIND="+canary.Kind,
		"VERSELF_CANARY_LABEL="+canary.Label,
		"VERSELF_CANARY_SIZE="+canary.Size,
		"VERSELF_COMPONENT="+vars["component"],
		"VERSELF_NOMAD_JOB_ID="+vars["job_id"],
	)
	keys := make([]string, 0, len(canary.Env))
	for key := range canary.Env {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	replacer := canaryReplacer(vars)
	for _, key := range keys {
		env = append(env, key+"="+replacer.Replace(canary.Env[key]))
	}
	return env
}

func canaryReplacer(vars map[string]string) *strings.Replacer {
	pairs := make([]string, 0, len(vars)*2)
	keys := make([]string, 0, len(vars))
	for key := range vars {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		pairs = append(pairs, "{"+key+"}", vars[key])
	}
	return strings.NewReplacer(pairs...)
}
