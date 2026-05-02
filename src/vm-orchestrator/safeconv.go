package vmorchestrator

import (
	"fmt"
	"strconv"
	"time"
)

const (
	maxInt32AsInt64    = int64(1<<31 - 1)
	minInt32AsInt64    = -1 << 31
	maxUint32AsUint64  = uint64(1<<32 - 1)
	maxInt64AsUint64   = uint64(1<<63 - 1)
	maxDurationSeconds = maxInt64AsUint64 / uint64(time.Second)
)

func intFromFD(fd uintptr, field string) (int, error) {
	if strconv.IntSize == 32 && fd > 1<<31-1 {
		return 0, fmt.Errorf("%s exceeds int range: %d", field, fd)
	}
	return int(fd), nil // #nosec G115 -- fd is range-checked for 32-bit; on 64-bit uintptr and int have the same width.
}

func int64FromUint64(value uint64, field string) (int64, error) {
	if value > maxInt64AsUint64 {
		return 0, fmt.Errorf("%s exceeds int64 range: %d", field, value)
	}
	return int64(value), nil // #nosec G115 -- value is checked against MaxInt64 above.
}

func uint64FromNonNegativeInt64(value int64, field string) (uint64, error) {
	if value < 0 {
		return 0, fmt.Errorf("%s is negative: %d", field, value)
	}
	return uint64(value), nil // #nosec G115 -- value is checked as non-negative above.
}

func durationFromSeconds(value uint64, field string) (time.Duration, error) {
	if value > maxDurationSeconds {
		return 0, fmt.Errorf("%s exceeds max duration seconds %d: %d", field, maxDurationSeconds, value)
	}
	return time.Duration(value) * time.Second, nil // #nosec G115 -- value is checked against the duration range above.
}

func uint32FromInt(value int, field string) (uint32, error) {
	if value < 0 || int64(value) > int64(maxUint32AsUint64) {
		return 0, fmt.Errorf("%s exceeds uint32 range: %d", field, value)
	}
	return uint32(value), nil // #nosec G115 -- value is checked against the uint32 range above.
}

func int32FromInt(value int, field string) (int32, error) {
	if int64(value) < minInt32AsInt64 || int64(value) > maxInt32AsInt64 {
		return 0, fmt.Errorf("%s exceeds int32 range: %d", field, value)
	}
	return int32(value), nil // #nosec G115 -- value is checked against the int32 range above.
}
