package zfs

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

func DatasetExists(ctx context.Context, dataset string) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "zfs", "list", "-H", "-o", "name", dataset)
	out, err := cmd.CombinedOutput()
	if err != nil {
		text := strings.TrimSpace(string(out))
		if strings.Contains(text, "does not exist") || strings.Contains(text, "dataset does not exist") {
			return false, nil
		}
		return false, fmt.Errorf("zfs list %s: %s: %w", dataset, text, err)
	}
	return true, nil
}

func SnapshotExists(ctx context.Context, snapshot string) (bool, error) {
	if !strings.Contains(snapshot, "@") {
		return false, fmt.Errorf("snapshot ref is invalid: %s", snapshot)
	}
	return DatasetExists(ctx, snapshot)
}

func Volsize(ctx context.Context, dataset string) (uint64, error) {
	return getUintProperty(ctx, dataset, "volsize")
}

func Quota(ctx context.Context, dataset string) (uint64, error) {
	return getUintProperty(ctx, dataset, "quota")
}

func Available(ctx context.Context, dataset string) (uint64, error) {
	return getUintProperty(ctx, dataset, "available")
}

func Written(ctx context.Context, dataset string) (uint64, error) {
	return getUintProperty(ctx, dataset, "written")
}

type PoolStats struct {
	SizeBytes      uint64
	AllocatedBytes uint64
	FreeBytes      uint64
}

func PoolCapacity(ctx context.Context, pool string) (PoolStats, error) {
	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "zpool", "list", "-H", "-p", "-o", "size,alloc,free", pool)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return PoolStats{}, fmt.Errorf("zpool list %s: %s: %w", pool, strings.TrimSpace(string(out)), err)
	}
	fields := strings.Fields(strings.TrimSpace(string(out)))
	if len(fields) != 3 {
		return PoolStats{}, fmt.Errorf("zpool list %s returned %d fields, want 3", pool, len(fields))
	}
	sizeBytes, err := strconv.ParseUint(fields[0], 10, 64)
	if err != nil {
		return PoolStats{}, fmt.Errorf("parse zpool size=%q: %w", fields[0], err)
	}
	allocatedBytes, err := strconv.ParseUint(fields[1], 10, 64)
	if err != nil {
		return PoolStats{}, fmt.Errorf("parse zpool allocated=%q: %w", fields[1], err)
	}
	freeBytes, err := strconv.ParseUint(fields[2], 10, 64)
	if err != nil {
		return PoolStats{}, fmt.Errorf("parse zpool free=%q: %w", fields[2], err)
	}
	return PoolStats{SizeBytes: sizeBytes, AllocatedBytes: allocatedBytes, FreeBytes: freeBytes}, nil
}

func Used(ctx context.Context, dataset string) (uint64, error) {
	return getUintProperty(ctx, dataset, "used")
}

func getUintProperty(ctx context.Context, target, property string) (uint64, error) {
	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "zfs", "get", "-H", "-p", "-o", "value", property, target)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("zfs get %s on %s: %s: %w", property, target, strings.TrimSpace(string(out)), err)
	}
	text := strings.TrimSpace(string(out))
	if text == "" || text == "-" || text == "none" {
		return 0, nil
	}
	value, err := strconv.ParseUint(text, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse zfs %s=%q on %s: %w", property, text, target, err)
	}
	return value, nil
}
