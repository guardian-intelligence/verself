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
	JobID          string          `json:"job_id"`
	SpecSHA256     string          `json:"spec_sha256"`
	ArtifactSHA256 string          `json:"artifact_sha256"`
	Spec           json.RawMessage `json:"spec"`
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
