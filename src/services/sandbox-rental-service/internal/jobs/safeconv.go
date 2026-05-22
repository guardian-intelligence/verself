package jobs

import (
	"fmt"
	"time"

	"github.com/verself/vm-orchestrator"
)

const (
	maxInt32AsInt64    = int64(1<<31 - 1)
	minInt32AsInt64    = -1 << 31
	maxInt64AsUint64   = uint64(1<<63 - 1)
	maxDurationSeconds = maxInt64AsUint64 / uint64(time.Second)
)

func mustInt64FromUint64(value uint64, field string) int64 {
	if value > maxInt64AsUint64 {
		panic(fmt.Sprintf("%s exceeds int64 range: %d", field, value))
	}
	return int64(value) // #nosec G115 -- value is checked against MaxInt64 above.
}

func int32FromUint32(value uint32, field string) int32 {
	if uint64(value) > uint64(maxInt32AsInt64) {
		panic(fmt.Sprintf("%s exceeds int32 range: %d", field, value))
	}
	return int32(value) // #nosec G115 -- value is checked against MaxInt32 above.
}

func uint64FromInt64(value int64, field string) uint64 {
	if value < 0 {
		panic(fmt.Sprintf("%s is negative: %d", field, value))
	}
	return uint64(value) // #nosec G115 -- value is checked as non-negative above.
}

func uint32FromInt32(value int32, field string) uint32 {
	if value < 0 {
		panic(fmt.Sprintf("%s is negative: %d", field, value))
	}
	return uint32(value) // #nosec G115 -- value is checked as non-negative above.
}

func int32FromInt(value int, field string) int32 {
	if int64(value) < minInt32AsInt64 || int64(value) > maxInt32AsInt64 {
		panic(fmt.Sprintf("%s exceeds int32 range: %d", field, value))
	}
	return int32(value) // #nosec G115 -- value is checked against the int32 range above.
}

func int32FromInt64(value int64, field string) int32 {
	if value < minInt32AsInt64 || value > maxInt32AsInt64 {
		panic(fmt.Sprintf("%s exceeds int32 range: %d", field, value))
	}
	return int32(value) // #nosec G115 -- value is checked against the int32 range above.
}

func durationFromSeconds(value uint64, field string) time.Duration {
	if value > maxDurationSeconds {
		panic(fmt.Sprintf("%s exceeds max duration seconds %d: %d", field, maxDurationSeconds, value))
	}
	return time.Duration(value) * time.Second // #nosec G115 -- value is checked against the duration range above.
}

func vmResourcesForLease(resources VMResources) vmorchestrator.VMResources {
	return vmorchestrator.VMResources{
		VCPUs:       resources.VCPUs,
		MemoryMiB:   resources.MemoryMiB,
		RootDiskGiB: resources.RootDiskGiB,
		KernelImage: vmorchestrator.KernelImageRef(resources.KernelImage),
	}
}
