package githubintegration

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strings"

	sandboxrentalclient "github.com/verself/sandbox-rental-service/client"
)

const (
	trustClassBranch  = "github_branch"
	trustClassPR      = "github_pr"
	trustClassForkPR  = "github_pr_fork"
	trustClassUnknown = "github_unknown"
)

type githubJobShape struct {
	Provider             string   `json:"provider"`
	ProviderRepositoryID int64    `json:"provider_repository_id"`
	RepositoryFullName   string   `json:"repository_full_name"`
	WorkflowPath         string   `json:"workflow_path"`
	WorkflowName         string   `json:"workflow_name"`
	JobName              string   `json:"job_name"`
	MatrixKey            string   `json:"matrix_key"`
	RunnerClass          string   `json:"runner_class"`
	RunnerLabels         []string `json:"runner_labels"`
	CacheManifestSHA256  string   `json:"cache_manifest_sha256"`
	TrustClass           string   `json:"trust_class"`
}

type githubJobShapeRecord struct {
	Shape         githubJobShape
	JobShapeID    string
	CanonicalJSON []byte
	LabelsJSON    []byte
}

func buildGitHubJobShape(event workflowJobWebhook, workflow workflowObservation, runnerClass string, cacheManifestSHA string) (githubJobShapeRecord, error) {
	labels := normalizedLabels(event.WorkflowJob.Labels)
	trustClass := classifyTrust(workflow)
	shape := githubJobShape{
		Provider:             providerGitHub,
		ProviderRepositoryID: event.Repository.ID,
		RepositoryFullName:   strings.TrimSpace(event.Repository.FullName),
		WorkflowPath:         strings.TrimSpace(workflow.WorkflowPath),
		WorkflowName:         strings.TrimSpace(event.WorkflowJob.WorkflowName),
		JobName:              strings.TrimSpace(event.WorkflowJob.Name),
		MatrixKey:            "",
		RunnerClass:          strings.TrimSpace(runnerClass),
		RunnerLabels:         labels,
		CacheManifestSHA256:  strings.TrimSpace(cacheManifestSHA),
		TrustClass:           trustClass,
	}
	if shape.WorkflowName == "" {
		shape.WorkflowName = strings.TrimSpace(workflow.WorkflowPath)
	}
	canonical, err := json.Marshal(shape)
	if err != nil {
		return githubJobShapeRecord{}, err
	}
	sum := sha256.Sum256(canonical)
	labelsJSON, err := json.Marshal(labels)
	if err != nil {
		return githubJobShapeRecord{}, err
	}
	return githubJobShapeRecord{
		Shape:         shape,
		JobShapeID:    "ghjob_" + hex.EncodeToString(sum[:]),
		CanonicalJSON: canonical,
		LabelsJSON:    labelsJSON,
	}, nil
}

func normalizedLabels(labels []string) []string {
	out := make([]string, 0, len(labels))
	seen := map[string]struct{}{}
	for _, label := range labels {
		label = strings.TrimSpace(label)
		if label == "" {
			continue
		}
		if _, ok := seen[label]; ok {
			continue
		}
		seen[label] = struct{}{}
		out = append(out, label)
	}
	sort.Strings(out)
	return out
}

func classifyTrust(workflow workflowObservation) string {
	event := strings.TrimSpace(workflow.EventName)
	switch event {
	case "pull_request", "pull_request_target":
		if workflow.HeadRepositoryFullName != "" && workflow.RepositoryFullName != "" && workflow.HeadRepositoryFullName != workflow.RepositoryFullName {
			return trustClassForkPR
		}
		return trustClassPR
	case "push", "workflow_dispatch", "schedule":
		return trustClassBranch
	default:
		if workflow.PullRequestNumber > 0 {
			if workflow.HeadRepositoryFullName != "" && workflow.RepositoryFullName != "" && workflow.HeadRepositoryFullName != workflow.RepositoryFullName {
				return trustClassForkPR
			}
			return trustClassPR
		}
		if workflow.HeadSHA != "" || workflow.HeadBranch != "" {
			return trustClassBranch
		}
		return trustClassUnknown
	}
}

func cacheManifestContentSHA(manifest *sandboxrentalclient.RunnerCacheManifest) string {
	if manifest == nil {
		return ""
	}
	sum := sha256.Sum256([]byte(manifest.ContentBase64))
	return hex.EncodeToString(sum[:])
}
