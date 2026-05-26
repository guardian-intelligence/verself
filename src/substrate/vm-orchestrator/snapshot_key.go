package vmorchestrator

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/verself/vm-orchestrator/vmproto"
)

const (
	GoldenVMSnapshotKeySchema         = "fc-precontrol-chrony-v5"
	GoldenVMRestoreHookVersion        = "lease-init+host-clock-first+chrony-restart-v3"
	GoldenVMBeforeSnapshotHookVersion = "before_golden_snapshot.stop_chronyd_v3"
	snapshotKeySchema                 = GoldenVMSnapshotKeySchema
	snapshotNetworkModel              = "network-overrides+lease-init-v1"
	snapshotRestoreHookVersion        = GoldenVMRestoreHookVersion
	snapshotBeforeSnapshotHookVersion = GoldenVMBeforeSnapshotHookVersion
	snapshotIfaceID                   = "eth0"
	snapshotGuestMAC                  = "06:00:00:00:00:01"
)

var errDatasetOriginMissing = errors.New("zfs dataset origin missing")

type ActivationMode string

const (
	ActivationModeColdBoot        ActivationMode = "cold_boot"
	ActivationModeSnapshotRestore ActivationMode = "snapshot_restore"
)

type SnapshotKey struct {
	value string
}

func (k SnapshotKey) String() string { return k.value }

type snapshotKeyMaterial struct {
	Schema       string                  `json:"schema"`
	DiskLayout   snapshotDiskLayoutKey   `json:"disk_layout"`
	RuntimeABI   snapshotRuntimeABIKey   `json:"runtime_abi"`
	NetworkModel snapshotNetworkModelKey `json:"network_model"`
}

type snapshotDiskLayoutKey struct {
	Root   snapshotRootDiskKey `json:"root"`
	Drives []snapshotDriveKey  `json:"drives,omitempty"`
}

type snapshotRootDiskKey struct {
	SubstrateRef      string `json:"substrate_ref"`
	SubstrateSnapshot string `json:"substrate_snapshot"`
	SubstrateGUID     string `json:"substrate_guid"`
	RootDiskGiB       uint32 `json:"root_disk_gib"`
}

type snapshotDriveKey struct {
	DriveID        string   `json:"drive_id"`
	Name           string   `json:"name"`
	SourceRef      string   `json:"source_ref"`
	SourceSnapshot string   `json:"source_snapshot,omitempty"`
	SourceGUID     string   `json:"source_guid,omitempty"`
	MountPath      string   `json:"mount_path"`
	BindPaths      []string `json:"bind_paths,omitempty"`
	FSType         string   `json:"fs_type"`
	ReadOnly       bool     `json:"read_only"`
	Required       bool     `json:"required"`
	Empty          bool     `json:"empty"`
}

type snapshotRuntimeABIKey struct {
	FirecrackerSHA256  string `json:"firecracker_sha256"`
	JailerSHA256       string `json:"jailer_sha256"`
	KernelSHA256       string `json:"kernel_sha256"`
	KernelCmdline      string `json:"kernel_cmdline"`
	VMProtoVersion     int    `json:"vmproto_version"`
	RestoreHook        string `json:"restore_hook"`
	BeforeSnapshotHook string `json:"before_snapshot_hook"`
	VCPUs              uint32 `json:"vcpus"`
	MemoryMiB          uint32 `json:"memory_mib"`
}

type snapshotNetworkModelKey struct {
	Model   string `json:"model"`
	IfaceID string `json:"iface_id"`
}

func (o *Orchestrator) buildSnapshotKey(ctx context.Context, rootDataset string, spec LeaseSpec, mounts []preparedFilesystemMount, bootArgs string) (SnapshotKey, error) {
	rootOrigin, err := o.datasetOriginSnapshot(ctx, rootDataset)
	if err != nil {
		return SnapshotKey{}, fmt.Errorf("snapshot key root origin: %w", err)
	}
	rootGUID, err := o.snapshotGUID(ctx, rootOrigin)
	if err != nil {
		return SnapshotKey{}, fmt.Errorf("snapshot key root guid: %w", err)
	}
	fcDigest, err := fileSHA256(o.cfg.FirecrackerBin)
	if err != nil {
		return SnapshotKey{}, fmt.Errorf("snapshot key firecracker digest: %w", err)
	}
	jailerDigest, err := fileSHA256(o.cfg.JailerBin)
	if err != nil {
		return SnapshotKey{}, fmt.Errorf("snapshot key jailer digest: %w", err)
	}
	kernelDigest, err := fileSHA256(o.cfg.KernelPath)
	if err != nil {
		return SnapshotKey{}, fmt.Errorf("snapshot key kernel digest: %w", err)
	}
	material := snapshotKeyMaterial{
		Schema: snapshotKeySchema,
		DiskLayout: snapshotDiskLayoutKey{
			Root: snapshotRootDiskKey{
				SubstrateRef:      o.cfg.DefaultSubstrateRef,
				SubstrateSnapshot: rootOrigin,
				SubstrateGUID:     rootGUID,
				RootDiskGiB:       spec.Resources.RootDiskGiB,
			},
			Drives: make([]snapshotDriveKey, 0, len(mounts)),
		},
		RuntimeABI: snapshotRuntimeABIKey{
			FirecrackerSHA256:  fcDigest,
			JailerSHA256:       jailerDigest,
			KernelSHA256:       kernelDigest,
			KernelCmdline:      bootArgs,
			VMProtoVersion:     vmproto.ProtocolVersion,
			RestoreHook:        snapshotRestoreHookVersion,
			BeforeSnapshotHook: snapshotBeforeSnapshotHookVersion,
			VCPUs:              spec.Resources.VCPUs,
			MemoryMiB:          spec.Resources.MemoryMiB,
		},
		NetworkModel: snapshotNetworkModelKey{
			Model:   snapshotNetworkModel,
			IfaceID: snapshotIfaceID,
		},
	}
	for _, mount := range mounts {
		drive, err := o.snapshotDriveKey(ctx, mount)
		if err != nil {
			return SnapshotKey{}, err
		}
		material.DiskLayout.Drives = append(material.DiskLayout.Drives, drive)
	}
	data, err := json.Marshal(material)
	if err != nil {
		return SnapshotKey{}, fmt.Errorf("marshal snapshot key material: %w", err)
	}
	sum := sha256.Sum256(data)
	return SnapshotKey{value: hex.EncodeToString(sum[:])}, nil
}

func (o *Orchestrator) snapshotDriveKey(ctx context.Context, mount preparedFilesystemMount) (snapshotDriveKey, error) {
	key := snapshotDriveKey{
		DriveID:   mount.DriveID,
		Name:      mount.Spec.Name,
		SourceRef: mount.Spec.SourceRef,
		MountPath: mount.Spec.MountPath,
		BindPaths: append([]string(nil), mount.Spec.BindPaths...),
		FSType:    mount.Spec.FSType,
		ReadOnly:  mount.Spec.ReadOnly,
		Required:  mount.Spec.Required,
		Empty:     strings.TrimSpace(mount.Spec.SourceRef) == "",
	}
	sort.Strings(key.BindPaths)
	if key.Empty {
		return key, nil
	}
	origin, err := o.datasetOriginSnapshot(ctx, mount.Dataset)
	if err != nil {
		if errors.Is(err, errDatasetOriginMissing) {
			key.Empty = true
			return key, nil
		}
		return snapshotDriveKey{}, fmt.Errorf("snapshot key mount origin %s: %w", mount.Dataset, err)
	}
	key.SourceSnapshot = origin
	guid, err := o.snapshotGUID(ctx, key.SourceSnapshot)
	if err != nil {
		return snapshotDriveKey{}, fmt.Errorf("snapshot key source guid %s: %w", key.SourceSnapshot, err)
	}
	key.SourceGUID = guid
	return key, nil
}

func (o *Orchestrator) datasetOriginSnapshot(ctx context.Context, dataset string) (string, error) {
	if strings.TrimSpace(dataset) == "" || strings.Contains(dataset, "@") {
		return "", fmt.Errorf("dataset is invalid: %s", dataset)
	}
	origin, err := o.ops.ZFSGetProperty(ctx, dataset, "origin")
	if err != nil {
		return "", err
	}
	origin = strings.TrimSpace(origin)
	if origin == "" || origin == "-" {
		return "", fmt.Errorf("dataset %s has empty origin: %w", dataset, errDatasetOriginMissing)
	}
	return origin, nil
}

func (o *Orchestrator) snapshotGUID(ctx context.Context, snapshot string) (string, error) {
	if strings.TrimSpace(snapshot) == "" {
		return "", fmt.Errorf("snapshot ref is required")
	}
	guid, err := o.ops.ZFSGetProperty(ctx, snapshot, "guid")
	if err != nil {
		return "", err
	}
	guid = strings.TrimSpace(guid)
	if guid == "" || guid == "-" {
		return "", fmt.Errorf("snapshot %s has empty guid", snapshot)
	}
	return guid, nil
}

func fileSHA256(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", fmt.Errorf("path is required")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", path, err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}
