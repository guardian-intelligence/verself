package vmproto

import "fmt"

const maxUint32AsInt64 = int64(1<<32 - 1)

func uint32FromInt(value int, field string) (uint32, error) {
	if value < 0 || int64(value) > maxUint32AsInt64 {
		return 0, fmt.Errorf("%s exceeds uint32 range: %d", field, value)
	}
	return uint32(value), nil // #nosec G115 -- value is checked against the uint32 range above.
}
