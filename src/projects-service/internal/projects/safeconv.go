package projects

import "fmt"

const (
	maxInt32AsInt  = 1<<31 - 1
	minInt32AsInt  = -1 << 31
	maxInt64AsUint = uint64(1<<63 - 1)
)

func int64FromUint64(value uint64, field string) int64 {
	if value > maxInt64AsUint {
		panic(fmt.Sprintf("%s exceeds int64 range: %d", field, value))
	}
	return int64(value) // #nosec G115 -- value is checked against MaxInt64 above.
}

func int32FromInt(value int, field string) int32 {
	if value < minInt32AsInt || value > maxInt32AsInt {
		panic(fmt.Sprintf("%s exceeds int32 range: %d", field, value))
	}
	return int32(value) // #nosec G115 -- value is checked against the int32 range above.
}
