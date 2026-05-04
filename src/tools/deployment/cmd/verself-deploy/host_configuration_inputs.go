package main

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/verself/deployment-tools/internal/runtime"
)

const (
	hostConfigurationComponent               = "host-configuration"
	hostConfigurationReasonNoPreviousSuccess = "no_previous_success"
	hostConfigurationReasonInputsChanged     = "host_inputs_changed"
	hostConfigurationReasonInputsUnchanged   = "host_inputs_unchanged"
)

var hostConfigurationDiffPathspecs = []string{
	"MODULE.bazel",
	"MODULE.bazel.lock",
	"src/host-configuration",
	"src/substrate/vm-orchestrator",
	"src/substrate/vm-guest-telemetry",
	"src/domain-transfer-objects/go",
	"src/tools/observability/go/otel",
}

type hostConfigurationDecision struct {
	Run          bool
	Reason       string
	BaseRunKey   string
	BaseSHA      string
	ChangedPaths []string
	IgnoredPaths []string
	SkipTags     []string
}

func decideHostConfigurationConvergence(ctx context.Context, rt *runtime.Runtime, site, sha, scope, repoRoot string) (hostConfigurationDecision, error) {
	ctx, span := rt.Tracer.Start(ctx, "verself_deploy.host_configuration.change_detect",
		trace.WithSpanKind(trace.SpanKindInternal),
		trace.WithAttributes(
			attribute.String("verself.site", site),
			attribute.String("verself.deploy_scope", scope),
			attribute.String("verself.deploy_sha", sha),
			attribute.StringSlice("host_configuration.pathspecs", hostConfigurationDiffPathspecs),
		),
	)
	defer span.End()

	last, ok, err := rt.DeployDB.LastSucceededDeploy(ctx, site, scope)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return hostConfigurationDecision{}, err
	}
	if !ok {
		decision := hostConfigurationDecision{
			Run:    true,
			Reason: hostConfigurationReasonNoPreviousSuccess,
		}
		recordHostConfigurationDecision(span, decision)
		return decision, nil
	}

	changedPaths, ignoredPaths, err := gitChangedHostConfigurationInputs(ctx, repoRoot, last.Sha, sha)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return hostConfigurationDecision{}, err
	}
	decision := hostConfigurationDecision{
		Run:          len(changedPaths) > 0,
		Reason:       hostConfigurationReasonInputsUnchanged,
		BaseRunKey:   last.RunKey,
		BaseSHA:      last.Sha,
		ChangedPaths: changedPaths,
		IgnoredPaths: ignoredPaths,
	}
	if decision.Run {
		decision.Reason = hostConfigurationReasonInputsChanged
		decision.SkipTags = hostConfigurationSkipTags(changedPaths)
	}
	recordHostConfigurationDecision(span, decision)
	return decision, nil
}

func recordHostConfigurationDecision(span trace.Span, decision hostConfigurationDecision) {
	attrs := []attribute.KeyValue{
		attribute.Bool("host_configuration.run", decision.Run),
		attribute.String("host_configuration.reason", decision.Reason),
		attribute.String("host_configuration.base_run_key", decision.BaseRunKey),
		attribute.String("host_configuration.base_sha", decision.BaseSHA),
		attribute.Int("host_configuration.changed_path_count", len(decision.ChangedPaths)),
		attribute.Int("host_configuration.ignored_path_count", len(decision.IgnoredPaths)),
	}
	if len(decision.ChangedPaths) > 0 {
		attrs = append(attrs, attribute.StringSlice("host_configuration.changed_paths", firstStrings(decision.ChangedPaths, 20)))
	}
	if len(decision.SkipTags) > 0 {
		attrs = append(attrs, attribute.StringSlice("host_configuration.skip_tags", decision.SkipTags))
	}
	if len(decision.IgnoredPaths) > 0 {
		attrs = append(attrs, attribute.StringSlice("host_configuration.ignored_paths", firstStrings(decision.IgnoredPaths, 20)))
	}
	span.SetAttributes(attrs...)
	span.SetStatus(codes.Ok, "")
}

func hostConfigurationPathRequiresAnsible(path string) bool {
	path = hostConfigurationNormalizePath(path)
	if path == "" {
		return false
	}
	if path == "MODULE.bazel" || path == "MODULE.bazel.lock" {
		return true
	}
	if path == "src/host-configuration/ansible/group_vars/all/catalog.yml" {
		return true
	}
	if strings.HasPrefix(path, "src/host-configuration/components/") {
		return hostConfigurationComponentPathRequiresAnsible(path)
	}
	if hasAnyPathPrefix(path,
		"src/host-configuration/ansible/",
		"src/host-configuration/binaries/",
		"src/substrate/vm-orchestrator/",
		"src/substrate/vm-guest-telemetry/",
		"src/domain-transfer-objects/go/",
		"src/tools/observability/go/otel/",
	) {
		return true
	}
	return false
}

func hostConfigurationComponentPathRequiresAnsible(path string) bool {
	rest := strings.TrimPrefix(path, "src/host-configuration/components/")
	parts := strings.Split(rest, "/")
	if len(parts) < 2 {
		return false
	}
	switch parts[1] {
	case "defaults", "files", "handlers", "meta", "migrations", "tasks", "templates", "vars":
		return true
	default:
		return false
	}
}

func hostConfigurationSkipTags(changedPaths []string) []string {
	var skipTags []string
	if !hostConfigurationAnyPathMatches(changedPaths, hostConfigurationPathRequiresGuestRootFS) {
		skipTags = append(skipTags, "guest_rootfs")
	}
	if !hostConfigurationAnyPathMatches(changedPaths, hostConfigurationPathRequiresFirecracker) {
		skipTags = append(skipTags, "firecracker")
	}
	return skipTags
}

func hostConfigurationAnyPathMatches(paths []string, match func(string) bool) bool {
	for _, path := range paths {
		if match(hostConfigurationNormalizePath(path)) {
			return true
		}
	}
	return false
}

func hostConfigurationPathRequiresGuestRootFS(path string) bool {
	if path == "MODULE.bazel" || path == "MODULE.bazel.lock" {
		return true
	}
	if path == "src/host-configuration/ansible/group_vars/all/catalog.yml" {
		return true
	}
	return hasAnyPathPrefix(path,
		"src/host-configuration/ansible/roles/guest_rootfs/",
		"src/substrate/vm-guest-telemetry/",
		"src/substrate/vm-orchestrator/cmd/vm-bridge/",
		"src/substrate/vm-orchestrator/guest-images/",
		"src/substrate/vm-orchestrator/vmproto/",
	)
}

func hostConfigurationPathRequiresFirecracker(path string) bool {
	if hostConfigurationPathRequiresGuestRootFS(path) {
		return true
	}
	return hasAnyPathPrefix(path,
		"src/host-configuration/ansible/group_vars/workers/",
		"src/host-configuration/ansible/group_vars/all/topology/ops.yml",
		"src/host-configuration/ansible/host-files/etc/nftables.d/firecracker.nft",
		"src/host-configuration/ansible/playbooks/tasks/firecracker-preflight.yml",
		"src/host-configuration/ansible/roles/firecracker/",
		"src/substrate/vm-orchestrator/",
	)
}

func hostConfigurationNormalizePath(path string) string {
	path = strings.TrimSpace(path)
	path = strings.TrimPrefix(path, "./")
	return strings.TrimPrefix(path, "/")
}

func hasAnyPathPrefix(path string, prefixes ...string) bool {
	for _, prefix := range prefixes {
		if path == strings.TrimSuffix(prefix, "/") || strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}

func gitChangedHostConfigurationInputs(ctx context.Context, repoRoot, baseSHA, headSHA string) ([]string, []string, error) {
	if baseSHA == "" {
		return nil, nil, fmt.Errorf("host configuration change detection: base SHA is required")
	}
	if headSHA == "" {
		return nil, nil, fmt.Errorf("host configuration change detection: head SHA is required")
	}
	if baseSHA == headSHA {
		return nil, nil, nil
	}
	args := []string{"diff", "--name-only", "--diff-filter=ACDMRT", baseSHA, headSHA, "--"}
	args = append(args, hostConfigurationDiffPathspecs...)
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = repoRoot
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, nil, fmt.Errorf("host configuration change detection: git diff: %w: %s", err, strings.TrimSpace(string(out)))
	}
	var ansiblePaths []string
	var ignoredPaths []string
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if hostConfigurationPathRequiresAnsible(line) {
			ansiblePaths = append(ansiblePaths, line)
		} else {
			ignoredPaths = append(ignoredPaths, line)
		}
	}
	return ansiblePaths, ignoredPaths, nil
}

func firstStrings(values []string, n int) []string {
	if len(values) <= n {
		return values
	}
	return values[:n]
}

func shortSHA(sha string) string {
	if len(sha) <= 12 {
		return sha
	}
	return sha[:12]
}
