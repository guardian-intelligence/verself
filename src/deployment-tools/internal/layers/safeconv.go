package layers

import (
	"fmt"
	"time"
)

const maxUint32AsInt64 = int64(1<<32 - 1)

func durationMillis(value time.Duration, field string) uint32 {
	if value < 0 || value.Milliseconds() > maxUint32AsInt64 {
		panic(fmt.Sprintf("%s exceeds uint32 milliseconds range: %s", field, value))
	}
	return uint32(value.Milliseconds()) // #nosec G115 -- duration is checked against uint32 milliseconds above.
}

func uint32FromInt(value int, field string) uint32 {
	if value < 0 || int64(value) > maxUint32AsInt64 {
		panic(fmt.Sprintf("%s exceeds uint32 range: %d", field, value))
	}
	return uint32(value) // #nosec G115 -- value is checked against the uint32 range above.
}
