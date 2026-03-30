//go:build !linux

package benchmark

import (
	"fmt"
	"log/slog"
	"syscall"
)

// cgroupScope is a no-op on non-Linux platforms.
type cgroupScope struct{}

func initCgroupSlice(_ *slog.Logger)        {}
func cleanStaleCgroupScopes(_ *slog.Logger) {}

func newCgroupScope(_ string) (*cgroupScope, error) {
	return nil, fmt.Errorf("cgroup v2 requires linux")
}

func (s *cgroupScope) sysProcAttr() *syscall.SysProcAttr { return nil }
func (s *cgroupScope) collect() *CgroupStats             { return &CgroupStats{} }
func (s *cgroupScope) cleanup()                           {}
