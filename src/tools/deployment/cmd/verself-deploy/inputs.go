package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/hashicorp/nomad/api"

	"github.com/verself/deployment-tools/internal/controlplane"
	"github.com/verself/deployment-tools/internal/deploycontract"
	"github.com/verself/deployment-tools/internal/deploymodel"
	"github.com/verself/deployment-tools/internal/siteconfig"
	"github.com/verself/deployment-tools/internal/siteinject"
)

const (
	nomadComponentSchemaVersion = 6
	artifactSourcePrefix        = "verself-artifact://"

	deployPhasePreArtifact = "pre_artifact"
	deployPhasePlatform    = "platform"
	deployPhaseProduct     = "product"
	deployPhaseEdge        = "edge"
)

type deployInputs struct {
	SHA          string
	DeployRunKey string
	SiteCfg      siteConfig
	SiteModel    siteconfig.Model
	Components   []nomadComponentDescriptor
	Artifacts    []deploymodel.Artifact
	Bindings     map[string]artifactBinding
	ControlPlane controlplane.Bundle
}

type siteConfig struct {
	NomadAddr        string
	ArtifactDelivery artifactDeliveryPolicy
}

type rawSiteConfig struct {
	ArtifactDelivery artifactDeliveryPolicy `json:"artifact_delivery"`
	NomadAddr        string                 `json:"nomad_addr"`
}

type artifactDeliveryPolicy struct {
	deploymodel.ArtifactDelivery
	KeyPrefix              string `json:"key_prefix"`
	ChecksumAlgorithm      string `json:"checksum_algorithm"`
	Public                 *bool  `json:"public"`
	CloudflareAccountID    string `json:"cloudflare_account_id"`
	CloudflareAccountIDEnv string `json:"cloudflare_account_id_env"`
	ControlPlaneAddr       string `json:"control_plane_addr"`
	ControlPlaneTokenFile  string `json:"control_plane_token_file"`
}

type nomadComponentDescriptor struct {
	SchemaVersion int                       `json:"schema_version"`
	Label         string                    `json:"label"`
	Component     string                    `json:"component"`
	DeployPhase   string                    `json:"deploy_phase"`
	JobID         string                    `json:"job_id"`
	JobSpec       string                    `json:"job_spec"`
	JobSpecPath   string                    `json:"job_spec_path"`
	Provides      []string                  `json:"provides"`
	Requires      []string                  `json:"requires"`
	Sites         []string                  `json:"sites"`
	Artifacts     []nomadDescriptorArtifact `json:"artifacts"`
	PreArtifacts  []nomadDescriptorArtifact `json:"pre_artifacts"`
	DigestInputs  []nomadDescriptorInput    `json:"digest_inputs"`
}

type nomadDescriptorArtifact struct {
	Label  string `json:"label"`
	Output string `json:"output"`
	Path   string `json:"path"`
}

type nomadDescriptorInput struct {
	Label     string `json:"label"`
	Path      string `json:"path"`
	ShortPath string `json:"short_path"`
}

type nomadJob struct {
	Component string
	JobID     string
	Provides  []string
	Requires  []string
	Source    string
	Job       *api.Job
}

type authoredNomadSpecParser interface {
	ParseJobHCL(context.Context, []byte, string) (*api.Job, error)
}

func buildDeployInputs(ctx context.Context, repoRoot, site, sha, deployRunKey string) (*deployInputs, error) {
	cfg, err := loadSiteConfig(repoRoot, site)
	if err != nil {
		return nil, err
	}
	model, err := siteconfig.Load(repoRoot, site)
	if err != nil {
		return nil, err
	}
	if _, err := deploycontract.ValidateRepo(repoRoot); err != nil {
		return nil, err
	}
	_, descriptorPaths, err := buildNomadComponentDescriptors(ctx, repoRoot)
	if err != nil {
		return nil, err
	}
	descriptors, err := loadNomadComponentDescriptors(site, descriptorPaths)
	if err != nil {
		return nil, err
	}
	ordered, err := orderNomadComponents(descriptors)
	if err != nil {
		return nil, err
	}
	bindings, artifacts, err := bindNomadArtifacts(repoRoot, cfg.ArtifactDelivery, ordered)
	if err != nil {
		return nil, err
	}
	bundle, err := controlplane.LoadBundle(repoRoot, site, sha, deployRunKey, controlPlaneComponents(ordered))
	if err != nil {
		return nil, err
	}
	return &deployInputs{
		SHA:          sha,
		DeployRunKey: deployRunKey,
		SiteCfg:      cfg,
		SiteModel:    model,
		Components:   ordered,
		Artifacts:    artifacts,
		Bindings:     bindings,
		ControlPlane: bundle,
	}, nil
}

func loadSiteConfig(repoRoot, site string) (siteConfig, error) {
	path := filepath.Join(repoRoot, "src", "host", "sites", site, "site.json")
	body, err := os.ReadFile(path)
	if err != nil {
		return siteConfig{}, fmt.Errorf("read %s: %w", path, err)
	}
	var raw rawSiteConfig
	if err := json.Unmarshal(body, &raw); err != nil {
		return siteConfig{}, fmt.Errorf("decode %s: %w", path, err)
	}
	if raw.ArtifactDelivery.Kind != "cloudflare_r2_control_plane" {
		return siteConfig{}, fmt.Errorf("%s: artifact_delivery.kind must be cloudflare_r2_control_plane", path)
	}
	if raw.ArtifactDelivery.Public == nil || *raw.ArtifactDelivery.Public {
		return siteConfig{}, fmt.Errorf("%s: artifact_delivery.public must be false", path)
	}
	if raw.ArtifactDelivery.Bucket == "" || raw.ArtifactDelivery.KeyPrefix == "" {
		return siteConfig{}, fmt.Errorf("%s: artifact_delivery requires bucket and key_prefix", path)
	}
	accountID, err := resolveCloudflareAccountID(raw.ArtifactDelivery)
	if err != nil {
		return siteConfig{}, fmt.Errorf("%s: %w", path, err)
	}
	raw.ArtifactDelivery.CloudflareAccountID = accountID
	raw.ArtifactDelivery.GetterSourcePrefix = "s3::https://" + accountID + ".r2.cloudflarestorage.com/" + raw.ArtifactDelivery.Bucket
	if raw.ArtifactDelivery.GetterOptions == nil {
		raw.ArtifactDelivery.GetterOptions = map[string]string{}
	}
	region := strings.TrimSpace(raw.ArtifactDelivery.GetterOptions["region"])
	if region == "" {
		raw.ArtifactDelivery.GetterOptions["region"] = "auto"
	} else if region != "auto" {
		return siteConfig{}, fmt.Errorf("%s: cloudflare_r2_control_plane artifact_delivery.getter_options.region must be auto", path)
	}
	if raw.ArtifactDelivery.GetterCredentials.EnvironmentFile == "" || raw.ArtifactDelivery.GetterCredentials.AccessKeyIDEnv == "" || raw.ArtifactDelivery.GetterCredentials.SecretAccessKeyEnv == "" {
		return siteConfig{}, fmt.Errorf("%s: artifact_delivery.getter_credentials requires environment_file, access_key_id_env, and secret_access_key_env", path)
	}
	if raw.ArtifactDelivery.GetterCredentials.Source != "host_environment" {
		return siteConfig{}, fmt.Errorf("%s: artifact_delivery.getter_credentials.source must be host_environment", path)
	}
	if raw.ArtifactDelivery.ChecksumAlgorithm != "sha256" {
		return siteConfig{}, fmt.Errorf("%s: only sha256 artifact checksums are supported", path)
	}
	if strings.TrimSpace(raw.ArtifactDelivery.ControlPlaneAddr) == "" {
		raw.ArtifactDelivery.ControlPlaneAddr = "http://127.0.0.1:18732"
	}
	raw.ArtifactDelivery.ControlPlaneTokenFile = strings.TrimSpace(raw.ArtifactDelivery.ControlPlaneTokenFile)
	if raw.ArtifactDelivery.ControlPlaneTokenFile == "" {
		return siteConfig{}, fmt.Errorf("%s: artifact_delivery.control_plane_token_file is required", path)
	}
	if !filepath.IsAbs(raw.ArtifactDelivery.ControlPlaneTokenFile) {
		raw.ArtifactDelivery.ControlPlaneTokenFile = filepath.Join(repoRoot, filepath.FromSlash(raw.ArtifactDelivery.ControlPlaneTokenFile))
	}
	if raw.NomadAddr == "" {
		raw.NomadAddr = "http://127.0.0.1:4646"
	}
	return siteConfig{NomadAddr: raw.NomadAddr, ArtifactDelivery: raw.ArtifactDelivery}, nil
}

func loadNomadComponentDescriptors(site string, paths []string) ([]nomadComponentDescriptor, error) {
	if len(paths) == 0 {
		return nil, errors.New("at least one Nomad component descriptor is required")
	}
	components := []nomadComponentDescriptor{}
	seenLabels := map[string]bool{}
	for _, path := range paths {
		body, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", path, err)
		}
		var component nomadComponentDescriptor
		if err := json.Unmarshal(body, &component); err != nil {
			return nil, fmt.Errorf("decode %s: %w", path, err)
		}
		if component.SchemaVersion != nomadComponentSchemaVersion {
			return nil, fmt.Errorf("%s: unsupported nomad_component schema_version=%d", path, component.SchemaVersion)
		}
		if !componentInSite(component.Sites, site) {
			continue
		}
		if component.Label == "" || component.Component == "" || component.JobID == "" || component.JobSpec == "" || component.JobSpecPath == "" {
			return nil, fmt.Errorf("%s: component descriptor requires label, component, job_id, job_spec, and job_spec_path", path)
		}
		if !validDeployPhase(component.DeployPhase) {
			return nil, fmt.Errorf("%s: deploy_phase must be one of %s", path, strings.Join(deployPhaseOrder, ", "))
		}
		if len(component.Provides) == 0 {
			component.Provides = []string{"nomad:job:" + component.JobID}
		}
		if seenLabels[component.Label] {
			return nil, fmt.Errorf("duplicate Nomad component descriptor label %s", component.Label)
		}
		seenLabels[component.Label] = true
		for _, artifact := range component.Artifacts {
			if artifact.Label == "" || artifact.Output == "" || artifact.Path == "" {
				return nil, fmt.Errorf("%s: artifact entries require label, output, and path", path)
			}
		}
		for _, artifact := range component.PreArtifacts {
			if artifact.Label == "" || artifact.Output == "" || artifact.Path == "" {
				return nil, fmt.Errorf("%s: pre_artifacts entries require label, output, and path", path)
			}
		}
		for _, input := range component.DigestInputs {
			if input.Label == "" || input.Path == "" || input.ShortPath == "" {
				return nil, fmt.Errorf("%s: digest_inputs entries require label, path, and short_path", path)
			}
		}
		components = append(components, component)
	}
	if len(components) == 0 {
		return nil, fmt.Errorf("no Nomad components participate in site %q", site)
	}
	return components, nil
}

func componentInSite(sites []string, site string) bool {
	if len(sites) == 0 {
		return true
	}
	for _, candidate := range sites {
		if candidate == site {
			return true
		}
	}
	return false
}

func prepareNomadJobs(ctx context.Context, parser authoredNomadSpecParser, repoRoot string, components []nomadComponentDescriptor) ([]nomadJob, error) {
	return prepareNomadJobsForSite(ctx, parser, repoRoot, siteconfig.Model{}, nil, components)
}

func prepareNomadJobsForSite(ctx context.Context, parser authoredNomadSpecParser, repoRoot string, model siteconfig.Model, bindings map[string]artifactBinding, components []nomadComponentDescriptor) ([]nomadJob, error) {
	seenJobIDs := map[string]bool{}
	jobs := make([]nomadJob, 0, len(components))
	for _, component := range components {
		if seenJobIDs[component.JobID] {
			return nil, fmt.Errorf("duplicate Nomad job_id %s", component.JobID)
		}
		seenJobIDs[component.JobID] = true
		specPath := resolveWorkspacePath(repoRoot, component.JobSpecPath)
		job, err := loadAuthoredNomadSpec(ctx, parser, specPath)
		if err != nil {
			return nil, err
		}
		if bindings != nil {
			if _, err := bindArtifactsInSpec(job, bindings); err != nil {
				return nil, fmt.Errorf("%s: %w", component.Label, err)
			}
		}
		if model.Site != "" {
			if err := siteinject.Apply(job, model); err != nil {
				return nil, fmt.Errorf("%s: %w", component.Label, err)
			}
		}
		parsedID := ""
		if job.ID != nil {
			parsedID = *job.ID
		}
		if parsedID != component.JobID {
			return nil, fmt.Errorf("%s: descriptor job_id %q does not match authored Nomad Job.ID %q", component.Label, component.JobID, parsedID)
		}
		jobs = append(jobs, nomadJob{
			Component: component.Component,
			JobID:     component.JobID,
			Provides:  append([]string(nil), component.Provides...),
			Requires:  append([]string(nil), component.Requires...),
			Source:    specPath,
			Job:       job,
		})
	}
	return jobs, nil
}

func loadAuthoredNomadSpec(ctx context.Context, parser authoredNomadSpecParser, path string) (*api.Job, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	if parser == nil {
		return nil, fmt.Errorf("nomad HCL parser is required for %s", path)
	}
	return parser.ParseJobHCL(ctx, body, path)
}

func resolveWorkspacePath(repoRoot, path string) string {
	if filepath.IsAbs(path) || repoRoot == "" {
		return path
	}
	return filepath.Join(repoRoot, filepath.FromSlash(path))
}

var deployPhaseOrder = []string{
	deployPhasePreArtifact,
	deployPhasePlatform,
	deployPhaseProduct,
	deployPhaseEdge,
}

var deployPhaseRank = map[string]int{
	deployPhasePreArtifact: 0,
	deployPhasePlatform:    1,
	deployPhaseProduct:     2,
	deployPhaseEdge:        3,
}

func validDeployPhase(value string) bool {
	_, ok := deployPhaseRank[value]
	return ok
}

func orderNomadComponents(components []nomadComponentDescriptor) ([]nomadComponentDescriptor, error) {
	remaining := map[string]nomadComponentDescriptor{}
	provider := map[string]string{}
	for _, component := range components {
		remaining[component.Label] = component
		for _, provide := range component.Provides {
			if prior := provider[provide]; prior != "" && prior != component.Label {
				return nil, fmt.Errorf("resource %s is provided by both %s and %s", provide, prior, component.Label)
			}
			provider[provide] = component.Label
		}
	}
	ordered := make([]nomadComponentDescriptor, 0, len(components))
	for len(remaining) > 0 {
		ready := make([]nomadComponentDescriptor, 0, len(remaining))
		for label, component := range remaining {
			blocked := false
			for _, need := range component.Requires {
				if owner := provider[need]; owner != "" {
					if _, pending := remaining[owner]; pending && owner != label {
						blocked = true
						break
					}
				}
			}
			if !blocked {
				ready = append(ready, component)
			}
		}
		if len(ready) == 0 {
			labels := make([]string, 0, len(remaining))
			for label := range remaining {
				labels = append(labels, label)
			}
			sort.Strings(labels)
			return nil, fmt.Errorf("cyclic Nomad component requirements among %s", strings.Join(labels, ", "))
		}
		sort.Slice(ready, func(i, j int) bool {
			if deployPhaseRank[ready[i].DeployPhase] != deployPhaseRank[ready[j].DeployPhase] {
				return deployPhaseRank[ready[i].DeployPhase] < deployPhaseRank[ready[j].DeployPhase]
			}
			return ready[i].JobID < ready[j].JobID
		})
		for _, component := range ready {
			ordered = append(ordered, component)
			delete(remaining, component.Label)
		}
	}
	sort.SliceStable(ordered, func(i, j int) bool {
		if deployPhaseRank[ordered[i].DeployPhase] != deployPhaseRank[ordered[j].DeployPhase] {
			return deployPhaseRank[ordered[i].DeployPhase] < deployPhaseRank[ordered[j].DeployPhase]
		}
		return false
	})
	return ordered, nil
}

func controlPlaneComponents(components []nomadComponentDescriptor) []controlplane.Component {
	out := make([]controlplane.Component, 0, len(components))
	for _, component := range components {
		jobSpec := component.JobSpec
		if jobSpec == "" {
			jobSpec = component.JobSpecPath
		}
		out = append(out, controlplane.Component{
			Component: component.Component,
			JobID:     component.JobID,
			JobSpec:   jobSpec,
		})
	}
	return out
}

func resolveCloudflareAccountID(policy artifactDeliveryPolicy) (string, error) {
	direct := strings.TrimSpace(policy.CloudflareAccountID)
	envName := strings.TrimSpace(policy.CloudflareAccountIDEnv)
	if direct != "" && envName != "" {
		return "", errors.New("artifact_delivery must declare only one of cloudflare_account_id or cloudflare_account_id_env")
	}
	accountID := direct
	if envName != "" {
		accountID = strings.TrimSpace(os.Getenv(envName))
		if accountID == "" {
			return "", fmt.Errorf("artifact_delivery.cloudflare_account_id_env %s is unset", envName)
		}
	}
	accountID = strings.ToLower(accountID)
	if !isCloudflareAccountID(accountID) {
		return "", errors.New("artifact_delivery.cloudflare_account_id must be a 32-character hex Cloudflare account ID")
	}
	return accountID, nil
}

func isCloudflareAccountID(value string) bool {
	if len(value) != 32 {
		return false
	}
	for _, r := range value {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}
