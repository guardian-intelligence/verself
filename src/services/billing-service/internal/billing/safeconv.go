package billing

import "fmt"

const (
	maxInt32AsInt64  = int64(1<<31 - 1)
	minInt32AsInt64  = -1 << 31
	maxUint16AsInt64 = int64(1<<16 - 1)
	maxUint32AsInt64 = int64(1<<32 - 1)
	maxInt64AsUint64 = uint64(1<<63 - 1)
)

// SQL stores billing quantities in signed columns; domain DTOs use unsigned
// counters. A failed conversion means persisted billing state has crossed a
// schema/domain boundary that should be impossible.
func checkedInt64FromUint64(value uint64, field string) int64 {
	if value > maxInt64AsUint64 {
		panic(fmt.Sprintf("%s exceeds int64 range: %d", field, value))
	}
	return int64(value) // #nosec G115 -- value is checked against MaxInt64 above.
}

func checkedUint64FromInt64(value int64, field string) uint64 {
	if value < 0 {
		panic(fmt.Sprintf("%s is negative: %d", field, value))
	}
	return uint64(value) // #nosec G115 -- value is checked as non-negative above.
}

func checkedUint32FromInt64(value int64, field string) uint32 {
	if value < 0 || value > maxUint32AsInt64 {
		panic(fmt.Sprintf("%s exceeds uint32 range: %d", field, value))
	}
	return uint32(value) // #nosec G115 -- value is checked against the uint32 range above.
}

func checkedUint32FromInt32(value int32, field string) uint32 {
	if value < 0 {
		panic(fmt.Sprintf("%s is negative: %d", field, value))
	}
	return uint32(value) // #nosec G115 -- value is checked as non-negative above.
}

func checkedUint16FromInt32(value int32, field string) uint16 {
	if value < 0 || int64(value) > maxUint16AsInt64 {
		panic(fmt.Sprintf("%s exceeds uint16 range: %d", field, value))
	}
	return uint16(value) // #nosec G115 -- value is checked against the uint16 range above.
}

func checkedInt32FromInt(value int, field string) int32 {
	if int64(value) < minInt32AsInt64 || int64(value) > maxInt32AsInt64 {
		panic(fmt.Sprintf("%s exceeds int32 range: %d", field, value))
	}
	return int32(value) // #nosec G115 -- value is checked against the int32 range above.
}
