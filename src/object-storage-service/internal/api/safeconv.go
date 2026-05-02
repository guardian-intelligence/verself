package api

import "fmt"

const maxInt64AsUint64 = uint64(1<<63 - 1)

func int64FromUint64(value uint64, field string) int64 {
	if value > maxInt64AsUint64 {
		panic(fmt.Sprintf("%s exceeds int64 range: %d", field, value))
	}
	return int64(value) // #nosec G115 -- value is checked against MaxInt64 above.
}

func uint64FromInt64(value int64, field string) uint64 {
	if value < 0 {
		panic(fmt.Sprintf("%s is negative: %d", field, value))
	}
	return uint64(value) // #nosec G115 -- value is checked as non-negative above.
}
