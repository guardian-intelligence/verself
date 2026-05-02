package source

import "fmt"

func int64FromUint64(value uint64, field string) int64 {
	if value > maxPostgresBigint {
		panic(fmt.Sprintf("%s exceeds int64 range: %d", field, value))
	}
	return int64(value) // #nosec G115 -- value is checked against the PostgreSQL bigint range above.
}
