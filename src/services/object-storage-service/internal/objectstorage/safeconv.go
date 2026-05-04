package objectstorage

import (
	"fmt"
	"time"
)

const (
	maxUint16AsInt    = 1<<16 - 1
	maxUint32AsInt    = 1<<32 - 1
	maxUint32AsInt64  = int64(1<<32 - 1)
	maxUint32Duration = time.Duration(maxUint32AsInt64) * time.Millisecond
)

func uint16FromStatus(value int, field string) uint16 {
	if value < 0 || value > maxUint16AsInt {
		panic(fmt.Sprintf("%s exceeds uint16 range: %d", field, value))
	}
	return uint16(value) // #nosec G115 -- value is checked against the uint16 range above.
}

func uint32FromIndex(value int, field string) uint32 {
	if value < 0 || value > maxUint32AsInt {
		panic(fmt.Sprintf("%s exceeds uint32 range: %d", field, value))
	}
	return uint32(value) // #nosec G115 -- value is checked against the uint32 range above.
}

func uint64FromNonNegativeInt64(value int64, field string) uint64 {
	if value < 0 {
		panic(fmt.Sprintf("%s is negative: %d", field, value))
	}
	return uint64(value) // #nosec G115 -- value is checked as non-negative above.
}

func latencyMillis(value time.Duration, field string) uint32 {
	if value < 0 || value > maxUint32Duration {
		panic(fmt.Sprintf("%s exceeds uint32 milliseconds range: %s", field, value))
	}
	return uint32(value.Milliseconds()) // #nosec G115 -- duration is checked against uint32 milliseconds above.
}
