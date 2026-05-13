package jobs

import "fmt"

type KernelImageRef string

const KernelImageDefault KernelImageRef = "default"

type VMResources struct {
	VCPUs       uint32         `json:"vcpus"`
	MemoryMiB   uint32         `json:"memory_mib"`
	RootDiskGiB uint32         `json:"root_disk_gib"`
	KernelImage KernelImageRef `json:"kernel_image,omitempty"`
}

type VMResourceBounds struct {
	MinVCPUs       uint32 `json:"min_vcpus"`
	MaxVCPUs       uint32 `json:"max_vcpus"`
	MinMemoryMiB   uint32 `json:"min_memory_mib"`
	MaxMemoryMiB   uint32 `json:"max_memory_mib"`
	MinRootDiskGiB uint32 `json:"min_root_disk_gib"`
	MaxRootDiskGiB uint32 `json:"max_root_disk_gib"`
}

var DefaultBounds = VMResourceBounds{
	MinVCPUs:       1,
	MaxVCPUs:       16,
	MinMemoryMiB:   128,
	MaxMemoryMiB:   65536,
	MinRootDiskGiB: 1,
	MaxRootDiskGiB: 512,
}

var DefaultResources = VMResources{
	VCPUs:       4,
	MemoryMiB:   16384,
	RootDiskGiB: 80,
	KernelImage: KernelImageDefault,
}

type ValidationError struct {
	Field  string
	Reason string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("vmresources: %s: %s", e.Field, e.Reason)
}

func (r VMResources) Validate(bounds VMResourceBounds) error {
	if bounds == (VMResourceBounds{}) {
		bounds = DefaultBounds
	}
	r = r.Normalize()
	if r.VCPUs < bounds.MinVCPUs || r.VCPUs > bounds.MaxVCPUs {
		return &ValidationError{
			Field:  "vcpus",
			Reason: fmt.Sprintf("requested %d, bounds are [%d, %d]", r.VCPUs, bounds.MinVCPUs, bounds.MaxVCPUs),
		}
	}
	if r.MemoryMiB < bounds.MinMemoryMiB || r.MemoryMiB > bounds.MaxMemoryMiB {
		return &ValidationError{
			Field:  "memory_mib",
			Reason: fmt.Sprintf("requested %d, bounds are [%d, %d]", r.MemoryMiB, bounds.MinMemoryMiB, bounds.MaxMemoryMiB),
		}
	}
	if r.RootDiskGiB < bounds.MinRootDiskGiB || r.RootDiskGiB > bounds.MaxRootDiskGiB {
		return &ValidationError{
			Field:  "root_disk_gib",
			Reason: fmt.Sprintf("requested %d, bounds are [%d, %d]", r.RootDiskGiB, bounds.MinRootDiskGiB, bounds.MaxRootDiskGiB),
		}
	}
	return r.KernelImage.Validate()
}

func (k KernelImageRef) Validate() error {
	switch k {
	case "", KernelImageDefault:
		return nil
	default:
		return &ValidationError{
			Field:  "kernel_image",
			Reason: fmt.Sprintf("unknown kernel image %q, supported: [%q]", string(k), string(KernelImageDefault)),
		}
	}
}

func (r VMResources) Normalize() VMResources {
	if r.VCPUs == 0 {
		r.VCPUs = DefaultResources.VCPUs
	}
	if r.MemoryMiB == 0 {
		r.MemoryMiB = DefaultResources.MemoryMiB
	}
	if r.RootDiskGiB == 0 {
		r.RootDiskGiB = DefaultResources.RootDiskGiB
	}
	if r.KernelImage == "" {
		r.KernelImage = KernelImageDefault
	}
	return r
}
