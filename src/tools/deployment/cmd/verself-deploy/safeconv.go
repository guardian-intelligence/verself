package main

import (
	"fmt"
	"time"
)

const (
	maxInt64AsUint64 = uint64(1<<63 - 1)
	maxUint32AsInt64 = int64(1<<32 - 1)
	maxUint16AsInt   = int(^uint16(0))
)

func int64FromUint64(value uint64, field string) int64 {
	if value > maxInt64AsUint64 {
		panic(fmt.Sprintf("%s exceeds int64 range: %d", field, value))
	}
	return int64(value) // #nosec G115 -- value is checked against MaxInt64 above.
}

func durationMillis(value time.Duration, field string) uint32 {
	if value < 0 || value.Milliseconds() > maxUint32AsInt64 {
		panic(fmt.Sprintf("%s exceeds uint32 milliseconds range: %s", field, value))
	}
	return uint32(value.Milliseconds()) // #nosec G115 -- duration is checked against uint32 milliseconds above.
}

func uint16FromInt(value int, field string) uint16 {
	if value < 0 || value > maxUint16AsInt {
		panic(fmt.Sprintf("%s exceeds uint16 range: %d", field, value))
	}
	return uint16(value) // #nosec G115 -- value is checked against MaxUint16 above.
}

func uint64FromInt(value int, field string) uint64 {
	if value < 0 {
		panic(fmt.Sprintf("%s cannot be negative: %d", field, value))
	}
	return uint64(value) // #nosec G115 -- value is checked to be non-negative above.
}
