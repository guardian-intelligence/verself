package deploymodel

import (
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
)

type ArtifactDelivery struct {
	Kind   string `json:"kind"`
	Bucket string `json:"bucket"`
}

type Artifact struct {
	Output       string `json:"output"`
	LocalPath    string `json:"local_path,omitempty"`
	SHA256       string `json:"sha256"`
	Bucket       string `json:"bucket"`
	Key          string `json:"key"`
	GetterSource string `json:"getter_source"`
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
