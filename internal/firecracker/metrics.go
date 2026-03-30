package firecracker

import (
	"encoding/json"
	"fmt"
	"os"
)

// VMMetrics holds Firecracker metrics extracted from the metrics FIFO.
// Fields map to ClickHouse wide event columns.
type VMMetrics struct {
	BootTimeUs      uint64 // api_server.process_startup_time_us
	BlockReadBytes  uint64 // block_rootfs.read_bytes
	BlockWriteBytes uint64 // block_rootfs.write_bytes
	BlockReadCount  uint64 // block_rootfs.read_count
	BlockWriteCount uint64 // block_rootfs.write_count
	NetRxBytes      uint64 // net_eth0.rx_bytes_count
	NetTxBytes      uint64 // net_eth0.tx_bytes_count
	VCPUExitCount   uint64 // sum of vcpu exit counters
}

// metricsJSON matches the top-level structure of Firecracker's NDJSON
// metrics output. We only parse the fields we care about.
type metricsJSON struct {
	APIServer   apiServerMetrics   `json:"api_server"`
	BlockRootfs blockMetrics       `json:"block_rootfs"`
	NetEth0     netMetrics         `json:"net_eth0"`
	VCPU        vcpuMetrics        `json:"vcpu"`
}

type apiServerMetrics struct {
	ProcessStartupTimeUs uint64 `json:"process_startup_time_us"`
}

type blockMetrics struct {
	ReadBytes  uint64 `json:"read_bytes"`
	WriteBytes uint64 `json:"write_bytes"`
	ReadCount  uint64 `json:"read_count"`
	WriteCount uint64 `json:"write_count"`
}

type netMetrics struct {
	RxBytesCount uint64 `json:"rx_bytes_count"`
	TxBytesCount uint64 `json:"tx_bytes_count"`
}

type vcpuMetrics struct {
	ExitIOIn      uint64 `json:"exit_io_in"`
	ExitIOOut     uint64 `json:"exit_io_out"`
	ExitMMIORead  uint64 `json:"exit_mmio_read"`
	ExitMMIOWrite uint64 `json:"exit_mmio_write"`
}

// parseMetricsBytes parses metrics from NDJSON data read from the FIFO.
// Returns zero metrics on any error (best-effort).
func parseMetricsBytes(data []byte) *VMMetrics {
	lines := splitLines(data)
	if len(lines) == 0 {
		return &VMMetrics{}
	}
	// The last non-empty line is the most recent flush.
	lastLine := lines[len(lines)-1]

	var m metricsJSON
	if err := json.Unmarshal(lastLine, &m); err != nil {
		fmt.Fprintf(os.Stderr, "parse metrics: %v\n", err)
		return &VMMetrics{}
	}

	return &VMMetrics{
		BootTimeUs:      m.APIServer.ProcessStartupTimeUs,
		BlockReadBytes:  m.BlockRootfs.ReadBytes,
		BlockWriteBytes: m.BlockRootfs.WriteBytes,
		BlockReadCount:  m.BlockRootfs.ReadCount,
		BlockWriteCount: m.BlockRootfs.WriteCount,
		NetRxBytes:      m.NetEth0.RxBytesCount,
		NetTxBytes:      m.NetEth0.TxBytesCount,
		VCPUExitCount:   m.VCPU.ExitIOIn + m.VCPU.ExitIOOut + m.VCPU.ExitMMIORead + m.VCPU.ExitMMIOWrite,
	}
}

func splitLines(data []byte) [][]byte {
	var lines [][]byte
	start := 0
	for i, b := range data {
		if b == '\n' {
			line := data[start:i]
			if len(line) > 0 {
				lines = append(lines, line)
			}
			start = i + 1
		}
	}
	if start < len(data) {
		line := data[start:]
		if len(line) > 0 {
			lines = append(lines, line)
		}
	}
	return lines
}
