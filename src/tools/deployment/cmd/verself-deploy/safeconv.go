package main

import (
	"math"
)

func int64FromUint64(value uint64, _ string) int64 {
	if value > math.MaxInt64 {
		return math.MaxInt64
	}
	return int64(value)
}

func truncateError(err error) string {
	if err == nil {
		return ""
	}
	return truncateErrorString(err.Error())
}

func truncateErrorString(msg string) string {
	const max = 4000
	if len(msg) <= max {
		return msg
	}
	return msg[:max]
}
