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
	defaultGuardianResourceGraph        = "/home/ubuntu/.local/state/guardian/repo/current/workspace/.guardian/fly/document.json"
	defaultGovernanceServiceResource    = "governance-service"
	defaultGovernanceServicePublicAddr  = "127.0.0.1:4250"
	defaultGovernanceServicePrivateAddr = "127.0.0.1:4254"
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

type governanceRuntimeConfig struct {
	InstallationID          string
	Environment             string
	PublicBaseURL           string
	WriterInstanceID        string
	AuthIssuerURL           string
	AuthAudienceName        string
	APIActivityHMACKeyID    string
	APIActivityHMACKeyName  string
	PostgresDSN             string
	PostgresMaxConns        int
	IdentityPostgresDSN     string
	IdentityPostgresMaxConn int
	BillingPostgresDSN      string
	BillingPostgresMaxConns int
	SandboxPostgresDSN      string
	SandboxPostgresMaxConns int
	ClickHouseAddress       string
	ClickHouseUser          string
	ClickHouseCACertPath    string
	ExportDir               string
	ExportTTLHours          int
	SPIFFEEndpointSocket    string
}

func parseRunOptions(args []string) (runOptions, error) {
	fs := flag.NewFlagSet("governance-service", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	opts := runOptions{
		ResourceGraph:      defaultGuardianResourceGraph,
		ResourceName:       defaultGovernanceServiceResource,
		ListenAddr:         defaultGovernanceServicePublicAddr,
		InternalListenAddr: defaultGovernanceServicePrivateAddr,
	}
	fs.StringVar(&opts.ResourceGraph, "resource-graph", opts.ResourceGraph, "Guardian resource graph JSON path")
	fs.StringVar(&opts.ResourceName, "resource-name", opts.ResourceName, "GovernanceService resource name")
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
	fs := flag.NewFlagSet("governance-service migrate", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	opts := migrationOptions{
		ResourceGraph: defaultGuardianResourceGraph,
		ResourceName:  defaultGovernanceServiceResource,
	}
	fs.StringVar(&opts.ResourceGraph, "resource-graph", opts.ResourceGraph, "Guardian resource graph JSON path")
	fs.StringVar(&opts.ResourceName, "resource-name", opts.ResourceName, "GovernanceService resource name")
	if err := fs.Parse(args); err != nil {
		return migrationOptions{}, nil, err
	}
	return opts, fs.Args(), nil
}

func loadGovernanceRuntimeConfig(path string, name string) (governanceRuntimeConfig, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return governanceRuntimeConfig{}, errors.New("resource graph path is required")
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return governanceRuntimeConfig{}, errors.New("GovernanceService resource name is required")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return governanceRuntimeConfig{}, fmt.Errorf("read Guardian resource graph: %w", err)
	}
	var doc governanceGuardianDocument
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&doc); err != nil {
		return governanceRuntimeConfig{}, fmt.Errorf("decode Guardian resource graph: %w", err)
	}
	var matches []governanceGuardianResource
	for _, resource := range doc.Resources {
		if resource.APIVersion == "governance.guardianintelligence.org/v1alpha1" &&
			resource.Kind == "GovernanceService" &&
			resource.Metadata.Name == name {
			matches = append(matches, resource)
		}
	}
	if len(matches) != 1 {
		return governanceRuntimeConfig{}, fmt.Errorf("expected exactly one GovernanceService resource named %q, found %d", name, len(matches))
	}
	var spec governanceServiceSpec
	if err := json.Unmarshal(matches[0].Spec, &spec); err != nil {
		return governanceRuntimeConfig{}, fmt.Errorf("decode GovernanceService spec: %w", err)
	}
	return governanceConfigFromSpec(spec)
}

type governanceGuardianDocument struct {
	Entrypoint json.RawMessage              `json:"entrypoint"`
	Resources  []governanceGuardianResource `json:"resources"`
}

type governanceGuardianResource struct {
	APIVersion string          `json:"apiVersion"`
	Kind       string          `json:"kind"`
	Metadata   governanceMeta  `json:"metadata"`
	Spec       json.RawMessage `json:"spec"`
}

type governanceMeta struct {
	Name string `json:"name"`
}

type objectRef struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	Name       string `json:"name"`
}

type governanceServiceSpec struct {
	InstallationID string `json:"installationID"`
	Environment    string `json:"environment"`
	PublicBaseURL  string `json:"publicBaseURL"`
	Writer         struct {
		InstanceID string `json:"instanceID"`
	} `json:"writer"`
	Auth struct {
		IssuerURL   string    `json:"issuerURL"`
		AudienceRef objectRef `json:"audienceRef"`
	} `json:"auth"`
	APIActivity struct {
		HMACKeyID  string    `json:"hmacKeyID"`
		HMACKeyRef objectRef `json:"hmacKeyRef"`
	} `json:"apiActivity"`
	Postgres struct {
		DSN      string `json:"dsn"`
		MaxConns int    `json:"maxConns"`
		Identity struct {
			DSN      string `json:"dsn"`
			MaxConns int    `json:"maxConns"`
		} `json:"identity"`
		Billing struct {
			DSN      string `json:"dsn"`
			MaxConns int    `json:"maxConns"`
		} `json:"billing"`
		Sandbox struct {
			DSN      string `json:"dsn"`
			MaxConns int    `json:"maxConns"`
		} `json:"sandbox"`
	} `json:"postgres"`
	ClickHouse struct {
		Address    string `json:"address"`
		User       string `json:"user"`
		CACertPath string `json:"caCertPath"`
	} `json:"clickhouse"`
	Exports struct {
		Dir      string `json:"dir"`
		TTLHours int    `json:"ttlHours"`
	} `json:"exports"`
	SPIFFE struct {
		EndpointSocket string `json:"endpointSocket"`
	} `json:"spiffe"`
}

func governanceConfigFromSpec(spec governanceServiceSpec) (governanceRuntimeConfig, error) {
	cfg := governanceRuntimeConfig{
		InstallationID:          strings.TrimSpace(spec.InstallationID),
		Environment:             strings.TrimSpace(spec.Environment),
		PublicBaseURL:           strings.TrimSpace(spec.PublicBaseURL),
		WriterInstanceID:        strings.TrimSpace(spec.Writer.InstanceID),
		AuthIssuerURL:           strings.TrimSpace(spec.Auth.IssuerURL),
		AuthAudienceName:        strings.TrimSpace(spec.Auth.AudienceRef.Name),
		APIActivityHMACKeyID:    strings.TrimSpace(spec.APIActivity.HMACKeyID),
		APIActivityHMACKeyName:  strings.TrimSpace(spec.APIActivity.HMACKeyRef.Name),
		PostgresDSN:             strings.TrimSpace(spec.Postgres.DSN),
		PostgresMaxConns:        spec.Postgres.MaxConns,
		IdentityPostgresDSN:     strings.TrimSpace(spec.Postgres.Identity.DSN),
		IdentityPostgresMaxConn: spec.Postgres.Identity.MaxConns,
		BillingPostgresDSN:      strings.TrimSpace(spec.Postgres.Billing.DSN),
		BillingPostgresMaxConns: spec.Postgres.Billing.MaxConns,
		SandboxPostgresDSN:      strings.TrimSpace(spec.Postgres.Sandbox.DSN),
		SandboxPostgresMaxConns: spec.Postgres.Sandbox.MaxConns,
		ClickHouseAddress:       strings.TrimSpace(spec.ClickHouse.Address),
		ClickHouseUser:          strings.TrimSpace(spec.ClickHouse.User),
		ClickHouseCACertPath:    strings.TrimSpace(spec.ClickHouse.CACertPath),
		ExportDir:               strings.TrimSpace(spec.Exports.Dir),
		ExportTTLHours:          spec.Exports.TTLHours,
		SPIFFEEndpointSocket:    strings.TrimSpace(spec.SPIFFE.EndpointSocket),
	}
	required := map[string]string{
		"installationID":              cfg.InstallationID,
		"environment":                 cfg.Environment,
		"publicBaseURL":               cfg.PublicBaseURL,
		"writer.instanceID":           cfg.WriterInstanceID,
		"auth.issuerURL":              cfg.AuthIssuerURL,
		"auth.audienceRef.name":       cfg.AuthAudienceName,
		"apiActivity.hmacKeyID":       cfg.APIActivityHMACKeyID,
		"apiActivity.hmacKeyRef.name": cfg.APIActivityHMACKeyName,
		"postgres.dsn":                cfg.PostgresDSN,
		"postgres.identity.dsn":       cfg.IdentityPostgresDSN,
		"postgres.billing.dsn":        cfg.BillingPostgresDSN,
		"postgres.sandbox.dsn":        cfg.SandboxPostgresDSN,
		"clickhouse.address":          cfg.ClickHouseAddress,
		"clickhouse.user":             cfg.ClickHouseUser,
		"clickhouse.caCertPath":       cfg.ClickHouseCACertPath,
		"exports.dir":                 cfg.ExportDir,
		"spiffe.endpointSocket":       cfg.SPIFFEEndpointSocket,
	}
	var missing []string
	for label, value := range required {
		if strings.TrimSpace(value) == "" {
			missing = append(missing, label)
		}
	}
	if len(missing) > 0 {
		return governanceRuntimeConfig{}, fmt.Errorf("GovernanceService missing required static config: %s", strings.Join(missing, ", "))
	}
	if cfg.PostgresMaxConns <= 0 || cfg.IdentityPostgresMaxConn <= 0 || cfg.BillingPostgresMaxConns <= 0 || cfg.SandboxPostgresMaxConns <= 0 {
		return governanceRuntimeConfig{}, errors.New("GovernanceService postgres maxConns values must be positive")
	}
	if cfg.ExportTTLHours <= 0 {
		return governanceRuntimeConfig{}, errors.New("GovernanceService.spec.exports.ttlHours must be positive")
	}
	return cfg, nil
}
