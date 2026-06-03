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
	ArtifactNamespace string
	DeployRunKey      string
	NomadAddr         string
	SiteModel         siteconfig.Model
	Components        []nomadComponentDescriptor
	Artifacts         []deploymodel.Artifact
	Bindings          map[string]artifactBinding
}

type siteConfig struct {
	NomadAddr string
}

type rawSiteConfig struct {
	NomadAddr string `json:"nomad_addr"`
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

type authoredNomadSpecParser interface {
	ParseJobHCL(context.Context, []byte, string) (*api.Job, error)
}

func buildDeployInputs(exec execution) (*deployInputs, error) {
	repoRoot := exec.RepoRoot
	site := exec.Site
	artifactNamespace := exec.ArtifactNamespace
	deployRunKey := exec.DeployRunKey
	cfg, err := loadSiteConfig(repoRoot, site)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(exec.NomadAddr) != "" {
		cfg.NomadAddr = strings.TrimSpace(exec.NomadAddr)
	}
	model, err := siteconfig.Load(repoRoot, site)
	if err != nil {
		return nil, err
	}
	descriptors, err := loadNomadComponentDescriptors(site, exec.NomadComponentDescriptors)
	if err != nil {
		return nil, err
	}
	bindings, artifacts, err := bindNomadArtifacts(repoRoot, descriptors)
	if err != nil {
		return nil, err
	}
	return &deployInputs{
		ArtifactNamespace: artifactNamespace,
		DeployRunKey:      deployRunKey,
		NomadAddr:         cfg.NomadAddr,
		SiteModel:         model,
		Components:        descriptors,
		Artifacts:         artifacts,
		Bindings:          bindings,
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
	if raw.NomadAddr == "" {
		raw.NomadAddr = "http://127.0.0.1:4646"
	}
	return siteConfig(raw), nil
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

var deployPhaseSet = map[string]struct{}{
	deployPhasePreArtifact: {},
	deployPhasePlatform:    {},
	deployPhaseProduct:     {},
	deployPhaseEdge:        {},
}

func validDeployPhase(value string) bool {
	_, ok := deployPhaseSet[value]
	return ok
}
