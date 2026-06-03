package deploymodel

import (
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
)

type Artifact struct {
	Output       string `json:"output"`
	LocalPath    string `json:"local_path,omitempty"`
	SHA256       string `json:"sha256"`
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
