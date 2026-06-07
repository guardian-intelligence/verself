package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	defaultGuardianResourceGraph       = "/home/ubuntu/.local/state/guardian/repo/workspace/.guardian/fly/document.json"
	defaultDeploymentServiceResource   = "deployment-service"
	defaultDeploymentServiceListen     = "127.0.0.1:4294"
	defaultDeploymentSourceInitTimeout = 2 * time.Minute
)

type deploymentRunOptions struct {
	ResourceGraph string
	ResourceName  string
	ListenAddr    string
}

type deploymentMigrationOptions struct {
	ResourceGraph string
	ResourceName  string
}

type deploymentRuntimeConfig struct {
	Site                  string
	PublicBaseURL         string
	SourceRepoRoot        string
	SourceRepoURL         string
	SourceRepoInitTimeout time.Duration
	NomadAddress          string
	NomadJobPaths         []string
	ObjectStorageAddress  string
	PostgresDSN           string
	PostgresMaxConns      int
	SPIFFEEndpointSocket  string
	AdmissionConcurrency  int
	RecoverySSHReady      bool
	GitHubRepositories    string
	GitHubRefs            string
	GitHubWorkflowRefs    string
	GitHubOIDCConfigured  bool
}

func parseDeploymentRunOptions(args []string) (deploymentRunOptions, error) {
	fs := flag.NewFlagSet("deployment-service", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	opts := deploymentRunOptions{
		ResourceGraph: defaultGuardianResourceGraph,
		ResourceName:  defaultDeploymentServiceResource,
		ListenAddr:    defaultDeploymentServiceListen,
	}
	fs.StringVar(&opts.ResourceGraph, "resource-graph", opts.ResourceGraph, "Guardian resource graph JSON path")
	fs.StringVar(&opts.ResourceName, "resource-name", opts.ResourceName, "DeploymentService resource name")
	fs.StringVar(&opts.ListenAddr, "listen-addr", opts.ListenAddr, "public listener address")
	if err := fs.Parse(args); err != nil {
		return deploymentRunOptions{}, err
	}
	if len(fs.Args()) != 0 {
		return deploymentRunOptions{}, fmt.Errorf("unexpected args: %s", strings.Join(fs.Args(), " "))
	}
	return opts, nil
}

func parseDeploymentMigrationOptions(args []string) (deploymentMigrationOptions, []string, error) {
	fs := flag.NewFlagSet("deployment-service migrate", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	opts := deploymentMigrationOptions{
		ResourceGraph: defaultGuardianResourceGraph,
		ResourceName:  defaultDeploymentServiceResource,
	}
	fs.StringVar(&opts.ResourceGraph, "resource-graph", opts.ResourceGraph, "Guardian resource graph JSON path")
	fs.StringVar(&opts.ResourceName, "resource-name", opts.ResourceName, "DeploymentService resource name")
	if err := fs.Parse(args); err != nil {
		return deploymentMigrationOptions{}, nil, err
	}
	return opts, fs.Args(), nil
}

func loadDeploymentRuntimeConfig(path string, name string) (deploymentRuntimeConfig, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return deploymentRuntimeConfig{}, errors.New("resource graph path is required")
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return deploymentRuntimeConfig{}, errors.New("DeploymentService resource name is required")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return deploymentRuntimeConfig{}, fmt.Errorf("read Guardian resource graph: %w", err)
	}
	var doc deploymentGuardianDocument
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&doc); err != nil {
		return deploymentRuntimeConfig{}, fmt.Errorf("decode Guardian resource graph: %w", err)
	}
	var matches []deploymentGuardianResource
	for _, resource := range doc.Resources {
		if resource.APIVersion == "deployment.guardianintelligence.org/v1alpha1" &&
			resource.Kind == "DeploymentService" &&
			resource.Metadata.Name == name {
			matches = append(matches, resource)
		}
	}
	if len(matches) != 1 {
		return deploymentRuntimeConfig{}, fmt.Errorf("expected exactly one DeploymentService resource named %q, found %d", name, len(matches))
	}
	var spec deploymentServiceSpec
	if err := json.Unmarshal(matches[0].Spec, &spec); err != nil {
		return deploymentRuntimeConfig{}, fmt.Errorf("decode DeploymentService spec: %w", err)
	}
	return deploymentConfigFromSpec(spec)
}

type deploymentGuardianDocument struct {
	Entrypoint json.RawMessage              `json:"entrypoint"`
	Resources  []deploymentGuardianResource `json:"resources"`
}

type deploymentGuardianResource struct {
	APIVersion string          `json:"apiVersion"`
	Kind       string          `json:"kind"`
	Metadata   deploymentMeta  `json:"metadata"`
	Spec       json.RawMessage `json:"spec"`
}

type deploymentMeta struct {
	Name string `json:"name"`
}

type deploymentServiceSpec struct {
	Site          string `json:"site"`
	PublicBaseURL string `json:"publicBaseURL"`
	SourceRepo    struct {
		Root        string `json:"root"`
		URL         string `json:"url"`
		InitTimeout string `json:"initTimeout"`
	} `json:"sourceRepo"`
	Nomad struct {
		Address  string   `json:"address"`
		JobPaths []string `json:"jobPaths"`
	} `json:"nomad"`
	ObjectStorage struct {
		Address string `json:"address"`
	} `json:"objectStorage"`
	Postgres struct {
		DSN      string `json:"dsn"`
		MaxConns int    `json:"maxConns"`
	} `json:"postgres"`
	SPIFFE struct {
		EndpointSocket string `json:"endpointSocket"`
	} `json:"spiffe"`
	AdmissionConcurrency int  `json:"admissionConcurrency"`
	RecoverySSHReady     bool `json:"recoverySSHReady"`
	GitHubOIDC           *struct {
		AllowedRepositories string `json:"allowedRepositories"`
		AllowedRefs         string `json:"allowedRefs"`
		AllowedWorkflowRefs string `json:"allowedWorkflowRefs"`
	} `json:"githubOIDC"`
}

func deploymentConfigFromSpec(spec deploymentServiceSpec) (deploymentRuntimeConfig, error) {
	initTimeout := defaultDeploymentSourceInitTimeout
	if strings.TrimSpace(spec.SourceRepo.InitTimeout) != "" {
		parsed, err := time.ParseDuration(strings.TrimSpace(spec.SourceRepo.InitTimeout))
		if err != nil {
			return deploymentRuntimeConfig{}, fmt.Errorf("DeploymentService.spec.sourceRepo.initTimeout: %w", err)
		}
		initTimeout = parsed
	}
	cfg := deploymentRuntimeConfig{
		Site:                  strings.TrimSpace(spec.Site),
		PublicBaseURL:         strings.TrimSpace(spec.PublicBaseURL),
		SourceRepoRoot:        strings.TrimSpace(spec.SourceRepo.Root),
		SourceRepoURL:         strings.TrimSpace(spec.SourceRepo.URL),
		SourceRepoInitTimeout: initTimeout,
		NomadAddress:          strings.TrimSpace(spec.Nomad.Address),
		NomadJobPaths:         cleanConfigList(spec.Nomad.JobPaths),
		ObjectStorageAddress:  strings.TrimSpace(spec.ObjectStorage.Address),
		PostgresDSN:           strings.TrimSpace(spec.Postgres.DSN),
		PostgresMaxConns:      spec.Postgres.MaxConns,
		SPIFFEEndpointSocket:  strings.TrimSpace(spec.SPIFFE.EndpointSocket),
		AdmissionConcurrency:  spec.AdmissionConcurrency,
		RecoverySSHReady:      spec.RecoverySSHReady,
	}
	if spec.GitHubOIDC != nil {
		cfg.GitHubRepositories = strings.TrimSpace(spec.GitHubOIDC.AllowedRepositories)
		cfg.GitHubRefs = strings.TrimSpace(spec.GitHubOIDC.AllowedRefs)
		cfg.GitHubWorkflowRefs = strings.TrimSpace(spec.GitHubOIDC.AllowedWorkflowRefs)
		cfg.GitHubOIDCConfigured = cfg.GitHubRepositories != "" || cfg.GitHubRefs != "" || cfg.GitHubWorkflowRefs != ""
	}
	required := map[string]string{
		"site":                  cfg.Site,
		"publicBaseURL":         cfg.PublicBaseURL,
		"sourceRepo.root":       cfg.SourceRepoRoot,
		"sourceRepo.url":        cfg.SourceRepoURL,
		"nomad.address":         cfg.NomadAddress,
		"objectStorage.address": cfg.ObjectStorageAddress,
		"postgres.dsn":          cfg.PostgresDSN,
		"spiffe.endpointSocket": cfg.SPIFFEEndpointSocket,
	}
	for field, value := range required {
		if strings.TrimSpace(value) == "" {
			return deploymentRuntimeConfig{}, fmt.Errorf("DeploymentService.spec.%s is required", field)
		}
	}
	if cfg.PostgresMaxConns <= 0 {
		return deploymentRuntimeConfig{}, errors.New("DeploymentService.spec.postgres.maxConns must be positive")
	}
	if !filepath.IsAbs(cfg.SourceRepoRoot) {
		return deploymentRuntimeConfig{}, errors.New("DeploymentService.spec.sourceRepo.root must be absolute")
	}
	if len(cfg.NomadJobPaths) == 0 {
		return deploymentRuntimeConfig{}, errors.New("DeploymentService.spec.nomad.jobPaths must not be empty")
	}
	if cfg.AdmissionConcurrency <= 0 {
		return deploymentRuntimeConfig{}, errors.New("DeploymentService.spec.admissionConcurrency must be positive")
	}
	if cfg.GitHubOIDCConfigured && (cfg.GitHubRepositories == "" || cfg.GitHubRefs == "" || cfg.GitHubWorkflowRefs == "") {
		return deploymentRuntimeConfig{}, errors.New("DeploymentService.spec.githubOIDC requires allowedRepositories, allowedRefs, and allowedWorkflowRefs")
	}
	return cfg, nil
}

func cleanConfigList(values []string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

func boolString(value bool) string {
	if value {
		return "true"
	}
	return "false"
}
