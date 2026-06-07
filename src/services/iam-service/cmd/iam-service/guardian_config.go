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
	defaultGuardianResourceGraph = "/home/ubuntu/.local/state/guardian/repo/workspace/.guardian/fly/document.json"
	defaultIAMResourceName       = "iam-service"
)

type iamRunOptions struct {
	ResourceGraph      string
	ResourceName       string
	ListenAddr         string
	InternalListenAddr string
}

type iamMigrationOptions struct {
	ResourceGraph string
	ResourceName  string
}

type iamRuntimeConfig struct {
	InstallationID              string
	AuthIssuerURL               string
	BrowserAuthPublicBaseURL    string
	PwnedPasswordsRangeEndpoint string
	ZitadelHost                 string
	PostgresDSN                 string
	PostgresMaxConns            int
	ClickHouseAddress           string
	ClickHouseUser              string
	ClickHouseCACertPath        string
	SPIFFEEndpointSocket        string
	ZitadelActionSigningKeyName string
	BrowserOIDCClientIDName     string
	BrowserOIDCClientSecretName string
	GithubLoginIDPName          string
	AuthAudienceName            string
	ZitadelAdminTokenName       string
	SpiceDBPresharedKeyName     string
	EmailIdentityHMACKeyName    string
}

func parseIAMRunOptions(args []string) (iamRunOptions, error) {
	fs := flag.NewFlagSet("iam-service", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	opts := iamRunOptions{
		ResourceGraph:      defaultGuardianResourceGraph,
		ResourceName:       defaultIAMResourceName,
		ListenAddr:         "127.0.0.1:4248",
		InternalListenAddr: "127.0.0.1:4241",
	}
	fs.StringVar(&opts.ResourceGraph, "resource-graph", opts.ResourceGraph, "Guardian resource graph JSON path")
	fs.StringVar(&opts.ResourceName, "resource-name", opts.ResourceName, "IAMService resource name")
	fs.StringVar(&opts.ListenAddr, "listen-addr", opts.ListenAddr, "public listener address")
	fs.StringVar(&opts.InternalListenAddr, "internal-listen-addr", opts.InternalListenAddr, "internal listener address")
	if err := fs.Parse(args); err != nil {
		return iamRunOptions{}, err
	}
	if len(fs.Args()) != 0 {
		return iamRunOptions{}, fmt.Errorf("unexpected args: %s", strings.Join(fs.Args(), " "))
	}
	return opts, nil
}

func parseIAMMigrationOptions(args []string) (iamMigrationOptions, []string, error) {
	fs := flag.NewFlagSet("iam-service migrate", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	opts := iamMigrationOptions{
		ResourceGraph: defaultGuardianResourceGraph,
		ResourceName:  defaultIAMResourceName,
	}
	fs.StringVar(&opts.ResourceGraph, "resource-graph", opts.ResourceGraph, "Guardian resource graph JSON path")
	fs.StringVar(&opts.ResourceName, "resource-name", opts.ResourceName, "IAMService resource name")
	if err := fs.Parse(args); err != nil {
		return iamMigrationOptions{}, nil, err
	}
	return opts, fs.Args(), nil
}

func loadIAMRuntimeConfig(path string, name string) (iamRuntimeConfig, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return iamRuntimeConfig{}, errors.New("resource graph path is required")
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return iamRuntimeConfig{}, errors.New("IAMService resource name is required")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return iamRuntimeConfig{}, fmt.Errorf("read Guardian resource graph: %w", err)
	}
	var doc guardianDocument
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&doc); err != nil {
		return iamRuntimeConfig{}, fmt.Errorf("decode Guardian resource graph: %w", err)
	}
	var matches []guardianResource
	for _, resource := range doc.Resources {
		if resource.APIVersion == "iam.guardianintelligence.org/v1alpha1" &&
			resource.Kind == "IAMService" &&
			resource.Metadata.Name == name {
			matches = append(matches, resource)
		}
	}
	if len(matches) != 1 {
		return iamRuntimeConfig{}, fmt.Errorf("expected exactly one IAMService resource named %q, found %d", name, len(matches))
	}
	var spec iamServiceSpec
	if err := json.Unmarshal(matches[0].Spec, &spec); err != nil {
		return iamRuntimeConfig{}, fmt.Errorf("decode IAMService spec: %w", err)
	}
	return iamConfigFromSpec(spec)
}

type guardianDocument struct {
	Entrypoint json.RawMessage    `json:"entrypoint"`
	Resources  []guardianResource `json:"resources"`
}

type guardianResource struct {
	APIVersion string          `json:"apiVersion"`
	Kind       string          `json:"kind"`
	Metadata   resourceMeta    `json:"metadata"`
	Spec       json.RawMessage `json:"spec"`
}

type resourceMeta struct {
	Name string `json:"name"`
}

type objectRef struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	Name       string `json:"name"`
}

type iamServiceSpec struct {
	InstallationID string `json:"installationID"`
	Auth           struct {
		IssuerURL                   string `json:"issuerURL"`
		BrowserAuthPublicBaseURL    string `json:"browserAuthPublicBaseURL"`
		PwnedPasswordsRangeEndpoint string `json:"pwnedPasswordsRangeEndpoint"`
	} `json:"auth"`
	Zitadel struct {
		Host string `json:"host"`
	} `json:"zitadel"`
	Postgres struct {
		DSN      string `json:"dsn"`
		MaxConns int    `json:"maxConns"`
	} `json:"postgres"`
	ClickHouse struct {
		Address    string `json:"address"`
		User       string `json:"user"`
		CACertPath string `json:"caCertPath"`
	} `json:"clickhouse"`
	SPIFFE struct {
		EndpointSocket string `json:"endpointSocket"`
	} `json:"spiffe"`
	Credentials struct {
		ZitadelActionSigningKeyRef objectRef `json:"zitadelActionSigningKeyRef"`
		BrowserOIDCClientIDRef     objectRef `json:"browserOIDCClientIDRef"`
		BrowserOIDCClientSecretRef objectRef `json:"browserOIDCClientSecretRef"`
		GithubLoginIDPRef          objectRef `json:"githubLoginIDPRef"`
		AuthAudienceRef            objectRef `json:"authAudienceRef"`
		ZitadelAdminTokenRef       objectRef `json:"zitadelAdminTokenRef"`
		SpiceDBPresharedKeyRef     objectRef `json:"spiceDBPresharedKeyRef"`
		EmailIdentityHMACKeyRef    objectRef `json:"emailIdentityHMACKeyRef"`
	} `json:"credentials"`
}

func iamConfigFromSpec(spec iamServiceSpec) (iamRuntimeConfig, error) {
	cfg := iamRuntimeConfig{
		InstallationID:              strings.TrimSpace(spec.InstallationID),
		AuthIssuerURL:               strings.TrimSpace(spec.Auth.IssuerURL),
		BrowserAuthPublicBaseURL:    strings.TrimSpace(spec.Auth.BrowserAuthPublicBaseURL),
		PwnedPasswordsRangeEndpoint: strings.TrimSpace(spec.Auth.PwnedPasswordsRangeEndpoint),
		ZitadelHost:                 strings.TrimSpace(spec.Zitadel.Host),
		PostgresDSN:                 strings.TrimSpace(spec.Postgres.DSN),
		PostgresMaxConns:            spec.Postgres.MaxConns,
		ClickHouseAddress:           strings.TrimSpace(spec.ClickHouse.Address),
		ClickHouseUser:              strings.TrimSpace(spec.ClickHouse.User),
		ClickHouseCACertPath:        strings.TrimSpace(spec.ClickHouse.CACertPath),
		SPIFFEEndpointSocket:        strings.TrimSpace(spec.SPIFFE.EndpointSocket),
		ZitadelActionSigningKeyName: refName(spec.Credentials.ZitadelActionSigningKeyRef),
		BrowserOIDCClientIDName:     refName(spec.Credentials.BrowserOIDCClientIDRef),
		BrowserOIDCClientSecretName: refName(spec.Credentials.BrowserOIDCClientSecretRef),
		GithubLoginIDPName:          refName(spec.Credentials.GithubLoginIDPRef),
		AuthAudienceName:            refName(spec.Credentials.AuthAudienceRef),
		ZitadelAdminTokenName:       refName(spec.Credentials.ZitadelAdminTokenRef),
		SpiceDBPresharedKeyName:     refName(spec.Credentials.SpiceDBPresharedKeyRef),
		EmailIdentityHMACKeyName:    refName(spec.Credentials.EmailIdentityHMACKeyRef),
	}
	required := map[string]string{
		"installationID":                      cfg.InstallationID,
		"auth.issuerURL":                      cfg.AuthIssuerURL,
		"auth.browserAuthPublicBaseURL":       cfg.BrowserAuthPublicBaseURL,
		"auth.pwnedPasswordsRangeEndpoint":    cfg.PwnedPasswordsRangeEndpoint,
		"zitadel.host":                        cfg.ZitadelHost,
		"postgres.dsn":                        cfg.PostgresDSN,
		"clickhouse.address":                  cfg.ClickHouseAddress,
		"clickhouse.user":                     cfg.ClickHouseUser,
		"clickhouse.caCertPath":               cfg.ClickHouseCACertPath,
		"spiffe.endpointSocket":               cfg.SPIFFEEndpointSocket,
		"credentials.zitadelActionSigningKey": cfg.ZitadelActionSigningKeyName,
		"credentials.browserOIDCClientID":     cfg.BrowserOIDCClientIDName,
		"credentials.browserOIDCClientSecret": cfg.BrowserOIDCClientSecretName,
		"credentials.authAudience":            cfg.AuthAudienceName,
		"credentials.zitadelAdminToken":       cfg.ZitadelAdminTokenName,
		"credentials.spiceDBPresharedKey":     cfg.SpiceDBPresharedKeyName,
		"credentials.emailIdentityHMACKey":    cfg.EmailIdentityHMACKeyName,
	}
	for field, value := range required {
		if strings.TrimSpace(value) == "" {
			return iamRuntimeConfig{}, fmt.Errorf("IAMService.spec.%s is required", field)
		}
	}
	if cfg.PostgresMaxConns <= 0 {
		return iamRuntimeConfig{}, errors.New("IAMService.spec.postgres.maxConns must be positive")
	}
	return cfg, nil
}

func refName(ref objectRef) string {
	if strings.TrimSpace(ref.APIVersion) == "" || strings.TrimSpace(ref.Kind) == "" {
		return ""
	}
	return strings.TrimSpace(ref.Name)
}
