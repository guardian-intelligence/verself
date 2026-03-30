package benchmark

import (
	"bufio"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// CgroupStats holds metrics read from cgroup v2 controller files.
type CgroupStats struct {
	CPUUserUs    uint64 // from cpu.stat: user_usec
	CPUSystemUs  uint64 // from cpu.stat: system_usec
	MemoryPeak   uint64 // from memory.peak (bytes)
	IOReadBytes  uint64 // from io.stat: rbytes
	IOWriteBytes uint64 // from io.stat: wbytes
}

// HardwareInfo is collected once at Runner startup and reused for all events.
type HardwareInfo struct {
	CPUModel string
	Cores    uint16
	MemoryMB uint32
	DiskType string
}

// detectHardware reads system information from /proc and /sys.
func detectHardware() HardwareInfo {
	hw := HardwareInfo{
		Cores: uint16(runtime.NumCPU()),
	}

	if f, err := os.Open("/proc/cpuinfo"); err == nil {
		defer f.Close()
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			if key, val, ok := strings.Cut(sc.Text(), ":"); ok {
				if strings.TrimSpace(key) == "model name" {
					hw.CPUModel = strings.TrimSpace(val)
					break
				}
			}
		}
	}

	if f, err := os.Open("/proc/meminfo"); err == nil {
		defer f.Close()
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			if strings.HasPrefix(sc.Text(), "MemTotal:") {
				fields := strings.Fields(sc.Text())
				if len(fields) >= 2 {
					if kb, err := strconv.ParseUint(fields[1], 10, 64); err == nil {
						hw.MemoryMB = uint32(kb / 1024)
					}
				}
				break
			}
		}
	}

	hw.DiskType = detectDiskType()
	return hw
}

func detectDiskType() string {
	entries, err := os.ReadDir("/sys/block")
	if err != nil {
		return "unknown"
	}
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, "loop") || strings.HasPrefix(name, "ram") {
			continue
		}
		if strings.HasPrefix(name, "nvme") {
			return "NVMe"
		}
		rot, err := os.ReadFile(filepath.Join("/sys/block", name, "queue", "rotational"))
		if err != nil {
			continue
		}
		if strings.TrimSpace(string(rot)) == "0" {
			return "SSD"
		}
		return "HDD"
	}
	return "unknown"
}

// EnvInfo is collected once at Runner startup.
type EnvInfo struct {
	NodeVersion string
	NPMVersion  string
}

// detectEnv collects runtime environment versions.
func detectEnv() EnvInfo {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	env := EnvInfo{}
	if out, err := exec.CommandContext(ctx, "node", "--version").Output(); err == nil {
		env.NodeVersion = strings.TrimSpace(strings.TrimPrefix(string(out), "v"))
	}
	if out, err := exec.CommandContext(ctx, "npm", "--version").Output(); err == nil {
		env.NPMVersion = strings.TrimSpace(string(out))
	}
	return env
}

func readUint64File(path string) (uint64, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	return strconv.ParseUint(strings.TrimSpace(string(data)), 10, 64)
}
