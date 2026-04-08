package vmorchestrator

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

const (
	guestTelemetryPort     = 10790
	guestTelemetryFrameLen = 128
	guestTelemetryMagic    = 0x46505600
	guestTelemetryVersion  = 1
)

type TelemetryFrameKind uint16

const (
	TelemetryFrameKindHello  TelemetryFrameKind = 1
	TelemetryFrameKindSample TelemetryFrameKind = 2
)

var (
	ErrTelemetryShortFrame   = errors.New("telemetry frame shorter than 128 bytes")
	ErrTelemetryInvalidMagic = errors.New("telemetry frame has invalid magic")
	ErrTelemetryVersion      = errors.New("telemetry frame has invalid version")
	ErrTelemetryKind         = errors.New("telemetry frame has invalid kind")
)

type telemetryHeader struct {
	Kind   TelemetryFrameKind
	Seq    uint32
	Flags  uint32
	MonoNS uint64
	WallNS uint64
}

func ReadTelemetryFrame(r io.Reader) (TelemetryEvent, error) {
	var buf [guestTelemetryFrameLen]byte
	if _, err := io.ReadFull(r, buf[:]); err != nil {
		return TelemetryEvent{}, err
	}
	return DecodeTelemetryFrame(buf)
}

func DecodeTelemetryFrame(buf [guestTelemetryFrameLen]byte) (TelemetryEvent, error) {
	header, err := decodeTelemetryHeader(buf)
	if err != nil {
		return TelemetryEvent{}, err
	}

	event := TelemetryEvent{}
	switch header.Kind {
	case TelemetryFrameKindHello:
		event.Hello = &TelemetryHello{
			Seq:        header.Seq,
			Flags:      header.Flags,
			MonoNS:     header.MonoNS,
			WallNS:     header.WallNS,
			BootID:     formatUUID(buf[32:48]),
			MemTotalKB: readU64(buf[:], 48),
		}
	case TelemetryFrameKindSample:
		event.Sample = &TelemetrySample{
			Seq:            header.Seq,
			Flags:          header.Flags,
			MonoNS:         header.MonoNS,
			WallNS:         header.WallNS,
			CPUUserTicks:   readU64(buf[:], 32),
			CPUSystemTicks: readU64(buf[:], 40),
			CPUIdleTicks:   readU64(buf[:], 48),
			Load1Centis:    readU32(buf[:], 56),
			Load5Centis:    readU32(buf[:], 60),
			Load15Centis:   readU32(buf[:], 64),
			ProcsRunning:   readU16(buf[:], 68),
			ProcsBlocked:   readU16(buf[:], 70),
			MemAvailableKB: readU64(buf[:], 72),
			IOReadBytes:    readU64(buf[:], 80),
			IOWriteBytes:   readU64(buf[:], 88),
			NetRXBytes:     readU64(buf[:], 96),
			NetTXBytes:     readU64(buf[:], 104),
			PSICPUPct100:   readU16(buf[:], 112),
			PSIMemPct100:   readU16(buf[:], 114),
			PSIIOPct100:    readU16(buf[:], 116),
		}
	default:
		return TelemetryEvent{}, fmt.Errorf("%w: %d", ErrTelemetryKind, header.Kind)
	}
	return event, nil
}

func decodeTelemetryHeader(buf [guestTelemetryFrameLen]byte) (telemetryHeader, error) {
	if readU32(buf[:], 0) != guestTelemetryMagic {
		return telemetryHeader{}, ErrTelemetryInvalidMagic
	}
	if readU16(buf[:], 4) != guestTelemetryVersion {
		return telemetryHeader{}, ErrTelemetryVersion
	}

	kind := TelemetryFrameKind(readU16(buf[:], 6))
	if kind != TelemetryFrameKindHello && kind != TelemetryFrameKindSample {
		return telemetryHeader{}, fmt.Errorf("%w: %d", ErrTelemetryKind, kind)
	}

	return telemetryHeader{
		Kind:   kind,
		Seq:    readU32(buf[:], 8),
		Flags:  readU32(buf[:], 12),
		MonoNS: readU64(buf[:], 16),
		WallNS: readU64(buf[:], 24),
	}, nil
}

func readU16(buf []byte, offset int) uint16 {
	return binary.LittleEndian.Uint16(buf[offset : offset+2])
}

func readU32(buf []byte, offset int) uint32 {
	return binary.LittleEndian.Uint32(buf[offset : offset+4])
}

func readU64(buf []byte, offset int) uint64 {
	return binary.LittleEndian.Uint64(buf[offset : offset+8])
}

func formatUUID(raw []byte) string {
	if len(raw) != 16 {
		return ""
	}
	return fmt.Sprintf(
		"%02x%02x%02x%02x-%02x%02x-%02x%02x-%02x%02x-%02x%02x%02x%02x%02x%02x",
		raw[0], raw[1], raw[2], raw[3],
		raw[4], raw[5],
		raw[6], raw[7],
		raw[8], raw[9],
		raw[10], raw[11], raw[12], raw[13], raw[14], raw[15],
	)
}
