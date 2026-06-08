package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
)

const (
	defaultGuardianResourceGraph       = "/home/ubuntu/.local/state/guardian/repo/workspace/.guardian/fly/document.json"
	defaultProjectsServiceResource     = "projects-service"
	defaultProjectsServicePublicAddr   = "127.0.0.1:4264"
	defaultProjectsServiceInternalAddr = "127.0.0.1:4265"
)

type runOptions struct {
	ResourceGraph      string
	ResourceName       string
	ListenAddr         string
	InternalListenAddr string
}

type migrationOptions struct {
	ResourceGraph string
	ResourceName  string
}

type projectsRuntimeConfig struct {
	InstallationID       string
	AuthIssuerURL        string
	AuthAudienceName     string
	PostgresDSN          string
	PostgresMaxConns     int
	SPIFFEEndpointSocket string
}

func parseRunOptions(args []string) (runOptions, error) {
	fs := flag.NewFlagSet("projects-service", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	opts := runOptions{
		ResourceGraph:      defaultGuardianResourceGraph,
		ResourceName:       defaultProjectsServiceResource,
		ListenAddr:         defaultProjectsServicePublicAddr,
		InternalListenAddr: defaultProjectsServiceInternalAddr,
	}
	fs.StringVar(&opts.ResourceGraph, "resource-graph", opts.ResourceGraph, "Guardian resource graph JSON path")
	fs.StringVar(&opts.ResourceName, "resource-name", opts.ResourceName, "ProjectsService resource name")
	fs.StringVar(&opts.ListenAddr, "listen-addr", opts.ListenAddr, "public listener address")
	fs.StringVar(&opts.InternalListenAddr, "internal-listen-addr", opts.InternalListenAddr, "internal mTLS listener address")
	if err := fs.Parse(args); err != nil {
		return runOptions{}, err
	}
	if len(fs.Args()) != 0 {
		return runOptions{}, fmt.Errorf("unexpected args: %s", strings.Join(fs.Args(), " "))
	}
	return opts, nil
}

func parseMigrationOptions(args []string) (migrationOptions, []string, error) {
	fs := flag.NewFlagSet("projects-service migrate", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	opts := migrationOptions{
		ResourceGraph: defaultGuardianResourceGraph,
		ResourceName:  defaultProjectsServiceResource,
	}
	fs.StringVar(&opts.ResourceGraph, "resource-graph", opts.ResourceGraph, "Guardian resource graph JSON path")
	fs.StringVar(&opts.ResourceName, "resource-name", opts.ResourceName, "ProjectsService resource name")
	if err := fs.Parse(args); err != nil {
		return migrationOptions{}, nil, err
	}
	return opts, fs.Args(), nil
}

func loadProjectsRuntimeConfig(path string, name string) (projectsRuntimeConfig, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return projectsRuntimeConfig{}, errors.New("resource graph path is required")
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return projectsRuntimeConfig{}, errors.New("ProjectsService resource name is required")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return projectsRuntimeConfig{}, fmt.Errorf("read Guardian resource graph: %w", err)
	}
	var doc projectsGuardianDocument
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&doc); err != nil {
		return projectsRuntimeConfig{}, fmt.Errorf("decode Guardian resource graph: %w", err)
	}
	var matches []projectsGuardianResource
	for _, resource := range doc.Resources {
		if resource.APIVersion == "projects.guardianintelligence.org/v1alpha1" &&
			resource.Kind == "ProjectsService" &&
			resource.Metadata.Name == name {
			matches = append(matches, resource)
		}
	}
	if len(matches) != 1 {
		return projectsRuntimeConfig{}, fmt.Errorf("expected exactly one ProjectsService resource named %q, found %d", name, len(matches))
	}
	var spec projectsServiceSpec
	if err := json.Unmarshal(matches[0].Spec, &spec); err != nil {
		return projectsRuntimeConfig{}, fmt.Errorf("decode ProjectsService spec: %w", err)
	}
	return projectsConfigFromSpec(spec)
}

type projectsGuardianDocument struct {
	Entrypoint json.RawMessage            `json:"entrypoint"`
	Resources  []projectsGuardianResource `json:"resources"`
}

type projectsGuardianResource struct {
	APIVersion string          `json:"apiVersion"`
	Kind       string          `json:"kind"`
	Metadata   projectsMeta    `json:"metadata"`
	Spec       json.RawMessage `json:"spec"`
}

type projectsMeta struct {
	Name string `json:"name"`
}

type objectRef struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	Name       string `json:"name"`
}

type projectsServiceSpec struct {
	InstallationID string `json:"installationID"`
	Auth           struct {
		IssuerURL   string    `json:"issuerURL"`
		AudienceRef objectRef `json:"audienceRef"`
	} `json:"auth"`
	Postgres struct {
		DSN      string `json:"dsn"`
		MaxConns int    `json:"maxConns"`
	} `json:"postgres"`
	SPIFFE struct {
		EndpointSocket string `json:"endpointSocket"`
	} `json:"spiffe"`
}

func projectsConfigFromSpec(spec projectsServiceSpec) (projectsRuntimeConfig, error) {
	cfg := projectsRuntimeConfig{
		InstallationID:       strings.TrimSpace(spec.InstallationID),
		AuthIssuerURL:        strings.TrimSpace(spec.Auth.IssuerURL),
		AuthAudienceName:     strings.TrimSpace(spec.Auth.AudienceRef.Name),
		PostgresDSN:          strings.TrimSpace(spec.Postgres.DSN),
		PostgresMaxConns:     spec.Postgres.MaxConns,
		SPIFFEEndpointSocket: strings.TrimSpace(spec.SPIFFE.EndpointSocket),
	}
	required := map[string]string{
		"installationID":        cfg.InstallationID,
		"auth.issuerURL":        cfg.AuthIssuerURL,
		"auth.audienceRef.name": cfg.AuthAudienceName,
		"postgres.dsn":          cfg.PostgresDSN,
		"spiffe.endpointSocket": cfg.SPIFFEEndpointSocket,
	}
	var missing []string
	for label, value := range required {
		if strings.TrimSpace(value) == "" {
			missing = append(missing, label)
		}
	}
	if len(missing) > 0 {
		return projectsRuntimeConfig{}, fmt.Errorf("ProjectsService missing required static config: %s", strings.Join(missing, ", "))
	}
	if cfg.PostgresMaxConns <= 0 {
		return projectsRuntimeConfig{}, errors.New("ProjectsService.spec.postgres.maxConns must be positive")
	}
	return cfg, nil
}
