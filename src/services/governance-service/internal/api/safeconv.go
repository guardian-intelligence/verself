package api

import "fmt"

const (
	maxInt32AsInt = 1<<31 - 1
	minInt32AsInt = -1 << 31
)

func int32FromInt(value int, field string) int32 {
	if value < minInt32AsInt || value > maxInt32AsInt {
		panic(fmt.Sprintf("%s exceeds int32 range: %d", field, value))
	}
	return int32(value) // #nosec G115 -- value is checked against the int32 range above.
}
