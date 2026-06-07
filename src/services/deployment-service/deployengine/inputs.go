package deployengine

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/hashicorp/nomad/api"

	"github.com/verself/deployment-service/internal/siteinject"
	"github.com/verself/deployment-service/siteconfig"
)

type deployInputs struct {
	NomadAddr string
	SiteModel siteconfig.Model
	JobPaths  []string
}

type siteConfig struct {
	NomadAddr string
}

type rawSiteConfig struct {
	NomadAddr string `json:"nomad_addr"`
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
	return &deployInputs{
		NomadAddr: cfg.NomadAddr,
		SiteModel: model,
		JobPaths:  cleanNomadJobPaths(exec.NomadJobPaths),
	}, nil
}

func loadSiteConfig(repoRoot, site string) (siteConfig, error) {
	path := filepath.Join(repoRoot, "src", "sites", site, "site.json")
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

func cleanNomadJobPaths(paths []string) []string {
	cleaned := make([]string, 0, len(paths))
	seen := map[string]bool{}
	for _, path := range paths {
		path = strings.TrimSpace(path)
		if path == "" || seen[path] {
			continue
		}
		seen[path] = true
		cleaned = append(cleaned, path)
	}
	return cleaned
}

func prepareNomadJobsForSite(ctx context.Context, parser authoredNomadSpecParser, repoRoot string, model siteconfig.Model, jobPaths []string, taskUserResolver TaskUserResolver) ([]nomadJob, error) {
	seenJobIDs := map[string]bool{}
	jobs := make([]nomadJob, 0, len(jobPaths))
	for _, jobPath := range jobPaths {
		specPath := resolveWorkspacePath(repoRoot, jobPath)
		job, err := loadAuthoredNomadSpec(ctx, parser, specPath)
		if err != nil {
			return nil, err
		}
		if model.Site != "" {
			if err := siteinject.Apply(job, model); err != nil {
				return nil, fmt.Errorf("%s: %w", jobPath, err)
			}
		}
		if err := applyTaskSecretTemplateOwnership(ctx, job, taskUserResolver); err != nil {
			return nil, fmt.Errorf("%s: %w", jobPath, err)
		}
		parsedID := ""
		if job.ID != nil {
			parsedID = *job.ID
		}
		if strings.TrimSpace(parsedID) == "" {
			return nil, fmt.Errorf("%s: authored Nomad job ID is required", jobPath)
		}
		if seenJobIDs[parsedID] {
			return nil, fmt.Errorf("duplicate Nomad job ID %s", parsedID)
		}
		seenJobIDs[parsedID] = true
		jobs = append(jobs, nomadJob{
			Component: parsedID,
			JobID:     parsedID,
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
