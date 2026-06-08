package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/verself/billing-service/internal/billingapi"
)

const (
	defaultGuardianResourceGraph      = "/home/ubuntu/.local/state/guardian/repo/workspace/.guardian/fly/document.json"
	defaultBillingServiceResource     = "billing-service"
	defaultBillingServicePublicAddr   = "127.0.0.1:4242"
	defaultBillingServiceInternalAddr = "127.0.0.1:4255"
	billingServiceAPIVersion          = "billing.guardianintelligence.org/v1alpha1"
	billingServiceKind                = "BillingService"
	openBaoSecretPathAPIVersion       = "openbao.guardianintelligence.org/v1alpha1"
	openBaoSecretPathKind             = "SecretPath"
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

type billingRuntimeConfig struct {
	InstallationID              string
	AuthIssuerURL               string
	AuthAudienceName            string
	PostgresDSN                 string
	PostgresMaxConns            int
	PostgresMinConns            int
	PostgresMaxConnLifetimeSecs int
	PostgresMaxConnIdleSecs     int
	ClickHouseAddress           string
	ClickHouseUser              string
	ClickHouseCACertPath        string
	TigerBeetleAddresses        []string
	TigerBeetleClusterID        uint64
	BillingReturnOrigins        []string
	StripeSecretKeyName         string
	StripeWebhookSecretName     string
	SPIFFEEndpointSocket        string
}

func parseRunOptions(args []string) (runOptions, error) {
	fs := flag.NewFlagSet(serviceName, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	opts := runOptions{
		ResourceGraph:      defaultGuardianResourceGraph,
		ResourceName:       defaultBillingServiceResource,
		ListenAddr:         defaultBillingServicePublicAddr,
		InternalListenAddr: defaultBillingServiceInternalAddr,
	}
	fs.StringVar(&opts.ResourceGraph, "resource-graph", opts.ResourceGraph, "Guardian resource graph JSON path")
	fs.StringVar(&opts.ResourceName, "resource-name", opts.ResourceName, "BillingService resource name")
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
		ResourceName:  defaultBillingServiceResource,
	}
	fs.StringVar(&opts.ResourceGraph, "resource-graph", opts.ResourceGraph, "Guardian resource graph JSON path")
	fs.StringVar(&opts.ResourceName, "resource-name", opts.ResourceName, "BillingService resource name")
	if err := fs.Parse(args); err != nil {
		return migrationOptions{}, nil, err
	}
	return opts, fs.Args(), nil
}

func loadBillingRuntimeConfig(path string, name string) (billingRuntimeConfig, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return billingRuntimeConfig{}, errors.New("resource graph path is required")
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return billingRuntimeConfig{}, errors.New("BillingService resource name is required")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return billingRuntimeConfig{}, fmt.Errorf("read Guardian resource graph: %w", err)
	}
	var doc billingGuardianDocument
	if err := decodeStrictJSON(data, &doc); err != nil {
		return billingRuntimeConfig{}, fmt.Errorf("decode Guardian resource graph: %w", err)
	}
	var matches []billingGuardianResource
	for _, resource := range doc.Resources {
		if resource.APIVersion == billingServiceAPIVersion &&
			resource.Kind == billingServiceKind &&
			resource.Metadata.Name == name {
			matches = append(matches, resource)
		}
	}
	if len(matches) != 1 {
		return billingRuntimeConfig{}, fmt.Errorf("expected exactly one BillingService resource named %q, found %d", name, len(matches))
	}
	var spec billingServiceSpec
	if err := decodeStrictJSON(matches[0].Spec, &spec); err != nil {
		return billingRuntimeConfig{}, fmt.Errorf("decode BillingService spec: %w", err)
	}
	return billingConfigFromSpec(spec)
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

type billingGuardianDocument struct {
	Entrypoint json.RawMessage           `json:"entrypoint"`
	Resources  []billingGuardianResource `json:"resources"`
}

type billingGuardianResource struct {
	APIVersion string          `json:"apiVersion"`
	Kind       string          `json:"kind"`
	Metadata   billingMeta     `json:"metadata"`
	Spec       json.RawMessage `json:"spec"`
}

type billingMeta struct {
	Name string `json:"name"`
}

type objectRef struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	Name       string `json:"name"`
}

type billingServiceSpec struct {
	InstallationID string `json:"installationID"`
	Auth           struct {
		IssuerURL   string    `json:"issuerURL"`
		AudienceRef objectRef `json:"audienceRef"`
	} `json:"auth"`
	Postgres struct {
		DSN                    string `json:"dsn"`
		MaxConns               int    `json:"maxConns"`
		MinConns               int    `json:"minConns"`
		MaxConnLifetimeSeconds int    `json:"maxConnLifetimeSeconds"`
		MaxConnIdleSeconds     int    `json:"maxConnIdleSeconds"`
	} `json:"postgres"`
	ClickHouse struct {
		Address    string `json:"address"`
		User       string `json:"user"`
		CACertPath string `json:"caCertPath"`
	} `json:"clickhouse"`
	TigerBeetle struct {
		Addresses []string `json:"addresses"`
		ClusterID int      `json:"clusterID"`
	} `json:"tigerbeetle"`
	Billing struct {
		ReturnOrigins []string `json:"returnOrigins"`
	} `json:"billing"`
	Stripe struct {
		SecretKeyRef     objectRef `json:"secretKeyRef"`
		WebhookSecretRef objectRef `json:"webhookSecretRef"`
	} `json:"stripe"`
	SPIFFE struct {
		EndpointSocket string `json:"endpointSocket"`
	} `json:"spiffe"`
}

func billingConfigFromSpec(spec billingServiceSpec) (billingRuntimeConfig, error) {
	authAudienceName, err := secretNameFromCredentialRef("auth.audienceRef", spec.Auth.AudienceRef)
	if err != nil {
		return billingRuntimeConfig{}, err
	}
	stripeSecretKeyName, err := secretNameFromCredentialRef("stripe.secretKeyRef", spec.Stripe.SecretKeyRef)
	if err != nil {
		return billingRuntimeConfig{}, err
	}
	stripeWebhookSecretName, err := secretNameFromCredentialRef("stripe.webhookSecretRef", spec.Stripe.WebhookSecretRef)
	if err != nil {
		return billingRuntimeConfig{}, err
	}
	tigerBeetleAddresses := compactStrings(spec.TigerBeetle.Addresses)
	billingReturnOrigins, err := billingapi.ParseBillingReturnOrigins(strings.Join(spec.Billing.ReturnOrigins, ","))
	if err != nil {
		return billingRuntimeConfig{}, err
	}
	if spec.TigerBeetle.ClusterID < 0 {
		return billingRuntimeConfig{}, errors.New("BillingService.spec.tigerbeetle.clusterID must be non-negative")
	}
	cfg := billingRuntimeConfig{
		InstallationID:              strings.TrimSpace(spec.InstallationID),
		AuthIssuerURL:               strings.TrimSpace(spec.Auth.IssuerURL),
		AuthAudienceName:            authAudienceName,
		PostgresDSN:                 strings.TrimSpace(spec.Postgres.DSN),
		PostgresMaxConns:            spec.Postgres.MaxConns,
		PostgresMinConns:            spec.Postgres.MinConns,
		PostgresMaxConnLifetimeSecs: spec.Postgres.MaxConnLifetimeSeconds,
		PostgresMaxConnIdleSecs:     spec.Postgres.MaxConnIdleSeconds,
		ClickHouseAddress:           strings.TrimSpace(spec.ClickHouse.Address),
		ClickHouseUser:              strings.TrimSpace(spec.ClickHouse.User),
		ClickHouseCACertPath:        strings.TrimSpace(spec.ClickHouse.CACertPath),
		TigerBeetleAddresses:        tigerBeetleAddresses,
		TigerBeetleClusterID:        uint64(spec.TigerBeetle.ClusterID),
		BillingReturnOrigins:        billingReturnOrigins,
		StripeSecretKeyName:         stripeSecretKeyName,
		StripeWebhookSecretName:     stripeWebhookSecretName,
		SPIFFEEndpointSocket:        strings.TrimSpace(spec.SPIFFE.EndpointSocket),
	}
	required := map[string]string{
		"installationID":        cfg.InstallationID,
		"auth.issuerURL":        cfg.AuthIssuerURL,
		"auth.audienceRef.name": cfg.AuthAudienceName,
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
		return billingRuntimeConfig{}, fmt.Errorf("BillingService missing required static config: %s", strings.Join(missing, ", "))
	}
	if cfg.PostgresMaxConns <= 0 {
		return billingRuntimeConfig{}, errors.New("BillingService.spec.postgres.maxConns must be positive")
	}
	if cfg.PostgresMinConns < 0 || cfg.PostgresMinConns > cfg.PostgresMaxConns {
		return billingRuntimeConfig{}, errors.New("BillingService.spec.postgres.minConns must be between zero and maxConns")
	}
	if cfg.PostgresMaxConnLifetimeSecs <= 0 || cfg.PostgresMaxConnIdleSecs <= 0 {
		return billingRuntimeConfig{}, errors.New("BillingService postgres connection lifetimes must be positive")
	}
	if len(cfg.TigerBeetleAddresses) == 0 {
		return billingRuntimeConfig{}, errors.New("BillingService.spec.tigerbeetle.addresses requires at least one address")
	}
	return cfg, nil
}

func secretNameFromCredentialRef(label string, ref objectRef) (string, error) {
	if ref.APIVersion != openBaoSecretPathAPIVersion || ref.Kind != openBaoSecretPathKind {
		return "", fmt.Errorf("%s must reference %s %s", label, openBaoSecretPathAPIVersion, openBaoSecretPathKind)
	}
	name := strings.TrimSpace(ref.Name)
	if name == "" {
		return "", fmt.Errorf("%s.name is required", label)
	}
	return name, nil
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
