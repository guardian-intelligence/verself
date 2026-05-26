package main

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"sort"
	"strings"

	"github.com/verself/deployment-tools/internal/bazelbuild"
)

const (
	nomadComponentsQuery = `kind("nomad_component rule", //src/...)`
)

func queryNomadComponentLabels(ctx context.Context, repoRoot string) ([]string, error) {
	cmd := exec.CommandContext(ctx, "bazelisk", "query", nomadComponentsQuery)
	cmd.Dir = repoRoot
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	body, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("bazelisk query %s: %w: %s", nomadComponentsQuery, err, strings.TrimSpace(stderr.String()))
	}
	labels := []string{}
	for _, line := range strings.Split(string(body), "\n") {
		label := strings.TrimSpace(line)
		if label != "" {
			labels = append(labels, label)
		}
	}
	sort.Strings(labels)
	if len(labels) == 0 {
		return nil, fmt.Errorf("bazel query %s returned no Nomad components", nomadComponentsQuery)
	}
	return labels, nil
}

func buildNomadComponentDescriptors(ctx context.Context, repoRoot string, extraBuildFlags ...string) ([]string, []string, error) {
	labels, err := queryNomadComponentLabels(ctx, repoRoot)
	if err != nil {
		return nil, nil, err
	}
	build, err := bazelbuild.Build(ctx, repoRoot, labels, extraBuildFlags...)
	if err != nil {
		return nil, nil, err
	}
	descriptorPaths := make([]string, 0, len(labels))
	outputGroup := "default"
	for _, flag := range extraBuildFlags {
		if strings.Contains(flag, "nomad_descriptor") {
			outputGroup = "nomad_descriptor"
			break
		}
	}
	for _, label := range labels {
		outputs, err := build.Stream.ResolveOutputGroup(label, outputGroup, repoRoot)
		if err != nil {
			return nil, nil, fmt.Errorf("resolve %s outputs: %w", label, err)
		}
		descriptorPath, err := selectBazelOutput(label, outputs, ".nomad_component.json")
		if err != nil {
			return nil, nil, err
		}
		descriptorPaths = append(descriptorPaths, descriptorPath)
	}
	return labels, descriptorPaths, nil
}

func selectBazelOutput(label string, outputs []string, suffix string) (string, error) {
	matches := make([]string, 0, 1)
	for _, output := range outputs {
		if strings.HasSuffix(output, suffix) {
			matches = append(matches, output)
		}
	}
	if len(matches) != 1 {
		return "", fmt.Errorf("%s must produce exactly one %s output, got %d from %d outputs: %v", label, suffix, len(matches), len(outputs), outputs)
	}
	return matches[0], nil
}

func runNomadComponentTests(ctx context.Context, repoRoot string, components []nomadComponentDescriptor) error {
	targetSet := map[string]bool{}
	for _, component := range components {
		for _, target := range component.TestTargets {
			target = strings.TrimSpace(target)
			if target != "" {
				targetSet[target] = true
			}
		}
	}
	if len(targetSet) == 0 {
		return nil
	}
	targets := make([]string, 0, len(targetSet))
	for target := range targetSet {
		targets = append(targets, target)
	}
	sort.Strings(targets)
	args := append([]string{"test"}, targets...)
	cmd := exec.CommandContext(ctx, "bazelisk", args...)
	cmd.Dir = repoRoot
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	cmd.Stdout = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("bazelisk %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return nil
}
