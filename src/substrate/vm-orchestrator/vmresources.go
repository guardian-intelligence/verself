package vmorchestrator

import (
	"fmt"
	"strings"
)

// VMResources is the host-side VM shape accepted by the lease API.
type VMResources struct {
	VCPUs       uint32         `json:"vcpus" minimum:"1" maximum:"128" doc:"Number of vCPUs exposed to the guest."`
	MemoryMiB   uint32         `json:"memory_mib" minimum:"128" maximum:"524288" doc:"Guest RAM in MiB."`
	RootDiskGiB uint32         `json:"root_disk_gib" minimum:"1" maximum:"2048" doc:"Guest root zvol size in GiB."`
	KernelImage KernelImageRef `json:"kernel_image,omitempty" doc:"Named guest kernel image. Defaults to \"default\"."`
}

type KernelImageRef string

const (
	KernelImageDefault KernelImageRef = "default"
)

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

var DefaultKernelCmdlineBase = []string{
	"root=/dev/vda",
	"rw",
	"console=ttyS0",
	"reboot=k",
	"panic=1",
	"init=/sbin/init",
	// quiet [KNL,EARLY] Disable most log messages.
	"quiet",
	// loglevel=1 [KNL,EARLY] Only emergency messages to the console.
	"loglevel=1",
	// tsc=reliable [x86] mark tsc clocksource as reliable; disables clocksource verification.
	"tsc=reliable",
	// random.trust_cpu=on [KNL,EARLY] Trust CPU RNG to seed kernel RNG.
	"random.trust_cpu=on",
	// 8250.nr_uarts=0 [SERIAL] Register zero 8250 UARTs; Firecracker serial uses ttyS0.
	"8250.nr_uarts=0",
	// pci=off [X86] Don't probe for the PCI bus.
	"pci=off",
	// i8042.noaux [HW] Don't check for auxiliary mouse ports.
	"i8042.noaux",
	// i8042.nopnp [HW] Don't use ACPIPnP / PnPBIOS to discover KBD/AUX.
	"i8042.nopnp",
	// i8042.dumbkbd [HW] Don't blink the kbd LEDs.
	"i8042.dumbkbd",
	// no_timer_check [X86,APIC] Disable IO-APIC timer IRQ probe under virtualization.
	"no_timer_check",
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
	if r.VCPUs == 0 {
		r.VCPUs = DefaultResources.VCPUs
	}
	if r.MemoryMiB == 0 {
		r.MemoryMiB = DefaultResources.MemoryMiB
	}
	if r.RootDiskGiB == 0 {
		r.RootDiskGiB = DefaultResources.RootDiskGiB
	}
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
	if err := r.KernelImage.Validate(); err != nil {
		return err
	}
	return nil
}

func (k KernelImageRef) Validate() error {
	switch k {
	case "", KernelImageDefault:
		return nil
	}
	return &ValidationError{
		Field:  "kernel_image",
		Reason: fmt.Sprintf("unknown kernel image %q, supported: [%q]", string(k), string(KernelImageDefault)),
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

func (r VMResources) ReservationShape() string {
	r = r.Normalize()
	return fmt.Sprintf("vcpu=%d;mem_mib=%d;disk_gib=%d;kernel=%s", r.VCPUs, r.MemoryMiB, r.RootDiskGiB, r.KernelImage)
}

func RenderCmdline(base []string, extras ...string) string {
	all := make([]string, 0, len(base)+len(extras))
	all = append(all, base...)
	all = append(all, extras...)
	return strings.Join(all, " ")
}
