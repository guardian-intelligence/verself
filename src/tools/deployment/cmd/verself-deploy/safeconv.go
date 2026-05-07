package main

import (
	"math"
	"time"
)

func int64FromUint64(value uint64, _ string) int64 {
	if value > math.MaxInt64 {
		return math.MaxInt64
	}
	return int64(value)
}

func uint16FromInt(value int, _ string) uint16 {
	if value < 0 {
		return 0
	}
	if value > math.MaxUint16 {
		return math.MaxUint16
	}
	return uint16(value)
}

func uint32FromDuration(value time.Duration) uint32 {
	ms := value.Milliseconds()
	if ms < 0 {
		return 0
	}
	if ms > math.MaxUint32 {
		return math.MaxUint32
	}
	return uint32(ms)
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
