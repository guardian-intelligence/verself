//go:build linux

package benchmark

import (
	"bufio"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// cgroupScope manages a transient cgroup v2 scope for a single job.
type cgroupScope struct {
	path string // e.g. /sys/fs/cgroup/benchmark.slice/job-abc123.scope
	fd   int    // open fd to scope dir for SysProcAttr.CgroupFD
}

const cgroupBase = "/sys/fs/cgroup/benchmark.slice"

// initCgroupSlice creates the parent cgroup slice and enables controllers.
// Called once at Runner startup.
func initCgroupSlice(logger *slog.Logger) {
	if err := os.MkdirAll(cgroupBase, 0o755); err != nil {
		logger.Warn("cgroup slice setup failed", "err", err)
		return
	}
	subtreeControl := filepath.Join(cgroupBase, "cgroup.subtree_control")
	_ = os.WriteFile(subtreeControl, []byte("+cpu +memory +io"), 0o644)
}

// cleanStaleCgroupScopes removes leftover scopes from previous crashed runs.
func cleanStaleCgroupScopes(logger *slog.Logger) {
	entries, err := os.ReadDir(cgroupBase)
	if err != nil {
		return
	}
	for _, e := range entries {
		if !e.IsDir() || !strings.HasPrefix(e.Name(), "job-") {
			continue
		}
		path := filepath.Join(cgroupBase, e.Name())
		if err := os.Remove(path); err != nil {
			logger.Warn("stale cgroup scope not removed", "path", path, "err", err)
		} else {
			logger.Info("cleaned stale cgroup scope", "path", path)
		}
	}
}

// newCgroupScope creates a transient cgroup v2 scope for a job.
// Opens an fd to the scope directory for SysProcAttr.CgroupFD,
// which places child processes directly into the cgroup at fork
// time — no PID race between Start() and cgroup placement.
func newCgroupScope(jobID string) (*cgroupScope, error) {
	path := filepath.Join(cgroupBase, "job-"+jobID+".scope")

	if err := os.MkdirAll(path, 0o755); err != nil {
		return nil, fmt.Errorf("create cgroup scope: %w", err)
	}

	fd, err := syscall.Open(path, syscall.O_RDONLY, 0)
	if err != nil {
		os.Remove(path)
		return nil, fmt.Errorf("open cgroup dir: %w", err)
	}

	return &cgroupScope{path: path, fd: fd}, nil
}

// sysProcAttr returns SysProcAttr that places a child process in this cgroup
// at fork time via CgroupFD.
func (s *cgroupScope) sysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{
		UseCgroupFD: true,
		CgroupFD:    s.fd,
	}
}

// collect reads cgroup stats after the job completes. Best-effort:
// missing or unreadable controller files result in zero values.
func (s *cgroupScope) collect() *CgroupStats {
	stats := &CgroupStats{}
	parseCPUStat(filepath.Join(s.path, "cpu.stat"), stats)

	if peak, err := readUint64File(filepath.Join(s.path, "memory.peak")); err == nil {
		stats.MemoryPeak = peak
	}

	parseIOStat(filepath.Join(s.path, "io.stat"), stats)
	return stats
}

// cleanup closes the cgroup fd and removes the scope directory.
func (s *cgroupScope) cleanup() {
	syscall.Close(s.fd)
	os.Remove(s.path)
}

func parseCPUStat(path string, stats *CgroupStats) {
	f, err := os.Open(path)
	if err != nil {
		return
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
}

func parseIOStat(path string, stats *CgroupStats) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
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
