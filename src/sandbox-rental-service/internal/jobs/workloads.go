package jobs

import (
	"fmt"
	"strings"

	vmorchestrator "github.com/forge-metal/vm-orchestrator"
	"github.com/forge-metal/workload"
	"github.com/google/uuid"
)

// Surgical note: repo-backed VM work prefers `.forge-metal/ci.toml` when
// present, but imported repos no longer require it. In the absence of that
// file, the shared workload package infers a minimal Node-oriented contract
// from package metadata so bootstrap can proceed while the runner-native path
// is still under construction.

type RepoTarget struct {
	Repo    string
	RepoURL string
}

type RepoExecSpec struct {
	JobID string
	RepoTarget
	Ref string
}

type WarmGoldenSpec struct {
	JobID string
	RepoTarget
	DefaultBranch string
}

type PreparedRepoExec struct {
	Inspection *workload.Inspection
	Request    vmorchestrator.RepoExecRequest
}

type PreparedWarmGolden struct {
	Inspection *workload.Inspection
	Request    vmorchestrator.WarmGoldenRequest
}

func PrepareRepoExec(spec RepoExecSpec) (*PreparedRepoExec, error) {
	if err := validateRepoExecSpec(spec); err != nil {
		return nil, err
	}
	inspection, err := workload.InspectRepoRef(strings.TrimSpace(spec.RepoURL), strings.TrimSpace(spec.Ref))
	if err != nil {
		return nil, err
	}
	request, err := BuildRepoExecRequest(spec, inspection)
	if err != nil {
		workload.CleanupInspection(inspection.Path)
		return nil, err
	}
	return &PreparedRepoExec{
		Inspection: inspection,
		Request:    request,
	}, nil
}

func PrepareWarmGolden(spec WarmGoldenSpec) (*PreparedWarmGolden, error) {
	if err := validateWarmGoldenSpec(spec); err != nil {
		return nil, err
	}
	inspection, err := workload.InspectRepoDefaultBranch(strings.TrimSpace(spec.RepoURL), defaultBranch(spec.DefaultBranch))
	if err != nil {
		return nil, err
	}
	request, err := BuildWarmGoldenRequest(spec, inspection)
	if err != nil {
		workload.CleanupInspection(inspection.Path)
		return nil, err
	}
	return &PreparedWarmGolden{
		Inspection: inspection,
		Request:    request,
	}, nil
}

func (p *PreparedRepoExec) Cleanup() {
	if p == nil || p.Inspection == nil {
		return
	}
	workload.CleanupInspection(p.Inspection.Path)
}

func (p *PreparedWarmGolden) Cleanup() {
	if p == nil || p.Inspection == nil {
		return
	}
	workload.CleanupInspection(p.Inspection.Path)
}

func BuildRepoExecRequest(spec RepoExecSpec, inspection *workload.Inspection) (vmorchestrator.RepoExecRequest, error) {
	if err := validateRepoExecSpec(spec); err != nil {
		return vmorchestrator.RepoExecRequest{}, err
	}
	if err := validateInspection(inspection); err != nil {
		return vmorchestrator.RepoExecRequest{}, err
	}
	env, err := workload.BuildJobEnv(inspection.Manifest)
	if err != nil {
		return vmorchestrator.RepoExecRequest{}, err
	}

	return vmorchestrator.RepoExecRequest{
		// RuntimeConfig intentionally stays zero-valued here. The daemon already
		// owns privileged host config and merges request overrides over that base.
		Repo:            strings.TrimSpace(spec.Repo),
		RepoURL:         strings.TrimSpace(spec.RepoURL),
		Ref:             strings.TrimSpace(spec.Ref),
		JobTemplate:     workload.BuildGuestJob(spec.JobID, inspection.Manifest, inspection.Toolchain, true, false, env),
		LockfileRelPath: inspection.Toolchain.LockfileRelPath,
	}, nil
}

func BuildWarmGoldenRequest(spec WarmGoldenSpec, inspection *workload.Inspection) (vmorchestrator.WarmGoldenRequest, error) {
	if err := validateWarmGoldenSpec(spec); err != nil {
		return vmorchestrator.WarmGoldenRequest{}, err
	}
	if err := validateInspection(inspection); err != nil {
		return vmorchestrator.WarmGoldenRequest{}, err
	}
	env, err := workload.BuildJobEnv(inspection.Manifest)
	if err != nil {
		return vmorchestrator.WarmGoldenRequest{}, err
	}

	return vmorchestrator.WarmGoldenRequest{
		// RuntimeConfig intentionally stays zero-valued here. The daemon already
		// owns privileged host config and merges request overrides over that base.
		Repo:            strings.TrimSpace(spec.Repo),
		RepoURL:         strings.TrimSpace(spec.RepoURL),
		DefaultBranch:   defaultBranch(spec.DefaultBranch),
		Job:             workload.BuildGuestJob(spec.JobID, inspection.Manifest, inspection.Toolchain, true, true, env),
		LockfileRelPath: inspection.Toolchain.LockfileRelPath,
	}, nil
}

func validateRepoExecSpec(spec RepoExecSpec) error {
	if err := validateUUID(spec.JobID); err != nil {
		return fmt.Errorf("repo exec job_id: %w", err)
	}
	if err := validateRepoTarget(spec.RepoTarget); err != nil {
		return err
	}
	if strings.TrimSpace(spec.Ref) == "" {
		return fmt.Errorf("repo exec ref is required")
	}
	return nil
}

func validateWarmGoldenSpec(spec WarmGoldenSpec) error {
	if err := validateUUID(spec.JobID); err != nil {
		return fmt.Errorf("warm golden job_id: %w", err)
	}
	if err := validateRepoTarget(spec.RepoTarget); err != nil {
		return err
	}
	return nil
}

func validateRepoTarget(target RepoTarget) error {
	if strings.TrimSpace(target.Repo) == "" {
		return fmt.Errorf("repo is required")
	}
	if strings.TrimSpace(target.RepoURL) == "" {
		return fmt.Errorf("repo_url is required")
	}
	if err := validateGitCloneURLField("repo_url", target.RepoURL); err != nil {
		return err
	}
	return nil
}

func validateUUID(value string) error {
	if _, err := uuid.Parse(strings.TrimSpace(value)); err != nil {
		return err
	}
	return nil
}

func validateInspection(inspection *workload.Inspection) error {
	switch {
	case inspection == nil:
		return fmt.Errorf("inspection is required")
	case inspection.Manifest == nil:
		return fmt.Errorf("inspection manifest is required")
	case inspection.Toolchain == nil:
		return fmt.Errorf("inspection toolchain is required")
	default:
		return nil
	}
}

func defaultBranch(branch string) string {
	branch = strings.TrimSpace(branch)
	if branch == "" {
		return "main"
	}
	return branch
}
