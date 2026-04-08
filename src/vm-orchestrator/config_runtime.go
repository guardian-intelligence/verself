package vmorchestrator

import vmrpc "github.com/forge-metal/vm-orchestrator/proto/v1"

func configToProto(cfg Config) *vmrpc.RuntimeConfig {
	return &vmrpc.RuntimeConfig{
		Pool:            cfg.Pool,
		GoldenZvol:      cfg.GoldenZvol,
		CiDataset:       cfg.CIDataset,
		KernelPath:      cfg.KernelPath,
		FirecrackerBin:  cfg.FirecrackerBin,
		JailerBin:       cfg.JailerBin,
		JailerRoot:      cfg.JailerRoot,
		JailerUid:       int32(cfg.JailerUID),
		JailerGid:       int32(cfg.JailerGID),
		Vcpus:           int32(cfg.VCPUs),
		MemoryMib:       int32(cfg.MemoryMiB),
		HostInterface:   cfg.HostInterface,
		GuestPoolCidr:   cfg.GuestPoolCIDR,
		NetworkLeaseDir: cfg.NetworkLeaseDir,
	}
}

func configFromProto(base Config, in *vmrpc.RuntimeConfig) Config {
	cfg := base
	if in == nil {
		return cfg
	}
	if in.GetPool() != "" {
		cfg.Pool = in.GetPool()
	}
	if in.GetGoldenZvol() != "" {
		cfg.GoldenZvol = in.GetGoldenZvol()
	}
	if in.GetCiDataset() != "" {
		cfg.CIDataset = in.GetCiDataset()
	}
	if in.GetKernelPath() != "" {
		cfg.KernelPath = in.GetKernelPath()
	}
	if in.GetFirecrackerBin() != "" {
		cfg.FirecrackerBin = in.GetFirecrackerBin()
	}
	if in.GetJailerBin() != "" {
		cfg.JailerBin = in.GetJailerBin()
	}
	if in.GetJailerRoot() != "" {
		cfg.JailerRoot = in.GetJailerRoot()
	}
	if in.GetJailerUid() > 0 {
		cfg.JailerUID = int(in.GetJailerUid())
	}
	if in.GetJailerGid() > 0 {
		cfg.JailerGID = int(in.GetJailerGid())
	}
	if in.GetVcpus() > 0 {
		cfg.VCPUs = int(in.GetVcpus())
	}
	if in.GetMemoryMib() > 0 {
		cfg.MemoryMiB = int(in.GetMemoryMib())
	}
	if in.GetHostInterface() != "" {
		cfg.HostInterface = in.GetHostInterface()
	}
	if in.GetGuestPoolCidr() != "" {
		cfg.GuestPoolCIDR = in.GetGuestPoolCidr()
	}
	if in.GetNetworkLeaseDir() != "" {
		cfg.NetworkLeaseDir = in.GetNetworkLeaseDir()
	}
	return cfg
}
