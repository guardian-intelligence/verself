package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"gopkg.in/yaml.v3"

	"github.com/verself/deployment-tools/internal/runtime"
	"github.com/verself/deployment-tools/internal/sshtun"
)

const (
	viteplusWorkspaceRel     = "src/viteplus-monorepo"
	topologyEndpointsYAMLRel = "src/host-configuration/ansible/group_vars/all/generated/endpoints.yml"
)

type topologyEndpointsYAML struct {
	TopologyEndpoints map[string]topologyComponent `yaml:"topology_endpoints"`
}

type topologyComponent struct {
	Endpoints map[string]topologyEndpoint `yaml:"endpoints"`
}

type topologyEndpoint struct {
	Port int `yaml:"port"`
}

func prepareViteplusWorkspace(ctx context.Context, rt *runtime.Runtime, repoRoot string) (func() error, error) {
	workspace := filepath.Join(repoRoot, viteplusWorkspaceRel)
	verdaccioPort, err := verdaccioRegistryPort(repoRoot)
	if err != nil {
		return nil, err
	}

	ctx, span := rt.Tracer.Start(ctx, "verself_deploy.viteplus.install",
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(
			attribute.String("viteplus.workspace", workspace),
			attribute.Int("verdaccio.remote_port", verdaccioPort),
		),
	)
	defer span.End()

	for _, input := range []struct {
		path string
		attr string
	}{
		{path: ".npmrc", attr: "viteplus.npmrc.sha256"},
		{path: "package.json", attr: "viteplus.package_json.sha256"},
		{path: "pnpm-lock.yaml", attr: "viteplus.pnpm_lock.sha256"},
		{path: "pnpm-workspace.yaml", attr: "viteplus.pnpm_workspace.sha256"},
	} {
		digest, err := fileSHA256(filepath.Join(workspace, input.path))
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			return nil, err
		}
		span.SetAttributes(attribute.String(input.attr, digest))
	}

	forward, registryURL, err := openVerdaccioRegistry(ctx, rt, verdaccioPort)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}
	span.SetAttributes(attribute.String("viteplus.registry_url", registryURL))
	closeForward := func() error { return nil }
	if forward != nil {
		closeForward = forward.Close
	}

	cmd := exec.CommandContext(ctx, "vp", "install", "--frozen-lockfile")
	cmd.Dir = workspace
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = append(os.Environ(),
		"VERSELF_NPM_REGISTRY="+registryURL,
		"NPM_CONFIG_REGISTRY="+registryURL,
		"npm_config_registry="+registryURL,
	)
	if err := cmd.Run(); err != nil {
		_ = closeForward()
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, fmt.Errorf("vp install --frozen-lockfile: %w", err)
	}

	span.SetStatus(codes.Ok, "")
	return closeForward, nil
}

func openVerdaccioRegistry(ctx context.Context, rt *runtime.Runtime, port int) (*sshtun.Forward, string, error) {
	registryURL := fmt.Sprintf("http://127.0.0.1:%d/", port)
	forward, err := rt.SSH.ForwardLocalPort(ctx, "verdaccio", port, port)
	if err == nil {
		return forward, registryURL, nil
	}
	return nil, "", fmt.Errorf("open Verdaccio registry forward: %w", err)
}

func verdaccioRegistryPort(repoRoot string) (int, error) {
	path := filepath.Join(repoRoot, topologyEndpointsYAMLRel)
	raw, err := os.ReadFile(path)
	if err != nil {
		return 0, fmt.Errorf("read topology endpoints: %w", err)
	}
	var cfg topologyEndpointsYAML
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		return 0, fmt.Errorf("parse topology endpoints: %w", err)
	}
	component, ok := cfg.TopologyEndpoints["verdaccio"]
	if !ok {
		return 0, fmt.Errorf("%s: missing topology_endpoints.verdaccio", topologyEndpointsYAMLRel)
	}
	endpoint, ok := component.Endpoints["http"]
	if !ok {
		return 0, fmt.Errorf("%s: missing topology_endpoints.verdaccio.endpoints.http", topologyEndpointsYAMLRel)
	}
	if endpoint.Port <= 0 {
		return 0, fmt.Errorf("%s: invalid Verdaccio http port %d", topologyEndpointsYAMLRel, endpoint.Port)
	}
	return endpoint.Port, nil
}

func fileSHA256(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", path, err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}
