package api

import "fmt"

const maxUint16AsInt = 1<<16 - 1

func uint16FromInt(value int, field string) uint16 {
	if value < 0 || value > maxUint16AsInt {
		panic(fmt.Sprintf("%s exceeds uint16 range: %d", field, value))
	}
	return uint16(value) // #nosec G115 -- value is checked against the uint16 range above.
}
