package identity

import "fmt"

func uint32FromNonNegativeInt32(value int32, field string) uint32 {
	if value < 0 {
		panic(fmt.Sprintf("%s is negative: %d", field, value))
	}
	return uint32(value) // #nosec G115 -- value is checked as non-negative above.
}
