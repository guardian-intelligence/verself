package zfs

import "fmt"

const maxInt64AsUint64 = uint64(1<<63 - 1)

func int64FromUint64(value uint64, field string) (int64, error) {
	if value > maxInt64AsUint64 {
		return 0, fmt.Errorf("%s exceeds int64 range: %d", field, value)
	}
	return int64(value), nil // #nosec G115 -- value is checked against MaxInt64 above.
}
