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

var hostConfigurationPathspecs = []string{
	"MODULE.bazel",
	"MODULE.bazel.lock",
	"src/host-configuration",
	"src/vm-orchestrator",
	"src/vm-guest-telemetry",
	"src/domain-transfer-objects/go",
	"src/observability/go/otel",
}

type hostConfigurationDecision struct {
	Run          bool
	Reason       string
	BaseRunKey   string
	BaseSHA      string
	ChangedPaths []string
	SkipTags     []string
}

func decideHostConfigurationConvergence(ctx context.Context, rt *runtime.Runtime, site, sha, scope, repoRoot string) (hostConfigurationDecision, error) {
	ctx, span := rt.Tracer.Start(ctx, "verself_deploy.host_configuration.change_detect",
		trace.WithSpanKind(trace.SpanKindInternal),
		trace.WithAttributes(
			attribute.String("verself.site", site),
			attribute.String("verself.deploy_scope", scope),
			attribute.String("verself.deploy_sha", sha),
			attribute.StringSlice("host_configuration.pathspecs", hostConfigurationPathspecs),
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

	changedPaths, err := gitChangedHostConfigurationInputs(ctx, repoRoot, last.Sha, sha)
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
	}
	if len(decision.ChangedPaths) > 0 {
		attrs = append(attrs, attribute.StringSlice("host_configuration.changed_paths", firstStrings(decision.ChangedPaths, 20)))
	}
	if len(decision.SkipTags) > 0 {
		attrs = append(attrs, attribute.StringSlice("host_configuration.skip_tags", decision.SkipTags))
	}
	span.SetAttributes(attrs...)
	span.SetStatus(codes.Ok, "")
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
		"src/vm-guest-telemetry/",
		"src/vm-orchestrator/cmd/vm-bridge/",
		"src/vm-orchestrator/guest-images/",
		"src/vm-orchestrator/vmproto/",
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
		"src/vm-orchestrator/",
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

func gitChangedHostConfigurationInputs(ctx context.Context, repoRoot, baseSHA, headSHA string) ([]string, error) {
	if baseSHA == "" {
		return nil, fmt.Errorf("host configuration change detection: base SHA is required")
	}
	if headSHA == "" {
		return nil, fmt.Errorf("host configuration change detection: head SHA is required")
	}
	if baseSHA == headSHA {
		return nil, nil
	}
	args := []string{"diff", "--name-only", "--diff-filter=ACDMRT", baseSHA, headSHA, "--"}
	args = append(args, hostConfigurationPathspecs...)
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = repoRoot
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("host configuration change detection: git diff: %w: %s", err, strings.TrimSpace(string(out)))
	}
	var paths []string
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			paths = append(paths, line)
		}
	}
	return paths, nil
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
