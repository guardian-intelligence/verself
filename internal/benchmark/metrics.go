package benchmark

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

// CgroupStats holds metrics read from cgroup v2 controller files.
type CgroupStats struct {
	CPUUserUs    uint64 // from cpu.stat: user_usec
	CPUSystemUs  uint64 // from cpu.stat: system_usec
	MemoryPeak   uint64 // from memory.peak (bytes)
	IOReadBytes  uint64 // from io.stat: rbytes
	IOWriteBytes uint64 // from io.stat: wbytes
}

// cgroupScope manages a transient cgroup v2 scope for a single job.
type cgroupScope struct {
	path string // e.g. /sys/fs/cgroup/benchmark.slice/job-abc123.scope
}

const cgroupBase = "/sys/fs/cgroup/benchmark.slice"

// newCgroupScope creates a transient cgroup v2 scope for a job.
// The caller must call cleanup() when done.
func newCgroupScope(jobID string) (*cgroupScope, error) {
	scope := &cgroupScope{
		path: filepath.Join(cgroupBase, "job-"+jobID+".scope"),
	}

	// Ensure parent slice exists.
	if err := os.MkdirAll(cgroupBase, 0o755); err != nil {
		return nil, fmt.Errorf("create cgroup slice: %w", err)
	}

	// Create the scope directory.
	if err := os.MkdirAll(scope.path, 0o755); err != nil {
		return nil, fmt.Errorf("create cgroup scope: %w", err)
	}

	// Enable controllers we need.
	subtreeControl := filepath.Join(cgroupBase, "cgroup.subtree_control")
	_ = os.WriteFile(subtreeControl, []byte("+cpu +memory +io"), 0o644)

	return scope, nil
}

// addPID moves a process into this cgroup scope.
func (s *cgroupScope) addPID(pid int) error {
	procsFile := filepath.Join(s.path, "cgroup.procs")
	return os.WriteFile(procsFile, []byte(strconv.Itoa(pid)), 0o644)
}

// collect reads cgroup stats after the job completes.
func (s *cgroupScope) collect() (*CgroupStats, error) {
	stats := &CgroupStats{}

	// cpu.stat: user_usec and system_usec
	if err := parseCPUStat(filepath.Join(s.path, "cpu.stat"), stats); err != nil {
		return stats, nil // best-effort: return partial stats
	}

	// memory.peak: single uint64 value
	if peak, err := readUint64File(filepath.Join(s.path, "memory.peak")); err == nil {
		stats.MemoryPeak = peak
	}

	// io.stat: rbytes= and wbytes= (summed across devices)
	parseIOStat(filepath.Join(s.path, "io.stat"), stats)

	return stats, nil
}

// cleanup removes the cgroup scope directory.
func (s *cgroupScope) cleanup() error {
	return os.Remove(s.path)
}

func parseCPUStat(path string, stats *CgroupStats) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		key, val, ok := strings.Cut(sc.Text(), " ")
		if !ok {
			continue
		}
		v, err := strconv.ParseUint(val, 10, 64)
		if err != nil {
			continue
		}
		switch key {
		case "user_usec":
			stats.CPUUserUs = v
		case "system_usec":
			stats.CPUSystemUs = v
		}
	}
	return sc.Err()
}

func parseIOStat(path string, stats *CgroupStats) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		// Format: "major:minor rbytes=N wbytes=N rios=N wios=N ..."
		for _, field := range strings.Fields(sc.Text()) {
			key, val, ok := strings.Cut(field, "=")
			if !ok {
				continue
			}
			v, err := strconv.ParseUint(val, 10, 64)
			if err != nil {
				continue
			}
			switch key {
			case "rbytes":
				stats.IOReadBytes += v
			case "wbytes":
				stats.IOWriteBytes += v
			}
		}
	}
}

func readUint64File(path string) (uint64, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	return strconv.ParseUint(strings.TrimSpace(string(data)), 10, 64)
}

// HardwareInfo is collected once at Runner startup and reused for all events.
type HardwareInfo struct {
	CPUModel string
	Cores    uint16
	MemoryMB uint32
	DiskType string // "NVMe", "SSD", "HDD"
}

// detectHardware reads system information from /proc and /sys.
func detectHardware() HardwareInfo {
	hw := HardwareInfo{
		Cores: uint16(runtime.NumCPU()),
	}

	// CPU model from /proc/cpuinfo
	if f, err := os.Open("/proc/cpuinfo"); err == nil {
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			if key, val, ok := strings.Cut(sc.Text(), ":"); ok {
				if strings.TrimSpace(key) == "model name" {
					hw.CPUModel = strings.TrimSpace(val)
					break
				}
			}
		}
		f.Close()
	}

	// Memory from /proc/meminfo
	if f, err := os.Open("/proc/meminfo"); err == nil {
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
		f.Close()
	}

	// Disk type: check /sys/block/*/queue/rotational for the first NVMe or disk
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
	env := EnvInfo{}
	if out, err := exec.Command("node", "--version").Output(); err == nil {
		env.NodeVersion = strings.TrimSpace(strings.TrimPrefix(string(out), "v"))
	}
	if out, err := exec.Command("npm", "--version").Output(); err == nil {
		env.NPMVersion = strings.TrimSpace(string(out))
	}
	return env
}
