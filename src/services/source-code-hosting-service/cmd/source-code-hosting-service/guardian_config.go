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
	defaultGuardianResourceGraph        = "/home/ubuntu/.local/state/guardian/repo/workspace/.guardian/fly/document.json"
	defaultSourceCodeHostingResource    = "source-code-hosting-service"
	defaultSourceCodeHostingPublicAddr  = "127.0.0.1:4261"
	defaultSourceCodeHostingPrivateAddr = "127.0.0.1:4262"
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

type sourceRuntimeConfig struct {
	InstallationID       string
	PublicBaseURL        string
	AuthIssuerURL        string
	AuthAudienceName     string
	ForgejoBaseURL       string
	ForgejoOwner         string
	ForgejoTokenName     string
	WebhookSecretName    string
	PostgresDSN          string
	PostgresMaxConns     int
	SPIFFEEndpointSocket string
}

func parseRunOptions(args []string) (runOptions, error) {
	fs := flag.NewFlagSet("source-code-hosting-service", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	opts := runOptions{
		ResourceGraph:      defaultGuardianResourceGraph,
		ResourceName:       defaultSourceCodeHostingResource,
		ListenAddr:         defaultSourceCodeHostingPublicAddr,
		InternalListenAddr: defaultSourceCodeHostingPrivateAddr,
	}
	fs.StringVar(&opts.ResourceGraph, "resource-graph", opts.ResourceGraph, "Guardian resource graph JSON path")
	fs.StringVar(&opts.ResourceName, "resource-name", opts.ResourceName, "SourceCodeHostingService resource name")
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
	fs := flag.NewFlagSet("source-code-hosting-service migrate", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	opts := migrationOptions{
		ResourceGraph: defaultGuardianResourceGraph,
		ResourceName:  defaultSourceCodeHostingResource,
	}
	fs.StringVar(&opts.ResourceGraph, "resource-graph", opts.ResourceGraph, "Guardian resource graph JSON path")
	fs.StringVar(&opts.ResourceName, "resource-name", opts.ResourceName, "SourceCodeHostingService resource name")
	if err := fs.Parse(args); err != nil {
		return migrationOptions{}, nil, err
	}
	return opts, fs.Args(), nil
}

func loadSourceRuntimeConfig(path string, name string) (sourceRuntimeConfig, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return sourceRuntimeConfig{}, errors.New("resource graph path is required")
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return sourceRuntimeConfig{}, errors.New("SourceCodeHostingService resource name is required")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return sourceRuntimeConfig{}, fmt.Errorf("read Guardian resource graph: %w", err)
	}
	var doc sourceGuardianDocument
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&doc); err != nil {
		return sourceRuntimeConfig{}, fmt.Errorf("decode Guardian resource graph: %w", err)
	}
	var matches []sourceGuardianResource
	for _, resource := range doc.Resources {
		if resource.APIVersion == "source.guardianintelligence.org/v1alpha1" &&
			resource.Kind == "SourceCodeHostingService" &&
			resource.Metadata.Name == name {
			matches = append(matches, resource)
		}
	}
	if len(matches) != 1 {
		return sourceRuntimeConfig{}, fmt.Errorf("expected exactly one SourceCodeHostingService resource named %q, found %d", name, len(matches))
	}
	var spec sourceCodeHostingServiceSpec
	if err := json.Unmarshal(matches[0].Spec, &spec); err != nil {
		return sourceRuntimeConfig{}, fmt.Errorf("decode SourceCodeHostingService spec: %w", err)
	}
	return sourceConfigFromSpec(spec)
}

type sourceGuardianDocument struct {
	Entrypoint json.RawMessage          `json:"entrypoint"`
	Resources  []sourceGuardianResource `json:"resources"`
}

type sourceGuardianResource struct {
	APIVersion string          `json:"apiVersion"`
	Kind       string          `json:"kind"`
	Metadata   sourceMeta      `json:"metadata"`
	Spec       json.RawMessage `json:"spec"`
}

type sourceMeta struct {
	Name string `json:"name"`
}

type objectRef struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	Name       string `json:"name"`
}

type sourceCodeHostingServiceSpec struct {
	InstallationID string `json:"installationID"`
	PublicBaseURL  string `json:"publicBaseURL"`
	Auth           struct {
		IssuerURL   string    `json:"issuerURL"`
		AudienceRef objectRef `json:"audienceRef"`
	} `json:"auth"`
	Forgejo struct {
		BaseURL            string    `json:"baseURL"`
		Owner              string    `json:"owner"`
		AutomationTokenRef objectRef `json:"automationTokenRef"`
		WebhookSecretRef   objectRef `json:"webhookSecretRef"`
	} `json:"forgejo"`
	Postgres struct {
		DSN      string `json:"dsn"`
		MaxConns int    `json:"maxConns"`
	} `json:"postgres"`
	SPIFFE struct {
		EndpointSocket string `json:"endpointSocket"`
	} `json:"spiffe"`
}

func sourceConfigFromSpec(spec sourceCodeHostingServiceSpec) (sourceRuntimeConfig, error) {
	cfg := sourceRuntimeConfig{
		InstallationID:       strings.TrimSpace(spec.InstallationID),
		PublicBaseURL:        strings.TrimSpace(spec.PublicBaseURL),
		AuthIssuerURL:        strings.TrimSpace(spec.Auth.IssuerURL),
		AuthAudienceName:     strings.TrimSpace(spec.Auth.AudienceRef.Name),
		ForgejoBaseURL:       strings.TrimSpace(spec.Forgejo.BaseURL),
		ForgejoOwner:         strings.TrimSpace(spec.Forgejo.Owner),
		ForgejoTokenName:     strings.TrimSpace(spec.Forgejo.AutomationTokenRef.Name),
		WebhookSecretName:    strings.TrimSpace(spec.Forgejo.WebhookSecretRef.Name),
		PostgresDSN:          strings.TrimSpace(spec.Postgres.DSN),
		PostgresMaxConns:     spec.Postgres.MaxConns,
		SPIFFEEndpointSocket: strings.TrimSpace(spec.SPIFFE.EndpointSocket),
	}
	required := map[string]string{
		"installationID":                  cfg.InstallationID,
		"publicBaseURL":                   cfg.PublicBaseURL,
		"auth.issuerURL":                  cfg.AuthIssuerURL,
		"auth.audienceRef.name":           cfg.AuthAudienceName,
		"forgejo.baseURL":                 cfg.ForgejoBaseURL,
		"forgejo.owner":                   cfg.ForgejoOwner,
		"forgejo.automationTokenRef.name": cfg.ForgejoTokenName,
		"forgejo.webhookSecretRef.name":   cfg.WebhookSecretName,
		"postgres.dsn":                    cfg.PostgresDSN,
		"spiffe.endpointSocket":           cfg.SPIFFEEndpointSocket,
	}
	var missing []string
	for label, value := range required {
		if strings.TrimSpace(value) == "" {
			missing = append(missing, label)
		}
	}
	if len(missing) > 0 {
		return sourceRuntimeConfig{}, fmt.Errorf("SourceCodeHostingService missing required static config: %s", strings.Join(missing, ", "))
	}
	if cfg.PostgresMaxConns <= 0 {
		return sourceRuntimeConfig{}, errors.New("SourceCodeHostingService.spec.postgres.maxConns must be positive")
	}
	return cfg, nil
}
