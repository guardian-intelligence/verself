package api

import "fmt"

const (
	maxInt32AsInt64 = 1<<31 - 1
	minInt32AsInt64 = -1 << 31
)

func int32FromInt64(value int64, field string) (int32, error) {
	if value < minInt32AsInt64 || value > maxInt32AsInt64 {
		return 0, fmt.Errorf("%s exceeds int32 range: %d", field, value)
	}
	return int32(value), nil // #nosec G115 -- value is checked against the int32 range above.
}
