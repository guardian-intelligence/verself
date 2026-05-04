package api

import "strconv"

func fmtUint(value uint64) string {
	return strconv.FormatUint(value, 10)
}
