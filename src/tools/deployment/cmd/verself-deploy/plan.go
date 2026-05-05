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

	"github.com/verself/deployment-tools/internal/deploymodel"
	"github.com/verself/deployment-tools/internal/identity"
	"github.com/verself/deployment-tools/internal/nomadclient"
	"github.com/verself/deployment-tools/internal/runtime"
)

const artifactSourcePrefix = "verself-artifact://"

type deployPlan struct {
	Identity  identity.Snapshot
	SHA       string
	Site      string
	SiteCfg   siteConfig
	Artifacts []deploymodel.Artifact
	Jobs      []deploymodel.NomadJob
}

type siteConfig struct {
	NomadAddr        string
	ArtifactDelivery artifactDeliveryPolicy
}

type artifactDeliveryPolicy struct {
	deploymodel.ArtifactDelivery
	KeyPrefix         string `json:"key_prefix"`
	ChecksumAlgorithm string `json:"checksum_algorithm"`
	Public            *bool  `json:"public"`
}

type rawSiteConfig struct {
	ArtifactDelivery artifactDeliveryPolicy `json:"artifact_delivery"`
	NomadAddr        string                 `json:"nomad_addr"`
}

type nomadComponentDescriptor struct {
	SchemaVersion int                       `json:"schema_version"`
	Label         string                    `json:"label"`
	Component     string                    `json:"component"`
	DependsOn     []string                  `json:"depends_on"`
	JobID         string                    `json:"job_id"`
	JobSpec       string                    `json:"job_spec"`
	JobSpecPath   string                    `json:"job_spec_path"`
	Provides      []string                  `json:"provides"`
	Requires      []string                  `json:"requires"`
	Sites         []string                  `json:"sites"`
	UnitID        string                    `json:"unit_id"`
	Artifacts     []nomadDescriptorArtifact `json:"artifacts"`
}

type nomadDescriptorArtifact struct {
	Label  string `json:"label"`
	Output string `json:"output"`
	Path   string `json:"path"`
}

type artifactBinding struct {
	Artifact deploymodel.Artifact
	Checksum string
	Label    string
	Path     string
}

type authoredNomadSpecParser interface {
	ParseJobHCL(context.Context, []byte, string) (*api.Job, error)
}

func buildDeployPlan(ctx context.Context, rt *runtime.Runtime, repoRoot, site, sha string, snap identity.Snapshot) (*deployPlan, error) {
	cfg, err := loadSiteConfig(repoRoot, site)
	if err != nil {
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
	forward, err := openNomadForward(ctx, rt, cfg.NomadAddr)
	if err != nil {
		return nil, err
	}
	defer func() { _ = forward.Close() }()
	nomad, err := nomadclient.New("http://" + forward.ListenAddr)
	if err != nil {
		return nil, err
	}
	bindings, artifacts, err := bindNomadArtifacts(repoRoot, cfg.ArtifactDelivery, ordered)
	if err != nil {
		return nil, err
	}
	jobs, err := resolveNomadJobs(ctx, nomad, repoRoot, ordered, bindings, snap.RunKey(), sha)
	if err != nil {
		return nil, err
	}
	return &deployPlan{
		Identity:  snap,
		SHA:       sha,
		Site:      site,
		SiteCfg:   cfg,
		Artifacts: artifacts,
		Jobs:      jobs,
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
	if raw.ArtifactDelivery.Public == nil || *raw.ArtifactDelivery.Public {
		return siteConfig{}, fmt.Errorf("%s: artifact_delivery.public must be false", path)
	}
	if raw.ArtifactDelivery.Bucket == "" || raw.ArtifactDelivery.GetterSourcePrefix == "" || raw.ArtifactDelivery.KeyPrefix == "" {
		return siteConfig{}, fmt.Errorf("%s: artifact_delivery requires bucket, getter_source_prefix, and key_prefix", path)
	}
	if raw.ArtifactDelivery.ChecksumAlgorithm != "sha256" {
		return siteConfig{}, fmt.Errorf("%s: only sha256 artifact checksums are supported", path)
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
		if component.SchemaVersion != 2 {
			return nil, fmt.Errorf("%s: unsupported nomad_component schema_version=%d", path, component.SchemaVersion)
		}
		if !componentInSite(component.Sites, site) {
			continue
		}
		if component.Label == "" || component.Component == "" || component.JobID == "" || component.JobSpec == "" || component.JobSpecPath == "" {
			return nil, fmt.Errorf("%s: component descriptor requires label, component, job_id, job_spec, and job_spec_path", path)
		}
		if component.UnitID == "" {
			component.UnitID = component.JobID
		}
		if len(component.Provides) == 0 {
			component.Provides = []string{"nomad:job:" + component.JobID}
		}
		if len(component.Requires) == 0 && len(component.DependsOn) > 0 {
			for _, dep := range component.DependsOn {
				if strings.HasPrefix(dep, "nomad:job:") {
					component.Requires = append(component.Requires, dep)
				} else {
					component.Requires = append(component.Requires, "nomad:job:"+dep)
				}
			}
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

func orderNomadComponents(components []nomadComponentDescriptor) ([]nomadComponentDescriptor, error) {
	byJobID := map[string]nomadComponentDescriptor{}
	providerByResource := map[string]string{}
	for _, component := range components {
		if _, exists := byJobID[component.JobID]; exists {
			return nil, fmt.Errorf("duplicate Nomad job_id %s", component.JobID)
		}
		byJobID[component.JobID] = component
		for _, provided := range component.Provides {
			if prior := providerByResource[provided]; prior != "" && prior != component.JobID {
				return nil, fmt.Errorf("resource %q is provided by both %s and %s", provided, prior, component.JobID)
			}
			providerByResource[provided] = component.JobID
		}
	}
	out := []nomadComponentDescriptor{}
	temporary := map[string]bool{}
	permanent := map[string]bool{}
	var visit func(string, []string) error
	visit = func(jobID string, stack []string) error {
		if permanent[jobID] {
			return nil
		}
		if temporary[jobID] {
			return fmt.Errorf("nomad dependency cycle: %s", strings.Join(append(stack, jobID), " -> "))
		}
		component, exists := byJobID[jobID]
		if !exists {
			return fmt.Errorf("unknown Nomad job dependency %q", jobID)
		}
		temporary[jobID] = true
		depJobs := map[string]bool{}
		for _, dep := range component.DependsOn {
			depJob := strings.TrimPrefix(dep, "nomad:job:")
			if depJob != "" {
				depJobs[depJob] = true
			}
		}
		for _, required := range component.Requires {
			if provider := providerByResource[required]; provider != "" {
				depJobs[provider] = true
			}
		}
		deps := make([]string, 0, len(depJobs))
		for dep := range depJobs {
			deps = append(deps, dep)
		}
		sort.Strings(deps)
		for _, dep := range deps {
			if _, exists := byJobID[dep]; !exists {
				return fmt.Errorf("%s requires unknown Nomad dependency %q", jobID, dep)
			}
			if dep == jobID {
				return fmt.Errorf("%s depends on itself", jobID)
			}
			if err := visit(dep, append(stack, jobID)); err != nil {
				return err
			}
		}
		temporary[jobID] = false
		permanent[jobID] = true
		out = append(out, component)
		return nil
	}
	jobIDs := make([]string, 0, len(byJobID))
	for jobID := range byJobID {
		jobIDs = append(jobIDs, jobID)
	}
	sort.Strings(jobIDs)
	for _, jobID := range jobIDs {
		if err := visit(jobID, nil); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func resolveNomadJobs(ctx context.Context, parser authoredNomadSpecParser, repoRoot string, components []nomadComponentDescriptor, bindings map[string]artifactBinding, runKey, sha string) ([]deploymodel.NomadJob, error) {
	referenced := map[string]bool{}
	jobs := make([]deploymodel.NomadJob, 0, len(components))
	for _, component := range components {
		specPath := resolveWorkspacePath(repoRoot, component.JobSpecPath)
		job, err := loadAuthoredNomadSpec(ctx, parser, specPath)
		if err != nil {
			return nil, err
		}
		seen, err := bindArtifactsInSpec(job, bindings)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", component.JobID, err)
		}
		for output := range seen {
			referenced[output] = true
		}
		artifactDigestInput, err := json.Marshal(canonicalArtifactDigestInput(seen, bindings))
		if err != nil {
			return nil, fmt.Errorf("%s: encode artifact digest input: %w", component.JobID, err)
		}
		artifactDigest := deploymodel.SHA256(artifactDigestInput)
		specSHA, err := stampNomadSpecMeta(job, artifactDigest, runKey, sha)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", component.JobID, err)
		}
		specBody, err := json.Marshal(struct {
			Job *api.Job `json:"Job"`
		}{Job: job})
		if err != nil {
			return nil, fmt.Errorf("%s: encode bound Nomad spec: %w", component.JobID, err)
		}
		jobs = append(jobs, deploymodel.NomadJob{
			JobID:          component.JobID,
			Component:      component.Component,
			DependsOn:      append([]string(nil), component.Requires...),
			SpecSHA256:     specSHA,
			ArtifactSHA256: artifactDigest,
			Spec:           specBody,
		})
	}
	for output := range bindings {
		if !referenced[output] {
			return nil, fmt.Errorf("artifact %q was declared by a Nomad component but not referenced by any authored job", output)
		}
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
