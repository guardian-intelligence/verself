package zfs

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// Timeout bounds individual zfs(8) shell invocations. Callers wrap their
// contexts with this when forking zfs subcommands so a hung pool cannot
// stall the orchestrator's caller goroutine indefinitely.
const Timeout = 30 * time.Second

// SnapshotExists reports whether the given dataset@name path resolves to a
// real snapshot. It treats the literal "does not exist" zfs error as a
// negative result; any other failure is returned to the caller.
func SnapshotExists(ctx context.Context, snapshot string) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "zfs", "list", "-H", snapshot)
	out, err := cmd.CombinedOutput()
	if err != nil {
		if strings.Contains(string(out), "does not exist") {
			return false, nil
		}
		return false, fmt.Errorf("zfs list %s: %w", snapshot, err)
	}
	return true, nil
}

// DatasetExists reports whether the given dataset (filesystem or volume)
// exists. Same error contract as SnapshotExists.
func DatasetExists(ctx context.Context, dataset string) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "zfs", "list", "-H", dataset)
	out, err := cmd.CombinedOutput()
	if err != nil {
		if strings.Contains(string(out), "does not exist") {
			return false, nil
		}
		return false, fmt.Errorf("zfs list %s: %w", dataset, err)
	}
	return true, nil
}

// Written returns bytes written to a dataset since it was cloned. It maps to
// the ZFS "written" property and is read with -p so the value is exact bytes
// rather than human-formatted.
func Written(ctx context.Context, dataset string) (uint64, error) {
	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "zfs", "get", "-H", "-p", "-o", "value", "written", dataset)
	out, err := cmd.Output()
	if err != nil {
		return 0, fmt.Errorf("zfs get written %s: %w", dataset, err)
	}
	return strconv.ParseUint(strings.TrimSpace(string(out)), 10, 64)
}

// Volsize returns the provisioned size in bytes for a ZFS volume.
func Volsize(ctx context.Context, dataset string) (uint64, error) {
	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "zfs", "get", "-H", "-p", "-o", "value", "volsize", dataset)
	out, err := cmd.Output()
	if err != nil {
		return 0, fmt.Errorf("zfs get volsize %s: %w", dataset, err)
	}
	return strconv.ParseUint(strings.TrimSpace(string(out)), 10, 64)
}
