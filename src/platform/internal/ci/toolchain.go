package ci

import "github.com/forge-metal/workload"

type PackageManager = workload.PackageManager

const (
	PackageManagerNPM  = workload.PackageManagerNPM
	PackageManagerPNPM = workload.PackageManagerPNPM
	PackageManagerBun  = workload.PackageManagerBun
)

type Toolchain = workload.Toolchain

func DetectToolchain(repoRoot string) (*Toolchain, error) {
	return workload.DetectToolchain(repoRoot)
}

func ComputeFileSHA256(path string) (string, error) {
	return workload.ComputeFileSHA256(path)
}
