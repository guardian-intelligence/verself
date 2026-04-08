package workload

import (
	"fmt"
	"os"

	vmorchestrator "github.com/forge-metal/vm-orchestrator"
)

func BuildGuestJob(jobID string, manifest *Manifest, toolchain *Toolchain, installNeeded bool, warm bool, env map[string]string) vmorchestrator.JobConfig {
	repoRoot := "/workspace"

	runCommand := manifest.Run
	if warm {
		runCommand = manifest.ResolvedPrepare()
	}
	if toolchain != nil {
		runCommand = toolchain.ResolveCommand(runCommand)
	}

	job := vmorchestrator.JobConfig{
		JobID:      jobID,
		RunCommand: cloneStringSlice(runCommand),
		RunWorkDir: manifest.RepoWorkDir(),
		Services:   cloneStringSlice(manifest.Services),
		Env:        cloneStringMap(env),
	}
	switch resolvedProfile(manifest) {
	case RuntimeProfileNode:
		if installNeeded && toolchain != nil {
			job.PrepareCommand = toolchain.InstallCommand()
			job.PrepareWorkDir = repoRoot
		}
	}
	return job
}

func BuildJobEnv(manifest *Manifest) (map[string]string, error) {
	env := map[string]string{
		"CI": "true",
	}
	for _, name := range manifest.Env {
		value, ok := os.LookupEnv(name)
		if !ok {
			return nil, fmt.Errorf("required env %s is not set", name)
		}
		env[name] = value
	}
	return env, nil
}

func resolvedProfile(manifest *Manifest) RuntimeProfile {
	if manifest == nil || manifest.Profile == "" || manifest.Profile == RuntimeProfileAuto {
		return RuntimeProfileNode
	}
	return manifest.Profile
}

func cloneStringSlice(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	return append([]string(nil), values...)
}

func cloneStringMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]string, len(values))
	for key, value := range values {
		out[key] = value
	}
	return out
}
