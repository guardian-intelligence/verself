package vmorchestrator

import (
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

type telemetryVectors struct {
	Vectors map[string]struct {
		Type   string `json:"type"`
		Hex    string `json:"hex"`
		Fields struct {
			Seq            uint32 `json:"seq"`
			Flags          uint32 `json:"flags"`
			MonoNS         string `json:"mono_ns"`
			WallNS         string `json:"wall_ns"`
			BootID         string `json:"boot_id"`
			MemTotalKB     string `json:"mem_total_kb"`
			CPUUserTicks   string `json:"cpu_user_ticks"`
			CPUSystemTicks string `json:"cpu_system_ticks"`
			CPUIdleTicks   string `json:"cpu_idle_ticks"`
			Load1Centis    uint32 `json:"load1_centis"`
			Load5Centis    uint32 `json:"load5_centis"`
			Load15Centis   uint32 `json:"load15_centis"`
			ProcsRunning   uint16 `json:"procs_running"`
			ProcsBlocked   uint16 `json:"procs_blocked"`
			MemAvailableKB string `json:"mem_available_kb"`
			IOReadBytes    string `json:"io_read_bytes"`
			IOWriteBytes   string `json:"io_write_bytes"`
			NetRXBytes     string `json:"net_rx_bytes"`
			NetTXBytes     string `json:"net_tx_bytes"`
			PSICPUPct100   uint16 `json:"psi_cpu_pct100"`
			PSIMemPct100   uint16 `json:"psi_mem_pct100"`
			PSIIOPct100    uint16 `json:"psi_io_pct100"`
		} `json:"fields"`
	} `json:"vectors"`
}

func TestDecodeTelemetryVectors(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile(filepath.Join("..", "vm-guest-telemetry", "protocol", "vectors.json"))
	if err != nil {
		t.Fatalf("read vectors: %v", err)
	}

	var vectors telemetryVectors
	if err := json.Unmarshal(data, &vectors); err != nil {
		t.Fatalf("unmarshal vectors: %v", err)
	}

	for name, vector := range vectors.Vectors {
		name := name
		vector := vector
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			wire, err := hex.DecodeString(vector.Hex)
			if err != nil {
				t.Fatalf("decode hex: %v", err)
			}
			if len(wire) != guestTelemetryFrameLen {
				t.Fatalf("frame len = %d, want %d", len(wire), guestTelemetryFrameLen)
			}

			var frame [guestTelemetryFrameLen]byte
			copy(frame[:], wire)

			event, err := DecodeTelemetryFrame(frame)
			if err != nil {
				t.Fatalf("decode telemetry frame: %v", err)
			}

			switch vector.Type {
			case "hello":
				if event.Hello == nil {
					t.Fatalf("hello frame decoded without hello payload")
				}
				assertEqualU32(t, "seq", event.Hello.Seq, vector.Fields.Seq)
				assertEqualU32(t, "flags", event.Hello.Flags, vector.Fields.Flags)
				assertEqualU64String(t, "mono_ns", event.Hello.MonoNS, vector.Fields.MonoNS)
				assertEqualU64String(t, "wall_ns", event.Hello.WallNS, vector.Fields.WallNS)
				if event.Hello.BootID != vector.Fields.BootID {
					t.Fatalf("boot_id = %q, want %q", event.Hello.BootID, vector.Fields.BootID)
				}
				assertEqualU64String(t, "mem_total_kb", event.Hello.MemTotalKB, vector.Fields.MemTotalKB)
			case "sample":
				if event.Sample == nil {
					t.Fatalf("sample frame decoded without sample payload")
				}
				assertEqualU32(t, "seq", event.Sample.Seq, vector.Fields.Seq)
				assertEqualU32(t, "flags", event.Sample.Flags, vector.Fields.Flags)
				assertEqualU64String(t, "mono_ns", event.Sample.MonoNS, vector.Fields.MonoNS)
				assertEqualU64String(t, "wall_ns", event.Sample.WallNS, vector.Fields.WallNS)
				assertEqualU64String(t, "cpu_user_ticks", event.Sample.CPUUserTicks, vector.Fields.CPUUserTicks)
				assertEqualU64String(t, "cpu_system_ticks", event.Sample.CPUSystemTicks, vector.Fields.CPUSystemTicks)
				assertEqualU64String(t, "cpu_idle_ticks", event.Sample.CPUIdleTicks, vector.Fields.CPUIdleTicks)
				assertEqualU32(t, "load1_centis", event.Sample.Load1Centis, vector.Fields.Load1Centis)
				assertEqualU32(t, "load5_centis", event.Sample.Load5Centis, vector.Fields.Load5Centis)
				assertEqualU32(t, "load15_centis", event.Sample.Load15Centis, vector.Fields.Load15Centis)
				assertEqualU16(t, "procs_running", event.Sample.ProcsRunning, vector.Fields.ProcsRunning)
				assertEqualU16(t, "procs_blocked", event.Sample.ProcsBlocked, vector.Fields.ProcsBlocked)
				assertEqualU64String(t, "mem_available_kb", event.Sample.MemAvailableKB, vector.Fields.MemAvailableKB)
				assertEqualU64String(t, "io_read_bytes", event.Sample.IOReadBytes, vector.Fields.IOReadBytes)
				assertEqualU64String(t, "io_write_bytes", event.Sample.IOWriteBytes, vector.Fields.IOWriteBytes)
				assertEqualU64String(t, "net_rx_bytes", event.Sample.NetRXBytes, vector.Fields.NetRXBytes)
				assertEqualU64String(t, "net_tx_bytes", event.Sample.NetTXBytes, vector.Fields.NetTXBytes)
				assertEqualU16(t, "psi_cpu_pct100", event.Sample.PSICPUPct100, vector.Fields.PSICPUPct100)
				assertEqualU16(t, "psi_mem_pct100", event.Sample.PSIMemPct100, vector.Fields.PSIMemPct100)
				assertEqualU16(t, "psi_io_pct100", event.Sample.PSIIOPct100, vector.Fields.PSIIOPct100)
			default:
				t.Fatalf("unexpected vector type %q", vector.Type)
			}
		})
	}
}

func assertEqualU16(t *testing.T, field string, got, want uint16) {
	t.Helper()
	if got != want {
		t.Fatalf("%s = %d, want %d", field, got, want)
	}
}

func assertEqualU32(t *testing.T, field string, got, want uint32) {
	t.Helper()
	if got != want {
		t.Fatalf("%s = %d, want %d", field, got, want)
	}
}

func assertEqualU64String(t *testing.T, field string, got uint64, want string) {
	t.Helper()
	wantU64, err := strconv.ParseUint(want, 10, 64)
	if err != nil {
		t.Fatalf("parse %s expected value %q: %v", field, want, err)
	}
	if got != wantU64 {
		t.Fatalf("%s = %d, want %d", field, got, wantU64)
	}
}
