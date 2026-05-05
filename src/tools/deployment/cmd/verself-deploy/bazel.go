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

const nomadComponentsQuery = `kind("nomad_component rule", //src/...)`

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

func buildNomadComponentDescriptors(ctx context.Context, repoRoot string) ([]string, []string, error) {
	labels, err := queryNomadComponentLabels(ctx, repoRoot)
	if err != nil {
		return nil, nil, err
	}
	build, err := bazelbuild.Build(ctx, repoRoot, labels)
	if err != nil {
		return nil, nil, err
	}
	descriptorPaths := make([]string, 0, len(labels))
	for _, label := range labels {
		outputs, err := build.Stream.ResolveOutputs(label, repoRoot)
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
