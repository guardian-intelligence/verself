package api

import "fmt"

const (
	maxInt32AsInt64  = int64(1<<31 - 1)
	minInt32AsInt64  = -1 << 31
	maxInt64AsUint64 = uint64(1<<63 - 1)
)

func int32FromInt(value int, field string) int32 {
	if int64(value) < minInt32AsInt64 || int64(value) > maxInt32AsInt64 {
		panic(fmt.Sprintf("%s exceeds int32 range: %d", field, value))
	}
	return int32(value) // #nosec G115 -- value is checked against the int32 range above.
}

func uint64FromInt64(value int64, field string) uint64 {
	if value < 0 {
		panic(fmt.Sprintf("%s is negative: %d", field, value))
	}
	return uint64(value) // #nosec G115 -- value is checked as non-negative above.
}

func int64FromUint64(value uint64, field string) int64 {
	if value > maxInt64AsUint64 {
		panic(fmt.Sprintf("%s exceeds int64 range: %d", field, value))
	}
	return int64(value) // #nosec G115 -- value is checked against MaxInt64 above.
}
