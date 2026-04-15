package apiwire

import (
	"fmt"
	"strings"
)

// VMResources is the canonical wire type for a customer-requested VM shape.
// It threads unchanged from the sandbox-rental-service HTTP intake, through
// the scheduler, into vm-orchestrator's LeaseSpec, and onto every billing
// window and trace span attached to the lease.
//
// Numeric fields are uint32 (not DecimalUint64) because the bounds enforced
// by VMResourceBounds keep them well inside JavaScript-safe range. See
// src/apiwire/docs/wire-contracts.md for the numeric-safety rubric.
type VMResources struct {
	VCPUs       uint32         `json:"vcpus" minimum:"1" maximum:"128" doc:"Number of vCPUs exposed to the guest."`
	MemoryMiB   uint32         `json:"memory_mib" minimum:"128" maximum:"524288" doc:"Guest RAM in MiB."`
	RootDiskGiB uint32         `json:"root_disk_gib" minimum:"1" maximum:"2048" doc:"Root disk quota in GiB; enforced on the per-lease ZFS clone via refquota/refreservation."`
	KernelImage KernelImageRef `json:"kernel_image,omitempty" doc:"Named guest kernel image. Defaults to \"default\"."`
}

// KernelImageRef is a tagged string naming a guest kernel image known to
// vm-orchestrator. Only "default" is accepted today; future work will ship
// additional images (e.g. "tiny", "debug") without a DTO migration.
type KernelImageRef string

const (
	KernelImageDefault KernelImageRef = "default"
)

// VMResourceBounds caps VMResources for an organization. Persisted in
// sandbox-rental-service's vm_resource_bounds table, cached per-request.
// Operators edit these via the organization console; defaults below seed
// any org that does not yet have a row.
type VMResourceBounds struct {
	MinVCPUs       uint32 `json:"min_vcpus"`
	MaxVCPUs       uint32 `json:"max_vcpus"`
	MinMemoryMiB   uint32 `json:"min_memory_mib"`
	MaxMemoryMiB   uint32 `json:"max_memory_mib"`
	MinRootDiskGiB uint32 `json:"min_root_disk_gib"`
	MaxRootDiskGiB uint32 `json:"max_root_disk_gib"`
}

// DefaultBounds are applied when an org has no explicit row in
// vm_resource_bounds. Ceiling reflects the "16 vCPU, 64 GiB, 512 GiB"
// envelope of a single bare-metal box's guest pool.
var DefaultBounds = VMResourceBounds{
	MinVCPUs:       1,
	MaxVCPUs:       16,
	MinMemoryMiB:   128,
	MaxMemoryMiB:   65536,
	MinRootDiskGiB: 1,
	MaxRootDiskGiB: 512,
}

// DefaultResources is applied when a caller omits VMResources. Phase 2
// drops this to 1 vCPU / 1 GiB / 10 GiB to cut boot cost on the hot path
// for ad-hoc workloads. Callers wanting more must ask for more.
var DefaultResources = VMResources{
	VCPUs:       1,
	MemoryMiB:   1024,
	RootDiskGiB: 10,
	KernelImage: KernelImageDefault,
}

// DefaultKernelCmdlineBase is the shared prefix of every guest kernel
// cmdline, joined with spaces at boot time. Each flag is annotated with
// its kernel-parameters.txt description so regressions are easy to audit.
//
// Phase 2 adds boot-time optimization flags here. pci=off is safe because
// Firecracker uses virtio-mmio by default (FC appends it automatically
// when --enable-pci is absent — we set it explicitly for clarity); acpi=off
// is deliberately omitted (version-dependent, zero upstream FC test
// coverage, requires CONFIG_X86_MPPARSE=y in the guest kernel build);
// i8042.nokbd is deliberately omitted (Firecracker's SendCtrlAltDel path
// in src/vmm/src/devices/legacy/i8042.rs uses the emulated keyboard, so
// disabling it would break graceful shutdown).
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
	// tsc=reliable [x86] mark tsc clocksource as reliable; disables
	// clocksource verification and stability checks at bootup. Used in
	// virtualized environments.
	"tsc=reliable",
	// random.trust_cpu=on [KNL,EARLY] Trust CPU RNG to seed kernel RNG.
	// Typically the compiled default (CONFIG_RANDOM_TRUST_CPU=y), forced
	// on here regardless of Kconfig.
	"random.trust_cpu=on",
	// 8250.nr_uarts=0 [SERIAL] Register zero 8250 UARTs; saves ~20ms of
	// probe time. We use ttyS0 via the Firecracker serial device, not
	// the 8250 driver, so this is safe.
	"8250.nr_uarts=0",
	// pci=off [X86] Don't probe for the PCI bus. Firecracker appends this
	// automatically when --enable-pci is absent; we set it explicitly so
	// the cmdline is self-documenting.
	"pci=off",
	// i8042.noaux [HW] Don't check for auxiliary (mouse) port.
	"i8042.noaux",
	// i8042.nopnp [HW] Don't use ACPIPnP / PnPBIOS to discover KBD/AUX.
	"i8042.nopnp",
	// i8042.dumbkbd [HW] Don't blink the kbd LEDs.
	"i8042.dumbkbd",
	// no_timer_check [X86,APIC] Disable IO-APIC timer IRQ probe that
	// occasionally misfires under hypervisors.
	"no_timer_check",
}

// ValidationError identifies which field was out of bounds and why.
// Callers at service boundaries wrap this into their RFC 7807 response.
type ValidationError struct {
	Field  string
	Reason string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("vmresources: %s: %s", e.Field, e.Reason)
}

// Validate applies VMResourceBounds to a (possibly partially-populated)
// VMResources. It returns a *ValidationError the caller can type-assert
// to surface a tagged error back to the customer.
//
// Zero-valued fields are replaced with DefaultResources values before
// bound checking; this lets callers pass only the fields they care about
// without inventing a builder pattern.
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

// Validate restricts KernelImageRef to the currently-supported set.
// Adding a new kernel image is a one-line change here plus a matching
// file on the host; the DTO and migrations do not move.
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

// Normalize returns a VMResources with zero-valued fields replaced by
// DefaultResources. Callers use this after Validate to get a canonical
// shape to persist.
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

// ReservationShape is the stable string encoding of a VMResources tuple
// used on billing_windows.reservation_shape and metering rows. Callers
// parsing this back out should not — treat it as opaque.
func (r VMResources) ReservationShape() string {
	r = r.Normalize()
	return fmt.Sprintf("vcpu=%d;mem_mib=%d;disk_gib=%d;kernel=%s", r.VCPUs, r.MemoryMiB, r.RootDiskGiB, r.KernelImage)
}

// RenderCmdline joins the base flag list into a single kernel cmdline
// string. Extra flags appended to the base are separated with spaces.
// The returned string is what goes into Firecracker's boot-source PUT.
func RenderCmdline(base []string, extras ...string) string {
	all := make([]string, 0, len(base)+len(extras))
	all = append(all, base...)
	all = append(all, extras...)
	return strings.Join(all, " ")
}
