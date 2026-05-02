package main

import (
	"fmt"
	"time"
)

const (
	maxInt64AsUint64   = uint64(1<<63 - 1)
	maxDurationSeconds = maxInt64AsUint64 / uint64(time.Second)
)

func durationFromSeconds(value uint64, field string) (time.Duration, error) {
	if value > maxDurationSeconds {
		return 0, fmt.Errorf("%s exceeds max duration seconds %d: %d", field, maxDurationSeconds, value)
	}
	return time.Duration(value) * time.Second, nil // #nosec G115 -- value is checked against the duration range above.
}

func uintptrFromFD(fd int, field string) (uintptr, error) {
	if fd < 0 {
		return 0, fmt.Errorf("%s is negative: %d", field, fd)
	}
	return uintptr(fd), nil // #nosec G115 -- Linux accept returned a non-negative file descriptor.
}
