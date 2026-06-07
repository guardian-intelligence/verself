package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/verself/object-storage-service/internal/objectstorage"
)

const (
	defaultGuardianResourceGraph = "/home/ubuntu/.local/state/guardian/repo/workspace/.guardian/fly/document.json"
	defaultResourceName          = "object-storage"
)

type serviceRunOptions struct {
	Role          runtimeRole
	ResourceGraph string
	ResourceName  string
	ListenAddr    string
	AdminAddr     string
	WriterID      string
}

type migrationOptions struct {
	ResourceGraph string
	ResourceName  string
}

type objectStorageRuntimeConfig struct {
	Site                       string
	Provider                   string
	R2Endpoint                 string
	R2Region                   string
	CredentialKEKName          string
	R2AdminAccessKeyIDName     string
	R2AdminSecretAccessKeyName string
	R2ProxyAccessKeyIDName     string
	R2ProxySecretAccessKeyName string
	AuthIssuerURL              string
	AuthAudienceCredentialName string
	DeploymentArtifactsBucket  string
	PostgresDSN                string
	ClickHouseAddress          string
	ClickHouseUser             string
	ClickHouseCACertPath       string
	SPIFFEEndpointSocket       string
}

func parseServiceRunOptions(args []string) (serviceRunOptions, error) {
	fs := flag.NewFlagSet("object-storage-service", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	roleRaw := fs.String("role", "", "runtime role: admin or s3")
	opts := serviceRunOptions{}
	fs.StringVar(&opts.ResourceGraph, "resource-graph", defaultGuardianResourceGraph, "Guardian resource graph JSON path")
	fs.StringVar(&opts.ResourceName, "resource-name", defaultResourceName, "ObjectStorageService resource name")
	fs.StringVar(&opts.ListenAddr, "listen-addr", "127.0.0.1:4256", "S3 listener address")
	fs.StringVar(&opts.AdminAddr, "admin-listen-addr", "127.0.0.1:4257", "admin listener address")
	fs.StringVar(&opts.WriterID, "writer-instance-id", "", "writer instance identifier")
	if err := fs.Parse(args); err != nil {
		return serviceRunOptions{}, err
	}
	if len(fs.Args()) != 0 {
		return serviceRunOptions{}, fmt.Errorf("unexpected args: %s", strings.Join(fs.Args(), " "))
	}
	role, err := parseRuntimeRole(*roleRaw)
	if err != nil {
		return serviceRunOptions{}, err
	}
	opts.Role = role
	return opts, nil
}

func parseMigrationOptions(args []string) (migrationOptions, []string, error) {
	fs := flag.NewFlagSet("object-storage-service migrate", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	opts := migrationOptions{
		ResourceGraph: defaultGuardianResourceGraph,
		ResourceName:  defaultResourceName,
	}
	fs.StringVar(&opts.ResourceGraph, "resource-graph", opts.ResourceGraph, "Guardian resource graph JSON path")
	fs.StringVar(&opts.ResourceName, "resource-name", opts.ResourceName, "ObjectStorageService resource name")
	if err := fs.Parse(args); err != nil {
		return migrationOptions{}, nil, err
	}
	return opts, fs.Args(), nil
}

func loadObjectStorageRuntimeConfig(path string, name string) (objectStorageRuntimeConfig, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return objectStorageRuntimeConfig{}, errors.New("resource graph path is required")
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return objectStorageRuntimeConfig{}, errors.New("ObjectStorageService resource name is required")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return objectStorageRuntimeConfig{}, fmt.Errorf("read Guardian resource graph: %w", err)
	}
	var doc guardianDocument
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&doc); err != nil {
		return objectStorageRuntimeConfig{}, fmt.Errorf("decode Guardian resource graph: %w", err)
	}
	var matches []guardianResource
	for _, resource := range doc.Resources {
		if resource.APIVersion == "objectstorage.guardianintelligence.org/v1alpha1" &&
			resource.Kind == "ObjectStorageService" &&
			resource.Metadata.Name == name {
			matches = append(matches, resource)
		}
	}
	if len(matches) != 1 {
		return objectStorageRuntimeConfig{}, fmt.Errorf("expected exactly one ObjectStorageService resource named %q, found %d", name, len(matches))
	}
	var spec objectStorageServiceSpec
	if err := json.Unmarshal(matches[0].Spec, &spec); err != nil {
		return objectStorageRuntimeConfig{}, fmt.Errorf("decode ObjectStorageService spec: %w", err)
	}
	return objectStorageConfigFromSpec(spec)
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

type objectStorageServiceSpec struct {
	Site        string `json:"site"`
	Credentials struct {
		CredentialKEKRef objectRef `json:"credentialKEKRef"`
	} `json:"credentials"`
	Provider struct {
		CloudflareR2 *struct {
			Endpoint     string    `json:"endpoint"`
			Region       string    `json:"region"`
			AuthorityRef objectRef `json:"authorityRef"`
			Credentials  struct {
				AdminAccessKeyIDRef     objectRef `json:"adminAccessKeyIDRef"`
				AdminSecretAccessKeyRef objectRef `json:"adminSecretAccessKeyRef"`
				ProxyAccessKeyIDRef     objectRef `json:"proxyAccessKeyIDRef"`
				ProxySecretAccessKeyRef objectRef `json:"proxySecretAccessKeyRef"`
			} `json:"credentials"`
		} `json:"cloudflareR2"`
	} `json:"provider"`
	Buckets []struct {
		Name         string `json:"name"`
		ProviderName string `json:"providerName"`
		Purpose      string `json:"purpose"`
	} `json:"buckets"`
	Auth struct {
		IssuerURL   string    `json:"issuerURL"`
		AudienceRef objectRef `json:"audienceRef"`
	} `json:"auth"`
	Postgres struct {
		DSN string `json:"dsn"`
	} `json:"postgres"`
	ClickHouse struct {
		Address    string `json:"address"`
		User       string `json:"user"`
		CACertPath string `json:"caCertPath"`
	} `json:"clickhouse"`
	SPIFFE struct {
		EndpointSocket string `json:"endpointSocket"`
	} `json:"spiffe"`
}

func objectStorageConfigFromSpec(spec objectStorageServiceSpec) (objectStorageRuntimeConfig, error) {
	r2 := spec.Provider.CloudflareR2
	if r2 == nil {
		return objectStorageRuntimeConfig{}, errors.New("ObjectStorageService provider.cloudflareR2 is required")
	}
	deploymentArtifactsBucket := ""
	for _, bucket := range spec.Buckets {
		if bucket.Purpose == "deploymentArtifacts" {
			if deploymentArtifactsBucket != "" {
				return objectStorageRuntimeConfig{}, errors.New("ObjectStorageService must declare exactly one deploymentArtifacts bucket")
			}
			deploymentArtifactsBucket = strings.TrimSpace(bucket.ProviderName)
		}
	}
	cfg := objectStorageRuntimeConfig{
		Site:                       strings.TrimSpace(spec.Site),
		Provider:                   objectstorage.ProviderCloudflareR2,
		R2Endpoint:                 strings.TrimSpace(r2.Endpoint),
		R2Region:                   strings.TrimSpace(r2.Region),
		CredentialKEKName:          refName(spec.Credentials.CredentialKEKRef),
		R2AdminAccessKeyIDName:     refName(r2.Credentials.AdminAccessKeyIDRef),
		R2AdminSecretAccessKeyName: refName(r2.Credentials.AdminSecretAccessKeyRef),
		R2ProxyAccessKeyIDName:     refName(r2.Credentials.ProxyAccessKeyIDRef),
		R2ProxySecretAccessKeyName: refName(r2.Credentials.ProxySecretAccessKeyRef),
		AuthIssuerURL:              strings.TrimSpace(spec.Auth.IssuerURL),
		AuthAudienceCredentialName: refName(spec.Auth.AudienceRef),
		DeploymentArtifactsBucket:  deploymentArtifactsBucket,
		PostgresDSN:                strings.TrimSpace(spec.Postgres.DSN),
		ClickHouseAddress:          strings.TrimSpace(spec.ClickHouse.Address),
		ClickHouseUser:             strings.TrimSpace(spec.ClickHouse.User),
		ClickHouseCACertPath:       strings.TrimSpace(spec.ClickHouse.CACertPath),
		SPIFFEEndpointSocket:       strings.TrimSpace(spec.SPIFFE.EndpointSocket),
	}
	return validateObjectStorageRuntimeConfig(cfg)
}

func refName(ref objectRef) string {
	return strings.TrimSpace(ref.Name)
}

func validateObjectStorageRuntimeConfig(cfg objectStorageRuntimeConfig) (objectStorageRuntimeConfig, error) {
	required := map[string]string{
		"site":                                                   cfg.Site,
		"provider.cloudflareR2.endpoint":                         cfg.R2Endpoint,
		"provider.cloudflareR2.region":                           cfg.R2Region,
		"credentials.credentialKEKRef.name":                      cfg.CredentialKEKName,
		"provider.cloudflareR2.credentials.adminAccessKeyID":     cfg.R2AdminAccessKeyIDName,
		"provider.cloudflareR2.credentials.adminSecretAccessKey": cfg.R2AdminSecretAccessKeyName,
		"provider.cloudflareR2.credentials.proxyAccessKeyID":     cfg.R2ProxyAccessKeyIDName,
		"provider.cloudflareR2.credentials.proxySecretAccessKey": cfg.R2ProxySecretAccessKeyName,
		"auth.issuerURL":                                         cfg.AuthIssuerURL,
		"auth.audienceRef.name":                                  cfg.AuthAudienceCredentialName,
		"deploymentArtifacts bucket":                             cfg.DeploymentArtifactsBucket,
		"postgres.dsn":                                           cfg.PostgresDSN,
		"clickhouse.address":                                     cfg.ClickHouseAddress,
		"clickhouse.user":                                        cfg.ClickHouseUser,
		"clickhouse.caCertPath":                                  cfg.ClickHouseCACertPath,
		"spiffe.endpointSocket":                                  cfg.SPIFFEEndpointSocket,
	}
	var missing []string
	for label, value := range required {
		if strings.TrimSpace(value) == "" {
			missing = append(missing, label)
		}
	}
	if len(missing) > 0 {
		return objectStorageRuntimeConfig{}, fmt.Errorf("ObjectStorageService missing required static config: %s", strings.Join(missing, ", "))
	}
	return cfg, nil
}
