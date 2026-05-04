package recurring

import "fmt"

const (
	maxInt32AsUint32 = uint32(1<<31 - 1)
	maxInt64AsUint64 = uint64(1<<63 - 1)
)

func int64FromUint64(value uint64, field string) int64 {
	if value > maxInt64AsUint64 {
		panic(fmt.Sprintf("%s exceeds int64 range: %d", field, value))
	}
	return int64(value) // #nosec G115 -- value is checked against MaxInt64 above.
}

func int32FromUint32(value uint32, field string) int32 {
	if value > maxInt32AsUint32 {
		panic(fmt.Sprintf("%s exceeds int32 range: %d", field, value))
	}
	return int32(value) // #nosec G115 -- value is checked against MaxInt32 above.
}

func uint32FromInt32(value int32, field string) uint32 {
	if value < 0 {
		panic(fmt.Sprintf("%s is negative: %d", field, value))
	}
	return uint32(value) // #nosec G115 -- value is checked as non-negative above.
}
