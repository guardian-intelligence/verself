package main

import (
	"math"
	"time"
)

func durationMillis(d time.Duration) uint32 {
	if d <= 0 {
		return 0
	}
	ms := d.Milliseconds()
	if ms > math.MaxUint32 {
		return math.MaxUint32
	}
	return uint32(ms)
}

func uint16FromInt(v int) uint16 {
	if v <= 0 {
		return 0
	}
	if v > math.MaxUint16 {
		return math.MaxUint16
	}
	return uint16(v)
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
