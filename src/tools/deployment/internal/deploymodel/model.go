package deploymodel

import (
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
)

type ArtifactDelivery struct {
	Kind                 string            `json:"kind"`
	Bucket               string            `json:"bucket"`
	GetterSourcePrefix   string            `json:"getter_source_prefix"`
	GetterOptions        map[string]string `json:"getter_options"`
	GetterCredentials    Credentials       `json:"getter_credentials"`
	PublisherCredentials Credentials       `json:"publisher_credentials"`
}

type Credentials struct {
	Source             string `json:"source"`
	EnvironmentFile    string `json:"environment_file"`
	AccessKeyIDEnv     string `json:"access_key_id_env"`
	SecretAccessKeyEnv string `json:"secret_access_key_env"`
	SessionTokenEnv    string `json:"session_token_env"`
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
