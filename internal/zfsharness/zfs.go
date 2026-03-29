package zfsharness

import (
	"bufio"
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// DefaultTimeout is the default command timeout. ZFS can hang indefinitely
// on degraded pools or during imports — always use a timeout.
const DefaultTimeout = 30 * time.Second

// DatasetInfo holds metadata about a ZFS dataset (filesystem, volume, or snapshot).
type DatasetInfo struct {
	Name       string
	Type       string // "filesystem", "volume", "snapshot"
	Used       uint64
	Available  uint64
	Referenced uint64
	Mountpoint string
	Origin     string // clone origin (empty for non-clones)
}

// Error wraps a ZFS command failure with context.
type Error struct {
	Cmd    string // full command string
	Stderr string // combined output
	Err    error  // underlying exec error
}

func (e *Error) Error() string {
	if e.Stderr != "" {
		return fmt.Sprintf("%s: %s", e.Cmd, e.Stderr)
	}
	return fmt.Sprintf("%s: %s", e.Cmd, e.Err)
}

func (e *Error) Unwrap() error { return e.Err }

// executor wraps ZFS CLI commands. Unexported — the Harness is the public API.
type executor struct {
	timeout time.Duration
}

func (e *executor) effectiveTimeout() time.Duration {
	if e.timeout > 0 {
		return e.timeout
	}
	return DefaultTimeout
}

// run executes a ZFS/zpool command with context timeout.
func (e *executor) run(ctx context.Context, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, e.effectiveTimeout())
	defer cancel()

	cmd := exec.CommandContext(ctx, name, args...)
	out, err := cmd.CombinedOutput()
	output := strings.TrimSpace(string(out))
	if err != nil {
		return output, &Error{
			Cmd:    name + " " + strings.Join(args, " "),
			Stderr: output,
			Err:    err,
		}
	}
	return output, nil
}

// zfs runs a `zfs` subcommand.
func (e *executor) zfs(ctx context.Context, args ...string) (string, error) {
	return e.run(ctx, "zfs", args...)
}

// exists checks whether a dataset or snapshot exists.
func (e *executor) exists(ctx context.Context, name string) (bool, error) {
	_, err := e.zfs(ctx, "list", "-H", name)
	if err != nil {
		if strings.Contains(err.Error(), "does not exist") {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// snapshot creates a ZFS snapshot. Name should be "dataset@snapname".
func (e *executor) snapshot(ctx context.Context, name string) error {
	_, err := e.zfs(ctx, "snapshot", name)
	return err
}

// clone creates a clone from a snapshot. The clone is a writable copy.
//
// The kernel operation (ZFS_IOC_CLONE) is ~1.7ms — metadata-only, no data
// copying. Total wall time is ~5.7ms due to subprocess overhead.
//
// Note: ZFS cannot destroy a snapshot that has active clones. Every project
// handles this differently:
//   - Incus: "ghost graveyard" — renames undeletable datasets to deleted/ namespace.
//   - OBuilder: promotion dance — zfs promote + rename to re-parent.
//   - DBLab: dual-pool rotation.
//   - Velo: clone-then-swap.
func (e *executor) clone(ctx context.Context, snapshot, target string) error {
	_, err := e.zfs(ctx, "clone", snapshot, target)
	return err
}

// cloneWithProps creates a clone and sets properties atomically.
func (e *executor) cloneWithProps(ctx context.Context, snapshot, target string, props map[string]string) error {
	args := []string{"clone"}
	for k, v := range props {
		args = append(args, "-o", k+"="+v)
	}
	args = append(args, snapshot, target)
	_, err := e.zfs(ctx, args...)
	return err
}

// destroy removes a dataset, snapshot, or clone. Use recursive=true for -r.
func (e *executor) destroy(ctx context.Context, name string, recursive bool) error {
	args := []string{"destroy"}
	if recursive {
		args = append(args, "-r")
	}
	args = append(args, name)
	_, err := e.zfs(ctx, args...)
	return err
}

// rollback rolls a dataset back to a snapshot. Intermediate snapshots are
// destroyed (-r flag).
func (e *executor) rollback(ctx context.Context, snapshot string) error {
	_, err := e.zfs(ctx, "rollback", "-r", snapshot)
	return err
}

// promote reverses the clone/origin relationship so the clone becomes the
// "parent" and the original dataset becomes its dependent.
//
// OBuilder documents the full promotion model with a worked example.
// Incus sidesteps promotion via its ghost graveyard pattern.
func (e *executor) promote(ctx context.Context, dataset string) error {
	_, err := e.zfs(ctx, "promote", dataset)
	return err
}

// rename renames a dataset. Used by Incus's ghost graveyard pattern.
func (e *executor) rename(ctx context.Context, oldName, newName string) error {
	_, err := e.zfs(ctx, "rename", oldName, newName)
	return err
}

// getProperty returns a single property value for a dataset.
func (e *executor) getProperty(ctx context.Context, dataset, property string) (string, error) {
	out, err := e.zfs(ctx, "get", "-H", "-p", "-o", "value", property, dataset)
	if err != nil {
		return "", err
	}
	val := strings.TrimSpace(out)
	if val == "-" {
		return "", nil
	}
	return val, nil
}

// setProperty sets a property on a dataset.
func (e *executor) setProperty(ctx context.Context, dataset, property, value string) error {
	_, err := e.zfs(ctx, "set", property+"="+value, dataset)
	return err
}

// getDataset returns metadata for a single dataset.
func (e *executor) getDataset(ctx context.Context, name string) (*DatasetInfo, error) {
	out, err := e.zfs(ctx, "list", "-H", "-p",
		"-o", "name,type,used,avail,refer,mountpoint,origin",
		name)
	if err != nil {
		return nil, err
	}
	return parseDatasetLine(out)
}

// listSnapshots lists all snapshots under a dataset.
func (e *executor) listSnapshots(ctx context.Context, dataset string) ([]DatasetInfo, error) {
	return e.list(ctx, dataset, "snapshot")
}

// listChildren lists all child filesystems under a dataset (non-recursive).
func (e *executor) listChildren(ctx context.Context, dataset string) ([]DatasetInfo, error) {
	return e.list(ctx, dataset, "filesystem")
}

// listClones returns datasets whose origin is the given snapshot.
func (e *executor) listClones(ctx context.Context, pool, snapshot string) ([]string, error) {
	out, err := e.zfs(ctx, "list", "-H", "-p", "-o", "name,origin",
		"-r", "-t", "filesystem,volume", pool)
	if err != nil {
		return nil, err
	}
	var clones []string
	scanner := bufio.NewScanner(strings.NewReader(out))
	for scanner.Scan() {
		fields := strings.Split(scanner.Text(), "\t")
		if len(fields) >= 2 && fields[1] == snapshot {
			clones = append(clones, fields[0])
		}
	}
	return clones, nil
}

func (e *executor) list(ctx context.Context, dataset, dsType string) ([]DatasetInfo, error) {
	out, err := e.zfs(ctx, "list", "-H", "-p",
		"-o", "name,type,used,avail,refer,mountpoint,origin",
		"-r", "-t", dsType, dataset)
	if err != nil {
		return nil, err
	}

	var datasets []DatasetInfo
	scanner := bufio.NewScanner(strings.NewReader(out))
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		ds, err := parseDatasetLine(line)
		if err != nil {
			continue
		}
		datasets = append(datasets, *ds)
	}
	return datasets, nil
}

// written returns the bytes written to a dataset since it was cloned.
func (e *executor) written(ctx context.Context, dataset string) (uint64, error) {
	val, err := e.getProperty(ctx, dataset, "written")
	if err != nil {
		return 0, err
	}
	return strconv.ParseUint(val, 10, 64)
}

// createFilesystem creates a new ZFS filesystem dataset.
func (e *executor) createFilesystem(ctx context.Context, name string) error {
	_, err := e.zfs(ctx, "create", "-p", name)
	return err
}

// sendCmd streams a snapshot to stdout (for piping to receive).
func (e *executor) sendCmd(ctx context.Context, snapshot string, incremental string) (*exec.Cmd, context.CancelFunc) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	args := []string{"send"}
	if incremental != "" {
		args = append(args, "-i", incremental)
	}
	args = append(args, snapshot)
	return exec.CommandContext(ctx, "zfs", args...), cancel
}

// receiveCmd returns a command that receives a ZFS stream into a dataset.
func (e *executor) receiveCmd(ctx context.Context, dataset string) (*exec.Cmd, context.CancelFunc) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	return exec.CommandContext(ctx, "zfs", "receive", "-F", dataset), cancel
}

// parseDatasetLine parses one line of `zfs list -H -p -o name,type,used,avail,refer,mountpoint,origin`.
func parseDatasetLine(line string) (*DatasetInfo, error) {
	fields := strings.Split(line, "\t")
	if len(fields) < 7 {
		return nil, fmt.Errorf("expected 7 tab-separated fields, got %d: %q", len(fields), line)
	}

	used, _ := strconv.ParseUint(fields[2], 10, 64)
	avail, _ := strconv.ParseUint(fields[3], 10, 64)
	ref, _ := strconv.ParseUint(fields[4], 10, 64)

	origin := fields[6]
	if origin == "-" {
		origin = ""
	}
	mountpoint := fields[5]
	if mountpoint == "-" {
		mountpoint = ""
	}

	return &DatasetInfo{
		Name:       fields[0],
		Type:       fields[1],
		Used:       used,
		Available:  avail,
		Referenced: ref,
		Mountpoint: mountpoint,
		Origin:     origin,
	}, nil
}
