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
	defaultGuardianResourceGraph      = "/home/ubuntu/.local/state/guardian/repo/current/workspace/.guardian/fly/document.json"
	defaultProfileServiceResource     = "profile-service"
	defaultProfileServicePublicAddr   = "127.0.0.1:4258"
	defaultProfileServiceInternalAddr = "127.0.0.1:4259"
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

type profileRuntimeConfig struct {
	AuthIssuerURL        string
	AuthAudienceName     string
	PostgresDSN          string
	PostgresMaxConns     int
	SPIFFEEndpointSocket string
}

func parseRunOptions(args []string) (runOptions, error) {
	fs := flag.NewFlagSet("profile-service", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	opts := runOptions{
		ResourceGraph:      defaultGuardianResourceGraph,
		ResourceName:       defaultProfileServiceResource,
		ListenAddr:         defaultProfileServicePublicAddr,
		InternalListenAddr: defaultProfileServiceInternalAddr,
	}
	fs.StringVar(&opts.ResourceGraph, "resource-graph", opts.ResourceGraph, "Guardian resource graph JSON path")
	fs.StringVar(&opts.ResourceName, "resource-name", opts.ResourceName, "ProfileService resource name")
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
	fs := flag.NewFlagSet("profile-service migrate", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	opts := migrationOptions{
		ResourceGraph: defaultGuardianResourceGraph,
		ResourceName:  defaultProfileServiceResource,
	}
	fs.StringVar(&opts.ResourceGraph, "resource-graph", opts.ResourceGraph, "Guardian resource graph JSON path")
	fs.StringVar(&opts.ResourceName, "resource-name", opts.ResourceName, "ProfileService resource name")
	if err := fs.Parse(args); err != nil {
		return migrationOptions{}, nil, err
	}
	return opts, fs.Args(), nil
}

func loadProfileRuntimeConfig(path string, name string) (profileRuntimeConfig, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return profileRuntimeConfig{}, errors.New("resource graph path is required")
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return profileRuntimeConfig{}, errors.New("ProfileService resource name is required")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return profileRuntimeConfig{}, fmt.Errorf("read Guardian resource graph: %w", err)
	}
	var doc profileGuardianDocument
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&doc); err != nil {
		return profileRuntimeConfig{}, fmt.Errorf("decode Guardian resource graph: %w", err)
	}
	var matches []profileGuardianResource
	for _, resource := range doc.Resources {
		if resource.APIVersion == "profile.guardianintelligence.org/v1alpha1" &&
			resource.Kind == "ProfileService" &&
			resource.Metadata.Name == name {
			matches = append(matches, resource)
		}
	}
	if len(matches) != 1 {
		return profileRuntimeConfig{}, fmt.Errorf("expected exactly one ProfileService resource named %q, found %d", name, len(matches))
	}
	var spec profileServiceSpec
	if err := json.Unmarshal(matches[0].Spec, &spec); err != nil {
		return profileRuntimeConfig{}, fmt.Errorf("decode ProfileService spec: %w", err)
	}
	return profileConfigFromSpec(spec)
}

type profileGuardianDocument struct {
	Entrypoint json.RawMessage           `json:"entrypoint"`
	Resources  []profileGuardianResource `json:"resources"`
}

type profileGuardianResource struct {
	APIVersion string          `json:"apiVersion"`
	Kind       string          `json:"kind"`
	Metadata   profileMeta     `json:"metadata"`
	Spec       json.RawMessage `json:"spec"`
}

type profileMeta struct {
	Name string `json:"name"`
}

type objectRef struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	Name       string `json:"name"`
}

type profileServiceSpec struct {
	Auth struct {
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

func profileConfigFromSpec(spec profileServiceSpec) (profileRuntimeConfig, error) {
	cfg := profileRuntimeConfig{
		AuthIssuerURL:        strings.TrimSpace(spec.Auth.IssuerURL),
		AuthAudienceName:     strings.TrimSpace(spec.Auth.AudienceRef.Name),
		PostgresDSN:          strings.TrimSpace(spec.Postgres.DSN),
		PostgresMaxConns:     spec.Postgres.MaxConns,
		SPIFFEEndpointSocket: strings.TrimSpace(spec.SPIFFE.EndpointSocket),
	}
	required := map[string]string{
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
		return profileRuntimeConfig{}, fmt.Errorf("ProfileService missing required static config: %s", strings.Join(missing, ", "))
	}
	if cfg.PostgresMaxConns <= 0 {
		return profileRuntimeConfig{}, errors.New("ProfileService.spec.postgres.maxConns must be positive")
	}
	return cfg, nil
}
