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
	defaultGuardianResourceGraph           = "/home/ubuntu/.local/state/guardian/repo/workspace/.guardian/fly/document.json"
	defaultDistributionServiceResource     = "distribution-service"
	defaultDistributionServicePublicAddr   = "127.0.0.1:4280"
	defaultDistributionServiceInternalAddr = "127.0.0.1:4281"
	distributionServiceAPIVersion          = "distribution.guardianintelligence.org/v1alpha1"
	distributionServiceKind                = "DistributionService"
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

type distributionRuntimeConfig struct {
	InstallationID       string
	AuthIssuerURL        string
	AuthAudience         string
	PostgresDSN          string
	PostgresMaxConns     int
	ClickHouseAddress    string
	ClickHouseUser       string
	ClickHouseCACertPath string
	TrustedBuilders      []string
	TrustedSigners       []string
	SPIFFEEndpointSocket string
}

func parseRunOptions(args []string) (runOptions, error) {
	fs := flag.NewFlagSet(serviceName, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	opts := runOptions{
		ResourceGraph:      defaultGuardianResourceGraph,
		ResourceName:       defaultDistributionServiceResource,
		ListenAddr:         defaultDistributionServicePublicAddr,
		InternalListenAddr: defaultDistributionServiceInternalAddr,
	}
	fs.StringVar(&opts.ResourceGraph, "resource-graph", opts.ResourceGraph, "Guardian resource graph JSON path")
	fs.StringVar(&opts.ResourceName, "resource-name", opts.ResourceName, "DistributionService resource name")
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
	fs := flag.NewFlagSet(serviceName+" migrate", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	opts := migrationOptions{
		ResourceGraph: defaultGuardianResourceGraph,
		ResourceName:  defaultDistributionServiceResource,
	}
	fs.StringVar(&opts.ResourceGraph, "resource-graph", opts.ResourceGraph, "Guardian resource graph JSON path")
	fs.StringVar(&opts.ResourceName, "resource-name", opts.ResourceName, "DistributionService resource name")
	if err := fs.Parse(args); err != nil {
		return migrationOptions{}, nil, err
	}
	return opts, fs.Args(), nil
}

func loadDistributionRuntimeConfig(path string, name string) (distributionRuntimeConfig, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return distributionRuntimeConfig{}, errors.New("resource graph path is required")
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return distributionRuntimeConfig{}, errors.New("DistributionService resource name is required")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return distributionRuntimeConfig{}, fmt.Errorf("read Guardian resource graph: %w", err)
	}
	var doc distributionGuardianDocument
	if err := decodeStrictJSON(data, &doc); err != nil {
		return distributionRuntimeConfig{}, fmt.Errorf("decode Guardian resource graph: %w", err)
	}
	var matches []distributionGuardianResource
	for _, resource := range doc.Resources {
		if resource.APIVersion == distributionServiceAPIVersion &&
			resource.Kind == distributionServiceKind &&
			resource.Metadata.Name == name {
			matches = append(matches, resource)
		}
	}
	if len(matches) != 1 {
		return distributionRuntimeConfig{}, fmt.Errorf("expected exactly one DistributionService resource named %q, found %d", name, len(matches))
	}
	var spec distributionServiceSpec
	if err := decodeStrictJSON(matches[0].Spec, &spec); err != nil {
		return distributionRuntimeConfig{}, fmt.Errorf("decode DistributionService spec: %w", err)
	}
	return distributionConfigFromSpec(spec)
}

func decodeStrictJSON(data []byte, v any) error {
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return err
	}
	var extra struct{}
	if err := dec.Decode(&extra); err == nil {
		return errors.New("unexpected trailing JSON")
	} else if !errors.Is(err, io.EOF) {
		return err
	}
	return nil
}

type distributionGuardianDocument struct {
	Entrypoint json.RawMessage                `json:"entrypoint"`
	Resources  []distributionGuardianResource `json:"resources"`
}

type distributionGuardianResource struct {
	APIVersion string           `json:"apiVersion"`
	Kind       string           `json:"kind"`
	Metadata   distributionMeta `json:"metadata"`
	Spec       json.RawMessage  `json:"spec"`
}

type distributionMeta struct {
	Name string `json:"name"`
}

type distributionServiceSpec struct {
	InstallationID string `json:"installationID"`
	Auth           struct {
		IssuerURL string `json:"issuerURL"`
		Audience  string `json:"audience"`
	} `json:"auth"`
	Postgres struct {
		DSN      string `json:"dsn"`
		MaxConns int    `json:"maxConns"`
	} `json:"postgres"`
	ClickHouse struct {
		Address    string `json:"address"`
		User       string `json:"user"`
		CACertPath string `json:"caCertPath"`
	} `json:"clickhouse"`
	Attestation struct {
		TrustedBuilders []string `json:"trustedBuilders"`
		TrustedSigners  []string `json:"trustedSigners"`
	} `json:"attestation"`
	SPIFFE struct {
		EndpointSocket string `json:"endpointSocket"`
	} `json:"spiffe"`
}

func distributionConfigFromSpec(spec distributionServiceSpec) (distributionRuntimeConfig, error) {
	cfg := distributionRuntimeConfig{
		InstallationID:       strings.TrimSpace(spec.InstallationID),
		AuthIssuerURL:        strings.TrimSpace(spec.Auth.IssuerURL),
		AuthAudience:         strings.TrimSpace(spec.Auth.Audience),
		PostgresDSN:          strings.TrimSpace(spec.Postgres.DSN),
		PostgresMaxConns:     spec.Postgres.MaxConns,
		ClickHouseAddress:    strings.TrimSpace(spec.ClickHouse.Address),
		ClickHouseUser:       strings.TrimSpace(spec.ClickHouse.User),
		ClickHouseCACertPath: strings.TrimSpace(spec.ClickHouse.CACertPath),
		TrustedBuilders:      compactStrings(spec.Attestation.TrustedBuilders),
		TrustedSigners:       compactStrings(spec.Attestation.TrustedSigners),
		SPIFFEEndpointSocket: strings.TrimSpace(spec.SPIFFE.EndpointSocket),
	}
	required := map[string]string{
		"installationID":        cfg.InstallationID,
		"auth.issuerURL":        cfg.AuthIssuerURL,
		"auth.audience":         cfg.AuthAudience,
		"postgres.dsn":          cfg.PostgresDSN,
		"clickhouse.address":    cfg.ClickHouseAddress,
		"clickhouse.user":       cfg.ClickHouseUser,
		"clickhouse.caCertPath": cfg.ClickHouseCACertPath,
		"spiffe.endpointSocket": cfg.SPIFFEEndpointSocket,
	}
	var missing []string
	for label, value := range required {
		if strings.TrimSpace(value) == "" {
			missing = append(missing, label)
		}
	}
	if len(missing) > 0 {
		return distributionRuntimeConfig{}, fmt.Errorf("DistributionService missing required static config: %s", strings.Join(missing, ", "))
	}
	if cfg.PostgresMaxConns <= 0 {
		return distributionRuntimeConfig{}, errors.New("DistributionService.spec.postgres.maxConns must be positive")
	}
	if len(cfg.TrustedBuilders) == 0 {
		return distributionRuntimeConfig{}, errors.New("DistributionService.spec.attestation.trustedBuilders requires at least one entry")
	}
	if len(cfg.TrustedSigners) == 0 {
		return distributionRuntimeConfig{}, errors.New("DistributionService.spec.attestation.trustedSigners requires at least one entry")
	}
	return cfg, nil
}

func compactStrings(items []string) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item != "" {
			out = append(out, item)
		}
	}
	return out
}
