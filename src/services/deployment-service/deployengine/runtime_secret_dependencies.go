package deployengine

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/verself/deployment-service/deploycontract"
	"github.com/verself/deployment-service/internal/nomadclient"
)

const defaultRuntimeSecretProducerReadyTimeout = 10 * time.Minute
const openBaoJobID = "openbao"

type runtimeSecretDeploymentPlan struct {
	jobs             []nomadJob
	readinessReasons map[string][]string
}

func planRuntimeSecretDeployment(repoRoot string, jobs []nomadJob) (runtimeSecretDeploymentPlan, error) {
	paths, err := collectRuntimeSecretDeclarationFiles(repoRoot)
	if err != nil {
		return runtimeSecretDeploymentPlan{}, err
	}
	dependencies, err := deploycontract.OpenBaoRuntimeSecretDependenciesFromFiles(paths)
	if err != nil {
		return runtimeSecretDeploymentPlan{}, err
	}
	return orderNomadJobsByRuntimeSecretDependencies(jobs, dependencies)
}

func collectRuntimeSecretDeclarationFiles(repoRoot string) ([]string, error) {
	repoRoot = strings.TrimSpace(repoRoot)
	if repoRoot == "" {
		return nil, errors.New("repo root is required")
	}
	roots := []string{
		filepath.Join(repoRoot, "src", "infrastructure-components"),
		filepath.Join(repoRoot, "src", "integrations"),
		filepath.Join(repoRoot, "src", "services"),
		filepath.Join(repoRoot, "src", "viteplus-monorepo", "apps"),
	}
	paths := []string{}
	for _, root := range roots {
		if _, err := os.Stat(root); errors.Is(err, fs.ErrNotExist) {
			continue
		} else if err != nil {
			return nil, fmt.Errorf("stat %s: %w", root, err)
		}
		if err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() || filepath.Base(path) != "runtime-secrets.yml" || filepath.Base(filepath.Dir(path)) != "deploy" {
				return nil
			}
			paths = append(paths, path)
			return nil
		}); err != nil {
			return nil, fmt.Errorf("walk %s: %w", root, err)
		}
	}
	sort.Strings(paths)
	return paths, nil
}

func orderNomadJobsByRuntimeSecretDependencies(jobs []nomadJob, dependencies []deploycontract.OpenBaoRuntimeSecretDependency) (runtimeSecretDeploymentPlan, error) {
	byID := map[string]nomadJob{}
	originalIndex := map[string]int{}
	for index, job := range jobs {
		if job.JobID == "" {
			return runtimeSecretDeploymentPlan{}, errors.New("nomad job id is required")
		}
		if _, exists := byID[job.JobID]; exists {
			return runtimeSecretDeploymentPlan{}, fmt.Errorf("duplicate Nomad job id %s", job.JobID)
		}
		byID[job.JobID] = job
		originalIndex[job.JobID] = index
	}
	edges := map[string]map[string]bool{}
	indegree := map[string]int{}
	readinessReasons := map[string][]string{}
	for _, job := range jobs {
		indegree[job.JobID] = 0
	}
	addEdge := func(producer, reader, reason string) {
		if edges[producer] == nil {
			edges[producer] = map[string]bool{}
		}
		if !edges[producer][reader] {
			edges[producer][reader] = true
			indegree[reader]++
		}
		readinessReasons[producer] = append(readinessReasons[producer], reason)
	}
	for _, dependency := range dependencies {
		producer := strings.TrimSpace(dependency.ProducerJobID)
		reader := strings.TrimSpace(dependency.ReaderJobID)
		if producer == "" || reader == "" || producer == reader {
			continue
		}
		_, readerParticipates := byID[reader]
		if !readerParticipates {
			continue
		}
		if _, producerParticipates := byID[producer]; !producerParticipates {
			return runtimeSecretDeploymentPlan{}, fmt.Errorf("runtime secret %s for job %s is produced by %s, but producer job is not in this deployment", dependency.SecretName, reader, producer)
		}
		addEdge(producer, reader, fmt.Sprintf("runtime secret %s for %s", dependency.SecretName, reader))
	}
	if _, openBaoParticipates := byID[openBaoJobID]; openBaoParticipates {
		for _, job := range jobs {
			if job.JobID == openBaoJobID || !job.UsesNomadVault {
				continue
			}
			addEdge(openBaoJobID, job.JobID, fmt.Sprintf("Nomad Vault integration for %s", job.JobID))
		}
	} else {
		for _, job := range jobs {
			if job.UsesNomadVault {
				return runtimeSecretDeploymentPlan{}, fmt.Errorf("%s uses Nomad Vault integration, but %s is not in this deployment", job.JobID, openBaoJobID)
			}
		}
	}

	ready := make([]string, 0, len(jobs))
	for _, job := range jobs {
		if indegree[job.JobID] == 0 {
			ready = append(ready, job.JobID)
		}
	}
	sort.Slice(ready, func(i, j int) bool {
		return originalIndex[ready[i]] < originalIndex[ready[j]]
	})
	ordered := make([]nomadJob, 0, len(jobs))
	for len(ready) > 0 {
		jobID := ready[0]
		ready = ready[1:]
		ordered = append(ordered, byID[jobID])
		children := make([]string, 0, len(edges[jobID]))
		for child := range edges[jobID] {
			children = append(children, child)
		}
		sort.Slice(children, func(i, j int) bool {
			return originalIndex[children[i]] < originalIndex[children[j]]
		})
		for _, child := range children {
			indegree[child]--
			if indegree[child] == 0 {
				ready = append(ready, child)
			}
		}
		sort.Slice(ready, func(i, j int) bool {
			return originalIndex[ready[i]] < originalIndex[ready[j]]
		})
	}
	if len(ordered) != len(jobs) {
		blocked := make([]string, 0)
		for jobID, degree := range indegree {
			if degree > 0 {
				blocked = append(blocked, jobID)
			}
		}
		sort.Strings(blocked)
		return runtimeSecretDeploymentPlan{}, fmt.Errorf("runtime secret producer graph contains a cycle involving jobs: %s", strings.Join(blocked, ", "))
	}
	return runtimeSecretDeploymentPlan{
		jobs:             ordered,
		readinessReasons: readinessReasons,
	}, nil
}

func waitForRuntimeSecretProducer(ctx context.Context, exec execution, client *nomadclient.Client, job nomadJob) error {
	if job.Job == nil || job.Job.Type == nil {
		return fmt.Errorf("%s: Nomad job type is required for runtime secret producer readiness", job.JobID)
	}
	timeout := exec.RuntimeSecretProducerReadyTimeout
	if timeout <= 0 {
		timeout = defaultRuntimeSecretProducerReadyTimeout
	}
	switch strings.TrimSpace(*job.Job.Type) {
	case "batch":
		if err := client.WaitForBatchComplete(ctx, job.JobID, timeout); err != nil {
			return fmt.Errorf("%s: runtime secret producer batch did not complete: %w", job.JobID, err)
		}
	case "service":
		if err := client.WaitForServiceDeployment(ctx, job.JobID, timeout); err != nil {
			return fmt.Errorf("%s: runtime secret producer service did not become healthy: %w", job.JobID, err)
		}
		if job.JobID == openBaoJobID {
			if err := client.WaitForVaultIntegration(ctx, timeout); err != nil {
				return fmt.Errorf("%s: Nomad Vault integration did not become ready: %w", job.JobID, err)
			}
		}
	default:
		return fmt.Errorf("%s: runtime secret producer job type %q is not supported", job.JobID, strings.TrimSpace(*job.Job.Type))
	}
	return nil
}
