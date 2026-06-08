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
	defaultGuardianResourceGraph  = "/home/ubuntu/.local/state/guardian/repo/workspace/.guardian/fly/document.json"
	defaultSecretsServiceResource = "secrets-service"
	defaultPublicListenAddr       = "127.0.0.1:4251"
	defaultInternalListenAddr     = "127.0.0.1:4253"
)

type runOptions struct {
	ResourceGraph      string
	ResourceName       string
	ListenAddr         string
	InternalListenAddr string
}

type secretsRuntimeConfig struct {
	InstallationID          string
	Environment             string
	AuthIssuerURL           string
	AuthAudienceName        string
	OpenBaoAddress          string
	OpenBaoCACertPath       string
	OpenBaoKVPrefix         string
	OpenBaoTransitPrefix    string
	OpenBaoJWTPrefix        string
	OpenBaoSPIFFEJWTPrefix  string
	OpenBaoWorkloadAudience string
	RuntimeSecretNamespace  string
	SPIFFEEndpointSocket    string
}

func parseRunOptions(args []string) (runOptions, error) {
	fs := flag.NewFlagSet("secrets-service", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	opts := runOptions{
		ResourceGraph:      defaultGuardianResourceGraph,
		ResourceName:       defaultSecretsServiceResource,
		ListenAddr:         defaultPublicListenAddr,
		InternalListenAddr: defaultInternalListenAddr,
	}
	fs.StringVar(&opts.ResourceGraph, "resource-graph", opts.ResourceGraph, "Guardian resource graph JSON path")
	fs.StringVar(&opts.ResourceName, "resource-name", opts.ResourceName, "SecretsService resource name")
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

func loadSecretsRuntimeConfig(path string, name string) (secretsRuntimeConfig, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return secretsRuntimeConfig{}, errors.New("resource graph path is required")
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return secretsRuntimeConfig{}, errors.New("SecretsService resource name is required")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return secretsRuntimeConfig{}, fmt.Errorf("read Guardian resource graph: %w", err)
	}
	var doc guardianDocument
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&doc); err != nil {
		return secretsRuntimeConfig{}, fmt.Errorf("decode Guardian resource graph: %w", err)
	}
	var matches []guardianResource
	for _, resource := range doc.Resources {
		if resource.APIVersion == "secrets.guardianintelligence.org/v1alpha1" &&
			resource.Kind == "SecretsService" &&
			resource.Metadata.Name == name {
			matches = append(matches, resource)
		}
	}
	if len(matches) != 1 {
		return secretsRuntimeConfig{}, fmt.Errorf("expected exactly one SecretsService resource named %q, found %d", name, len(matches))
	}
	var spec secretsServiceSpec
	if err := json.Unmarshal(matches[0].Spec, &spec); err != nil {
		return secretsRuntimeConfig{}, fmt.Errorf("decode SecretsService spec: %w", err)
	}
	return secretsConfigFromSpec(spec)
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

type secretsServiceSpec struct {
	InstallationID string `json:"installationID"`
	Environment    string `json:"environment"`
	Auth           struct {
		IssuerURL   string    `json:"issuerURL"`
		AudienceRef objectRef `json:"audienceRef"`
	} `json:"auth"`
	OpenBao struct {
		Address          string `json:"address"`
		CACertPath       string `json:"caCertPath"`
		KVPrefix         string `json:"kvPrefix"`
		TransitPrefix    string `json:"transitPrefix"`
		JWTPrefix        string `json:"jwtPrefix"`
		SPIFFEJWTPrefix  string `json:"spiffeJWTPrefix"`
		WorkloadAudience string `json:"workloadAudience"`
	} `json:"openbao"`
	RuntimeSecretNamespace string `json:"runtimeSecretNamespace"`
	SPIFFE                 struct {
		EndpointSocket string `json:"endpointSocket"`
	} `json:"spiffe"`
}

func secretsConfigFromSpec(spec secretsServiceSpec) (secretsRuntimeConfig, error) {
	cfg := secretsRuntimeConfig{
		InstallationID:          strings.TrimSpace(spec.InstallationID),
		Environment:             strings.TrimSpace(spec.Environment),
		AuthIssuerURL:           strings.TrimSpace(spec.Auth.IssuerURL),
		AuthAudienceName:        strings.TrimSpace(spec.Auth.AudienceRef.Name),
		OpenBaoAddress:          strings.TrimSpace(spec.OpenBao.Address),
		OpenBaoCACertPath:       strings.TrimSpace(spec.OpenBao.CACertPath),
		OpenBaoKVPrefix:         strings.TrimSpace(spec.OpenBao.KVPrefix),
		OpenBaoTransitPrefix:    strings.TrimSpace(spec.OpenBao.TransitPrefix),
		OpenBaoJWTPrefix:        strings.TrimSpace(spec.OpenBao.JWTPrefix),
		OpenBaoSPIFFEJWTPrefix:  strings.TrimSpace(spec.OpenBao.SPIFFEJWTPrefix),
		OpenBaoWorkloadAudience: strings.TrimSpace(spec.OpenBao.WorkloadAudience),
		RuntimeSecretNamespace:  strings.TrimSpace(spec.RuntimeSecretNamespace),
		SPIFFEEndpointSocket:    strings.TrimSpace(spec.SPIFFE.EndpointSocket),
	}
	required := map[string]string{
		"installationID":           cfg.InstallationID,
		"environment":              cfg.Environment,
		"auth.issuerURL":           cfg.AuthIssuerURL,
		"auth.audienceRef.name":    cfg.AuthAudienceName,
		"openbao.address":          cfg.OpenBaoAddress,
		"openbao.caCertPath":       cfg.OpenBaoCACertPath,
		"openbao.kvPrefix":         cfg.OpenBaoKVPrefix,
		"openbao.transitPrefix":    cfg.OpenBaoTransitPrefix,
		"openbao.jwtPrefix":        cfg.OpenBaoJWTPrefix,
		"openbao.spiffeJWTPrefix":  cfg.OpenBaoSPIFFEJWTPrefix,
		"openbao.workloadAudience": cfg.OpenBaoWorkloadAudience,
		"runtimeSecretNamespace":   cfg.RuntimeSecretNamespace,
		"spiffe.endpointSocket":    cfg.SPIFFEEndpointSocket,
	}
	var missing []string
	for label, value := range required {
		if strings.TrimSpace(value) == "" {
			missing = append(missing, label)
		}
	}
	if len(missing) > 0 {
		return secretsRuntimeConfig{}, fmt.Errorf("SecretsService missing required static config: %s", strings.Join(missing, ", "))
	}
	return cfg, nil
}
