package ci

import "github.com/forge-metal/workload"

// Surgical note: this file is now a temporary adapter so the deprecated
// platform CI package consumes the shared workload contract instead of carrying
// its own divergent copy. Delete the adapter once the remaining callers move.
const ManifestRelPath = workload.ManifestRelPath

type RuntimeProfile = workload.RuntimeProfile

const (
	RuntimeProfileAuto = workload.RuntimeProfileAuto
	RuntimeProfileNode = workload.RuntimeProfileNode
)

type Manifest = workload.Manifest

func LoadManifest(repoRoot string) (*Manifest, error) {
	return workload.LoadManifest(repoRoot)
}
