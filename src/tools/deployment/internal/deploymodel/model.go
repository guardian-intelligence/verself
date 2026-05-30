// Package deploymodel contains the shared immutable artifact and Nomad submit
// value types used by the deploy controller.
package deploymodel

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"path/filepath"
)

type ArtifactDelivery struct {
	Bucket               string            `json:"bucket"`
	GetterSourcePrefix   string            `json:"getter_source_prefix"`
	GetterOptions        map[string]string `json:"getter_options"`
	GetterCredentials    Credentials       `json:"getter_credentials"`
	PublisherCredentials Credentials       `json:"publisher_credentials"`
	Origin               Origin            `json:"origin"`
}

type Origin struct {
	Scheme       string `json:"scheme"`
	Hostname     string `json:"hostname"`
	Port         int    `json:"port"`
	CABundlePath string `json:"ca_bundle_path"`
}

type Credentials struct {
	EnvironmentFile    string `json:"environment_file"`
	AccessKeyIDEnv     string `json:"access_key_id_env"`
	SecretAccessKeyEnv string `json:"secret_access_key_env"`
}

type Artifact struct {
	Output        string            `json:"output"`
	LocalPath     string            `json:"local_path,omitempty"`
	SHA256        string            `json:"sha256"`
	Bucket        string            `json:"bucket"`
	Key           string            `json:"key"`
	GetterSource  string            `json:"getter_source"`
	GetterOptions map[string]string `json:"getter_options,omitempty"`
}

type NomadJob struct {
	JobID              string             `json:"job_id"`
	Component          string             `json:"component"`
	DependsOn          []string           `json:"depends_on,omitempty"`
	ArtifactOutputs    []string           `json:"artifact_outputs,omitempty"`
	PostDeployCanaries []PostDeployCanary `json:"post_deploy_canaries,omitempty"`
	InputSHA256        string             `json:"input_sha256,omitempty"`
	SpecSHA256         string             `json:"spec_sha256"`
	ArtifactSHA256     string             `json:"artifact_sha256"`
	Spec               json.RawMessage    `json:"spec"`
}

type PostDeployCanary struct {
	Label   string            `json:"label"`
	Target  string            `json:"target"`
	Kind    string            `json:"kind"`
	Size    string            `json:"size"`
	Args    []string          `json:"args"`
	Env     map[string]string `json:"env,omitempty"`
	Timeout string            `json:"timeout,omitempty"`
}

func (a Artifact) ResolveLocalPath(repoRoot string) string {
	if filepath.IsAbs(a.LocalPath) {
		return a.LocalPath
	}
	return filepath.Join(repoRoot, a.LocalPath)
}

func SHA256(body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}
