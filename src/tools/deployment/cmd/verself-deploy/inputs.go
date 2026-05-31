package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/hashicorp/nomad/api"
)

const (
	nomadComponentSchemaVersion = 6
)

type deployInputs struct {
	SiteCfg    siteConfig
	Components []nomadComponentDescriptor
}

type siteConfig struct {
	NomadAddr string
}

type rawSiteConfig struct {
	NomadAddr string `json:"nomad_addr"`
}

type nomadComponentDescriptor struct {
	SchemaVersion int      `json:"schema_version"`
	Label         string   `json:"label"`
	Component     string   `json:"component"`
	JobID         string   `json:"job_id"`
	JobSpec       string   `json:"job_spec"`
	JobSpecPath   string   `json:"job_spec_path"`
	Sites         []string `json:"sites"`
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

func buildDeployInputs(ctx context.Context, repoRoot, site string) (*deployInputs, error) {
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
	return &deployInputs{
		SiteCfg:    cfg,
		Components: descriptors,
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
	return siteConfig{NomadAddr: raw.NomadAddr}, nil
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
		if seenLabels[component.Label] {
			return nil, fmt.Errorf("duplicate Nomad component descriptor label %s", component.Label)
		}
		seenLabels[component.Label] = true
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
	sort.Slice(jobs, func(i, j int) bool {
		return jobs[i].JobID < jobs[j].JobID
	})
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
