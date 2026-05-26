package main

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/verself/vm-orchestrator/vmproto"
	"golang.org/x/sys/unix"
)

const (
	chronydBin       = "/usr/sbin/chronyd"
	chronycBin       = "/usr/bin/chronyc"
	chronyConfigPath = "/etc/chrony/chrony.conf"
	chronyPTPDevice  = "/dev/ptp0"
	chronySocketPath = "/run/chrony/chronyd.sock"
	chronyDriftPath  = "/var/lib/chrony/drift"

	chronyReadyTimeout = 5 * time.Second
	chronyReadyPoll    = 50 * time.Millisecond
	chronycCommandWait = 15 * time.Second
	chronyStopGrace    = 2 * time.Second
	chronyKillGrace    = time.Second
	clockMaxOffsetNS   = int64(time.Millisecond)
	clockMaxSkewPPM    = 100.0
	clockMaxWallOffset = time.Millisecond
)

var (
	wallClockNow                    = time.Now
	setRealtimeUnixNano             = realSetRealtimeUnixNano
	startChronyDaemon               = startChrony
	stopChronyDaemonForSnapshot     = stopChronyForSnapshot
	restartChronyDaemonAfterRestore = restartChronyAfterRestore
	syncRunningChrony               = syncClockWithChrony
	guestChrony                     = &chronyDaemon{}
)

type chronyDaemonState string

const (
	chronyStopped  chronyDaemonState = "stopped"
	chronyStarting chronyDaemonState = "starting"
	chronyRunning  chronyDaemonState = "running"
	chronyStopping chronyDaemonState = "stopping"
)

type chronyDaemon struct {
	mu     sync.Mutex
	cmd    *exec.Cmd
	state  chronyDaemonState
	exited chan error
}

func startChrony() error {
	return guestChrony.start()
}

func stopChronyForSnapshot() error {
	return guestChrony.stop(true)
}

func restartChronyAfterRestore() error {
	if err := guestChrony.stop(false); err != nil {
		return err
	}
	return guestChrony.start()
}

func (d *chronyDaemon) start() error {
	if err := ensureChronyRuntime(); err != nil {
		return err
	}

	d.mu.Lock()
	if d.cmd != nil {
		state := d.state
		d.mu.Unlock()
		if state == chronyRunning {
			return nil
		}
		return fmt.Errorf("chronyd is %s", state)
	}
	if err := removeChronyRuntimeFiles(false); err != nil {
		d.mu.Unlock()
		return err
	}
	cmd := exec.Command(chronydBin, "-n", "-f", chronyConfigPath, "-L", "1")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		d.mu.Unlock()
		return fmt.Errorf("start chronyd: %w", err)
	}
	exited := make(chan error, 1)
	d.cmd = cmd
	d.state = chronyStarting
	d.exited = exited
	d.mu.Unlock()

	go d.wait(cmd, exited)

	if err := waitForChronyCommand(exited); err != nil {
		if stopErr := d.stop(false); stopErr != nil {
			return fmt.Errorf("%w; stop chronyd after failed startup: %v", err, stopErr)
		}
		return err
	}

	d.mu.Lock()
	if d.cmd != cmd {
		d.mu.Unlock()
		return errors.New("chronyd exited during startup")
	}
	d.state = chronyRunning
	d.mu.Unlock()

	_, _ = fmt.Fprintf(os.Stdout, "%s chronyd started (pid=%d source=%s socket=%s)\n", logPrefix, cmd.Process.Pid, chronyPTPDevice, chronySocketPath)
	return nil
}

func (d *chronyDaemon) wait(cmd *exec.Cmd, exited chan<- error) {
	err := cmd.Wait()
	d.mu.Lock()
	state := d.state
	if d.cmd == cmd {
		d.cmd = nil
		d.state = chronyStopped
		d.exited = nil
	}
	d.mu.Unlock()

	exited <- err
	close(exited)

	if state == chronyStarting || state == chronyStopping {
		return
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s FATAL: chronyd exited: %v\n", logPrefix, err)
	} else {
		fmt.Fprintf(os.Stderr, "%s FATAL: chronyd exited\n", logPrefix)
	}
	os.Exit(1)
}

func (d *chronyDaemon) stop(removeDrift bool) error {
	d.mu.Lock()
	cmd := d.cmd
	exited := d.exited
	if cmd != nil {
		d.state = chronyStopping
	}
	d.mu.Unlock()

	if cmd != nil {
		if _, err := runChronycWithTimeout(time.Second, "shutdown"); err != nil {
			if signalErr := cmd.Process.Signal(syscall.SIGTERM); signalErr != nil && !errors.Is(signalErr, os.ErrProcessDone) {
				return fmt.Errorf("signal chronyd: %w", signalErr)
			}
		}
		if err := waitForChronyExit(cmd, exited); err != nil {
			return err
		}
	}
	if err := removeChronyRuntimeFiles(removeDrift); err != nil {
		return err
	}
	return nil
}

func waitForChronyExit(cmd *exec.Cmd, exited <-chan error) error {
	timer := time.NewTimer(chronyStopGrace)
	defer timer.Stop()
	select {
	case <-exited:
		return nil
	case <-timer.C:
		if err := cmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
			return fmt.Errorf("kill chronyd: %w", err)
		}
	}

	killTimer := time.NewTimer(chronyKillGrace)
	defer killTimer.Stop()
	select {
	case <-exited:
		return nil
	case <-killTimer.C:
		return errors.New("chronyd did not exit after SIGKILL")
	}
}

func ensureChronyRuntime() error {
	for _, path := range []string{chronydBin, chronycBin, chronyConfigPath} {
		if _, err := os.Stat(path); err != nil {
			return fmt.Errorf("chrony runtime %s: %w", path, err)
		}
	}
	if err := requirePTPDevice(); err != nil {
		return err
	}
	chronyUID, chronyGID, err := lookupPasswdUser("_chrony")
	if err != nil {
		return err
	}
	if err := ensureChronyDir("/run/chrony", 0o750, chronyUID, chronyGID); err != nil {
		return err
	}
	if err := ensureChronyDir("/var/lib/chrony", 0o755, chronyUID, chronyGID); err != nil {
		return err
	}
	if err := os.Chown(chronyPTPDevice, chronyUID, chronyGID); err != nil {
		return fmt.Errorf("chown %s: %w", chronyPTPDevice, err)
	}
	return nil
}

func removeChronyRuntimeFiles(removeDrift bool) error {
	if err := os.Remove(chronySocketPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove %s: %w", chronySocketPath, err)
	}
	if removeDrift {
		if err := os.Remove(chronyDriftPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove %s: %w", chronyDriftPath, err)
		}
	}
	return nil
}

func ensureChronyDir(dir string, mode os.FileMode, uid, gid int) error {
	if err := os.MkdirAll(dir, mode); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}
	if err := os.Chown(dir, uid, gid); err != nil {
		return fmt.Errorf("chown %s: %w", dir, err)
	}
	if err := os.Chmod(dir, mode); err != nil {
		return fmt.Errorf("chmod %s: %w", dir, err)
	}
	return nil
}

func waitForChronyCommand(done <-chan error) error {
	deadline := time.Now().Add(chronyReadyTimeout)
	var lastErr error
	for time.Now().Before(deadline) {
		select {
		case err, ok := <-done:
			if !ok {
				return errors.New("chronyd exited during startup")
			}
			if err == nil {
				return errors.New("chronyd exited during startup")
			}
			return fmt.Errorf("chronyd exited during startup: %w", err)
		default:
		}
		if _, err := runChronycWithTimeout(time.Second, "tracking"); err == nil {
			return nil
		} else {
			lastErr = err
		}
		time.Sleep(chronyReadyPoll)
	}
	if lastErr == nil {
		return errors.New("chronyd command socket not ready")
	}
	return fmt.Errorf("chronyd command socket not ready: %w", lastErr)
}

func syncClockWithChrony() (vmproto.ClockSyncResult, error) {
	started := time.Now()
	result := vmproto.ClockSyncResult{Status: "syncing"}
	if err := requirePTPDevice(); err != nil {
		result.Status = "ptp_unavailable"
		result.WaitSyncMS = time.Since(started).Milliseconds()
		return result, err
	}

	if out, err := runChronyc("waitsync", "10", "0", "0", "1"); err != nil {
		result.Status = "source_wait_failed"
		result.TrackingRaw = collectChronyDiagnostics(out)
		result.WaitSyncMS = time.Since(started).Milliseconds()
		return result, fmt.Errorf("chrony source wait: %w", err)
	}
	if out, err := runChronyc("makestep"); err != nil {
		result.Status = "makestep_failed"
		result.TrackingRaw = collectChronyDiagnostics(out)
		result.WaitSyncMS = time.Since(started).Milliseconds()
		return result, fmt.Errorf("chrony makestep: %w", err)
	}
	maxOffsetSeconds := strconv.FormatFloat(float64(clockMaxOffsetNS)/float64(time.Second), 'f', 3, 64)
	if out, err := runChronyc("waitsync", "10", maxOffsetSeconds, strconv.FormatFloat(clockMaxSkewPPM, 'f', 0, 64), "1"); err != nil {
		result.Status = "sync_wait_failed"
		result.TrackingRaw = collectChronyDiagnostics(out)
		result.WaitSyncMS = time.Since(started).Milliseconds()
		return result, fmt.Errorf("chrony sync wait: %w", err)
	}

	trackingOut, err := runChronyc("tracking")
	result.WaitSyncMS = time.Since(started).Milliseconds()
	result.TrackingRaw = strings.TrimSpace(trackingOut)
	if err != nil {
		result.Status = "tracking_failed"
		return result, fmt.Errorf("chrony tracking: %w", err)
	}
	tracking, err := parseChronyTracking(trackingOut)
	if err != nil {
		result.Status = "tracking_parse_failed"
		return result, err
	}
	tracking.WaitSyncMS = result.WaitSyncMS
	tracking.TrackingRaw = result.TrackingRaw
	if tracking.Source == "" {
		tracking.Status = "source_missing"
		return tracking, errors.New("chrony tracking has no selected source")
	}
	if tracking.LeapStatus != "" && tracking.LeapStatus != "Normal" {
		tracking.Status = "leap_not_normal"
		return tracking, fmt.Errorf("chrony leap status is %s", tracking.LeapStatus)
	}
	if math.Abs(float64(tracking.OffsetNS)) > float64(clockMaxOffsetNS) {
		tracking.Status = "offset_exceeded"
		return tracking, fmt.Errorf("chrony offset %dns exceeds %dns", tracking.OffsetNS, clockMaxOffsetNS)
	}
	if tracking.SkewPPM > clockMaxSkewPPM {
		tracking.Status = "skew_exceeded"
		return tracking, fmt.Errorf("chrony skew %.3fppm exceeds %.3fppm", tracking.SkewPPM, clockMaxSkewPPM)
	}
	tracking.Status = "synchronized"
	return tracking, nil
}

func syncRestoredClockWithChrony(hostUnixNano int64) (vmproto.ClockSyncResult, error) {
	result := vmproto.ClockSyncResult{Status: "restoring_host_time"}
	if err := verifyRestoredWallClock(&result, hostUnixNano); err != nil {
		return result, err
	}
	// Snapshotting live chronyd preserves stale source-selection state.
	if err := restartChronyDaemonAfterRestore(); err != nil {
		result.Status = "restored_chrony_start_failed"
		return result, fmt.Errorf("restart restored chrony: %w", err)
	}
	syncResult, err := syncRunningChrony()
	mergeRestoredWallClockFields(&syncResult, result)
	if err != nil {
		if strings.TrimSpace(syncResult.Status) == "" {
			syncResult.Status = "restored_chrony_wait_failed"
		} else {
			syncResult.Status = "restored_chrony_" + syncResult.Status
		}
		return syncResult, fmt.Errorf("restored chrony sync: %w", err)
	}
	return syncResult, nil
}

func collectChronyDiagnostics(primary string) string {
	var sections []string
	if trimmed := strings.TrimSpace(primary); trimmed != "" {
		sections = append(sections, "primary:\n"+trimmed)
	}
	for _, args := range [][]string{
		{"tracking"},
		{"sources", "-a", "-v"},
		{"selectdata", "-a", "-v"},
		{"sourcestats", "-a", "-v"},
	} {
		out, err := runChronycWithTimeout(time.Second, args...)
		trimmed := strings.TrimSpace(out)
		if err != nil {
			if trimmed == "" {
				trimmed = err.Error()
			} else {
				trimmed = trimmed + "\n" + err.Error()
			}
		}
		sections = append(sections, strings.Join(args, " ")+":\n"+trimmed)
	}
	return strings.Join(sections, "\n\n")
}

func mergeRestoredWallClockFields(dst *vmproto.ClockSyncResult, src vmproto.ClockSyncResult) {
	dst.HostUnixNano = src.HostUnixNano
	dst.GuestUnixNano = src.GuestUnixNano
	dst.WallOffsetNS = src.WallOffsetNS
	dst.PreStepWallOffsetNS = src.PreStepWallOffsetNS
	dst.PostStepWallOffsetNS = src.PostStepWallOffsetNS
	dst.HostStepApplied = src.HostStepApplied
}

func verifyRestoredWallClock(result *vmproto.ClockSyncResult, hostUnixNano int64) error {
	if hostUnixNano <= 0 {
		result.Status = "host_time_missing"
		return errors.New("after_restore host_unix_nano is required")
	}
	result.HostUnixNano = hostUnixNano
	guestUnixNano := wallClockNow().UnixNano()
	preOffset := guestUnixNano - hostUnixNano
	result.GuestUnixNano = guestUnixNano
	result.WallOffsetNS = preOffset
	if wallOffsetWithinLimit(preOffset) {
		return nil
	}

	result.PreStepWallOffsetNS = preOffset
	if err := setRealtimeUnixNano(hostUnixNano); err != nil {
		result.Status = "host_step_failed"
		return fmt.Errorf("step restored realtime clock: %w", err)
	}
	result.HostStepApplied = true

	guestUnixNano = wallClockNow().UnixNano()
	postOffset := guestUnixNano - hostUnixNano
	result.GuestUnixNano = guestUnixNano
	result.WallOffsetNS = postOffset
	result.PostStepWallOffsetNS = postOffset
	if !wallOffsetWithinLimit(postOffset) {
		result.Status = "host_step_offset_exceeded"
		return fmt.Errorf("restored wall clock offset %dns exceeds %dns after host step", postOffset, clockMaxWallOffset.Nanoseconds())
	}
	return nil
}

func wallOffsetWithinLimit(offsetNS int64) bool {
	return math.Abs(float64(offsetNS)) <= float64(clockMaxWallOffset.Nanoseconds())
}

func realSetRealtimeUnixNano(unixNano int64) error {
	ts := unix.NsecToTimespec(unixNano)
	if err := unix.ClockSettime(unix.CLOCK_REALTIME, &ts); err != nil {
		return fmt.Errorf("clock_settime(CLOCK_REALTIME): %w", err)
	}
	return nil
}

func requirePTPDevice() error {
	info, err := os.Stat(chronyPTPDevice)
	if err != nil {
		return fmt.Errorf("chrony PTP device %s: %w", chronyPTPDevice, err)
	}
	if info.Mode()&os.ModeDevice == 0 {
		return fmt.Errorf("chrony PTP device %s is not a device", chronyPTPDevice)
	}
	return nil
}

func runChronyc(args ...string) (string, error) {
	return runChronycWithTimeout(chronycCommandWait, args...)
}

func runChronycWithTimeout(timeout time.Duration, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	allArgs := chronycArgs(args...)
	cmd := exec.CommandContext(ctx, chronycBin, allArgs...)
	out, err := cmd.CombinedOutput()
	text := string(out)
	if ctx.Err() != nil {
		return text, ctx.Err()
	}
	if err != nil {
		return text, fmt.Errorf("%s %s: %s: %w", chronycBin, strings.Join(allArgs, " "), strings.TrimSpace(text), err)
	}
	return text, nil
}

func chronycArgs(args ...string) []string {
	return append([]string{"-h", chronySocketPath, "-n"}, args...)
}

func parseChronyTracking(out string) (vmproto.ClockSyncResult, error) {
	result := vmproto.ClockSyncResult{}
	var haveReferenceID, haveSystemTime, haveSkew, haveLeapStatus bool
	for _, line := range strings.Split(out, "\n") {
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		switch key {
		case "Reference ID":
			haveReferenceID = true
			result.Source = chronyReferenceName(value)
		case "System time":
			haveSystemTime = true
			offset, err := parseChronySystemTimeOffset(value)
			if err != nil {
				return result, err
			}
			result.OffsetNS = offset
		case "Skew":
			haveSkew = true
			skew, err := parseChronyFloatPrefix(value, "skew")
			if err != nil {
				return result, err
			}
			result.SkewPPM = skew
		case "Leap status":
			haveLeapStatus = true
			result.LeapStatus = value
		}
	}
	if !haveReferenceID {
		return result, errors.New("chrony tracking missing Reference ID")
	}
	if !haveSystemTime {
		return result, errors.New("chrony tracking missing System time")
	}
	if !haveSkew {
		return result, errors.New("chrony tracking missing Skew")
	}
	if !haveLeapStatus {
		return result, errors.New("chrony tracking missing Leap status")
	}
	return result, nil
}

func chronyReferenceName(value string) string {
	start := strings.LastIndex(value, "(")
	end := strings.LastIndex(value, ")")
	if start >= 0 && end > start {
		return strings.TrimSpace(value[start+1 : end])
	}
	fields := strings.Fields(value)
	if len(fields) == 0 {
		return ""
	}
	if fields[0] == "7F7F0101" {
		return ""
	}
	return fields[0]
}

func parseChronySystemTimeOffset(value string) (int64, error) {
	fields := strings.Fields(value)
	if len(fields) < 3 {
		return 0, fmt.Errorf("parse chrony system time %q", value)
	}
	seconds, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return 0, fmt.Errorf("parse chrony system time %q: %w", value, err)
	}
	switch fields[2] {
	case "slow":
		seconds = -seconds
	case "fast":
	default:
		return 0, fmt.Errorf("parse chrony system time direction %q", value)
	}
	return int64(math.Round(seconds * float64(time.Second))), nil
}

func parseChronyFloatPrefix(value, field string) (float64, error) {
	fields := strings.Fields(value)
	if len(fields) == 0 {
		return 0, fmt.Errorf("parse chrony %s %q", field, value)
	}
	parsed, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return 0, fmt.Errorf("parse chrony %s %q: %w", field, value, err)
	}
	return parsed, nil
}

func lookupPasswdUser(name string) (int, int, error) {
	raw, err := os.ReadFile("/etc/passwd")
	if err != nil {
		return 0, 0, fmt.Errorf("read passwd: %w", err)
	}
	for _, line := range strings.Split(string(raw), "\n") {
		fields := strings.Split(line, ":")
		if len(fields) < 4 || fields[0] != name {
			continue
		}
		uid, uidErr := strconv.Atoi(fields[2])
		if uidErr != nil {
			return 0, 0, fmt.Errorf("parse uid for %s: %w", name, uidErr)
		}
		gid, gidErr := strconv.Atoi(fields[3])
		if gidErr != nil {
			return 0, 0, fmt.Errorf("parse gid for %s: %w", name, gidErr)
		}
		return uid, gid, nil
	}
	return 0, 0, fmt.Errorf("passwd user %s not found", name)
}
