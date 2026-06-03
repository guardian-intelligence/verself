package deployengine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/hashicorp/nomad/api"

	"github.com/verself/deployment-service/controlplane"
	"github.com/verself/deployment-service/deploycontract"
	"github.com/verself/deployment-service/internal/deploymodel"
	"github.com/verself/deployment-service/internal/siteinject"
	"github.com/verself/deployment-service/siteconfig"
)

const (
	nomadComponentSchemaVersion = 7
	artifactSourcePrefix        = "verself-artifact://"

	deployPhasePreArtifact = "pre_artifact"
	deployPhasePlatform    = "platform"
	deployPhaseProduct     = "product"
	deployPhaseEdge        = "edge"
)

type deployInputs struct {
	SHA                string
	DeployRunKey       string
	SiteCfg            siteConfig
	SiteModel          siteconfig.Model
	Components         []nomadComponentDescriptor
	Artifacts          []deploymodel.Artifact
	Bindings           map[string]artifactBinding
	ControlPlane       controlplane.Bundle
	ControlPlaneSHA256 string
	ControlPlaneObject objectArtifact
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
	SitePrefix             string `json:"site_prefix"`
	ChecksumAlgorithm      string `json:"checksum_algorithm"`
	Public                 *bool  `json:"public"`
	CloudflareAccountID    string `json:"cloudflare_account_id"`
	CloudflareAccountIDEnv string `json:"cloudflare_account_id_env"`
	ControlPlaneAddr       string `json:"control_plane_addr"`
}

type nomadComponentDescriptor struct {
	SchemaVersion int                       `json:"schema_version"`
	Label         string                    `json:"label"`
	Component     string                    `json:"component"`
	DeployPhase   string                    `json:"deploy_phase"`
	JobID         string                    `json:"job_id"`
	JobSpec       string                    `json:"job_spec"`
	JobSpecPath   string                    `json:"job_spec_path"`
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
	Source    string
	Job       *api.Job
}

type objectArtifact struct {
	Artifact deploymodel.Artifact
	Body     []byte
	Label    string
}

type authoredNomadSpecParser interface {
	ParseJobHCL(context.Context, []byte, string) (*api.Job, error)
}

func buildDeployInputs(ctx context.Context, exec execution) (*deployInputs, error) {
	repoRoot := exec.RepoRoot
	site := exec.Site
	sha := exec.SHA
	deployRunKey := exec.DeployRunKey
	cfg, err := loadSiteConfig(repoRoot, site)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(exec.NomadAddr) != "" {
		cfg.NomadAddr = strings.TrimSpace(exec.NomadAddr)
	}
	if strings.TrimSpace(exec.R2ControlPlaneAddr) != "" {
		cfg.ArtifactDelivery.ControlPlaneAddr = strings.TrimSpace(exec.R2ControlPlaneAddr)
	}
	model, err := siteconfig.Load(repoRoot, site)
	if err != nil {
		return nil, err
	}
	if _, err := deploycontract.ValidateRepo(repoRoot); err != nil {
		return nil, err
	}
	_, descriptorPaths, err := buildNomadComponentDescriptors(ctx, repoRoot, exec.BazelBuildFlags...)
	if err != nil {
		return nil, err
	}
	descriptors, err := loadNomadComponentDescriptors(site, descriptorPaths)
	if err != nil {
		return nil, err
	}
	bindings, artifacts, err := bindNomadArtifacts(repoRoot, cfg.ArtifactDelivery, descriptors)
	if err != nil {
		return nil, err
	}
	bundle, err := controlplane.LoadBundle(repoRoot, site, controlPlaneComponents(descriptors))
	if err != nil {
		return nil, err
	}
	bundleSHA256, err := controlplane.BundleSHA256(bundle)
	if err != nil {
		return nil, err
	}
	controlPlaneObject, err := bindControlPlaneBundleArtifact(cfg.ArtifactDelivery, bundle)
	if err != nil {
		return nil, err
	}
	return &deployInputs{
		SHA:                sha,
		DeployRunKey:       deployRunKey,
		SiteCfg:            cfg,
		SiteModel:          model,
		Components:         descriptors,
		Artifacts:          artifacts,
		Bindings:           bindings,
		ControlPlane:       bundle,
		ControlPlaneSHA256: bundleSHA256,
		ControlPlaneObject: controlPlaneObject,
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
	if strings.TrimSpace(raw.ArtifactDelivery.Bucket) != "" {
		return siteConfig{}, fmt.Errorf("%s: artifact_delivery.bucket belongs to src/integrations/cloudflare/account.json", path)
	}
	if strings.TrimSpace(raw.ArtifactDelivery.CloudflareAccountID) != "" || strings.TrimSpace(raw.ArtifactDelivery.CloudflareAccountIDEnv) != "" {
		return siteConfig{}, fmt.Errorf("%s: artifact_delivery.cloudflare_account_id belongs to src/integrations/cloudflare/account.json", path)
	}
	if raw.ArtifactDelivery.KeyPrefix == "" {
		return siteConfig{}, fmt.Errorf("%s: artifact_delivery requires key_prefix", path)
	}
	sitePrefix, err := artifactSitePrefix(site)
	if err != nil {
		return siteConfig{}, fmt.Errorf("%s: %w", path, err)
	}
	cloudflare, err := siteconfig.LoadCloudflareProvider(repoRoot)
	if err != nil {
		return siteConfig{}, err
	}
	raw.ArtifactDelivery.CloudflareAccountID = cloudflare.AccountID
	raw.ArtifactDelivery.Bucket = cloudflare.DeploymentArtifactsBucket
	raw.ArtifactDelivery.SitePrefix = sitePrefix
	if raw.ArtifactDelivery.ChecksumAlgorithm != "sha256" {
		return siteConfig{}, fmt.Errorf("%s: only sha256 artifact checksums are supported", path)
	}
	if strings.TrimSpace(raw.ArtifactDelivery.ControlPlaneAddr) == "" {
		raw.ArtifactDelivery.ControlPlaneAddr = "http://127.0.0.1:18732"
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
			return nil, fmt.Errorf("%s: deploy_phase must be one of %s", path, strings.Join(deployPhaseValues, ", "))
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

func prepareNomadJobsForSite(ctx context.Context, parser authoredNomadSpecParser, repoRoot string, model siteconfig.Model, bindings map[string]artifactBinding, components []nomadComponentDescriptor, taskUserResolver TaskUserResolver) ([]nomadJob, error) {
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
		if err := applyTaskSecretTemplateOwnership(ctx, job, taskUserResolver); err != nil {
			return nil, fmt.Errorf("%s: %w", component.Label, err)
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

var deployPhaseValues = []string{
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

func artifactSitePrefix(site string) (string, error) {
	site = strings.TrimSpace(site)
	if site == "" {
		return "", errors.New("site is required for artifact object prefix")
	}
	if site == "." || site == ".." || strings.Contains(site, "/") || strings.Contains(site, "\\") {
		return "", fmt.Errorf("site %q cannot be used as an artifact object prefix segment", site)
	}
	for _, r := range site {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '-' {
			return "", fmt.Errorf("site %q cannot be used as an artifact object prefix segment", site)
		}
	}
	return site, nil
}
