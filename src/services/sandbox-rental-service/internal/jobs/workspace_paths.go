package jobs

import (
	"fmt"
	"path"
	"strings"
)

const (
	githubRunnerRuntimeDir     = "/tmp/verself-actions-runner"
	githubRunnerDurableWorkDir = "/workspace/.verself/actions-runner/_work"
)

func githubWorkspacePath(repositoryFullName string) (string, error) {
	if _, err := githubRunnerWorkspacePath(repositoryFullName); err != nil {
		return "", err
	}
	return githubRunnerDurableWorkDir, nil
}

func githubRunnerWorkspacePath(repositoryFullName string) (string, error) {
	owner, repo, ok := strings.Cut(strings.TrimSpace(repositoryFullName), "/")
	if !ok || owner == "" || repo == "" {
		return "", fmt.Errorf("github repository must be owner/name")
	}
	if strings.ContainsAny(repo, "\x00/\r\n\t ") || strings.Contains(repo, "..") {
		return "", fmt.Errorf("invalid github repository name")
	}
	return path.Join(githubRunnerRuntimeDir, githubRunnerWorkFolder, repo, repo), nil
}
