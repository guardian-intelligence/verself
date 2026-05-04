package api

import "fmt"

const (
	maxInt32AsInt  = 1<<31 - 1
	minInt32AsInt  = -1 << 31
	maxUint16AsInt = 1<<16 - 1
)

func int32FromInt(value int, field string) int32 {
	if value < minInt32AsInt || value > maxInt32AsInt {
		panic(fmt.Sprintf("%s exceeds int32 range: %d", field, value))
	}
	return int32(value) // #nosec G115 -- value is checked against the int32 range above.
}

func uint16FromInt(value int, field string) uint16 {
	if value < 0 || value > maxUint16AsInt {
		panic(fmt.Sprintf("%s exceeds uint16 range: %d", field, value))
	}
	return uint16(value) // #nosec G115 -- value is checked against the uint16 range above.
}
