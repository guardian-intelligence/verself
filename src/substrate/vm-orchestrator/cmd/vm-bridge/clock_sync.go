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
	"time"

	"github.com/verself/vm-orchestrator/vmproto"
)

const (
	chronydBin       = "/usr/sbin/chronyd"
	chronycBin       = "/usr/bin/chronyc"
	chronyConfigPath = "/etc/chrony/chrony.conf"
	chronyPTPDevice  = "/dev/ptp0"

	chronyStartGrace   = 200 * time.Millisecond
	chronycCommandWait = 15 * time.Second
	clockMaxOffsetNS   = int64(10 * time.Millisecond)
	clockMaxSkewPPM    = 100.0
)

func startChrony() error {
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
	for _, dir := range []string{"/run/chrony", "/var/lib/chrony"} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create %s: %w", dir, err)
		}
		if err := os.Chown(dir, chronyUID, chronyGID); err != nil {
			return fmt.Errorf("chown %s: %w", dir, err)
		}
	}
	if err := os.Chown(chronyPTPDevice, chronyUID, chronyGID); err != nil {
		return fmt.Errorf("chown %s: %w", chronyPTPDevice, err)
	}

	cmd := exec.Command(chronydBin, "-n", "-f", chronyConfigPath, "-L", "1")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start chronyd: %w", err)
	}

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	select {
	case err := <-done:
		if err == nil {
			return errors.New("chronyd exited during startup")
		}
		return fmt.Errorf("chronyd exited during startup: %w", err)
	case <-time.After(chronyStartGrace):
	}

	_, _ = fmt.Fprintf(os.Stdout, "%s chronyd started (pid=%d source=%s)\n", logPrefix, cmd.Process.Pid, chronyPTPDevice)
	go func() {
		err := <-done
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s FATAL: chronyd exited: %v\n", logPrefix, err)
		} else {
			fmt.Fprintf(os.Stderr, "%s FATAL: chronyd exited\n", logPrefix)
		}
		os.Exit(1)
	}()
	return nil
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
		result.TrackingRaw = strings.TrimSpace(out)
		result.WaitSyncMS = time.Since(started).Milliseconds()
		return result, fmt.Errorf("chrony source wait: %w", err)
	}
	if out, err := runChronyc("makestep"); err != nil {
		result.Status = "makestep_failed"
		result.TrackingRaw = strings.TrimSpace(out)
		result.WaitSyncMS = time.Since(started).Milliseconds()
		return result, fmt.Errorf("chrony makestep: %w", err)
	}
	if out, err := runChronyc("waitsync", "10", "0.010", strconv.FormatFloat(clockMaxSkewPPM, 'f', 0, 64), "1"); err != nil {
		result.Status = "sync_wait_failed"
		result.TrackingRaw = strings.TrimSpace(out)
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
	ctx, cancel := context.WithTimeout(context.Background(), chronycCommandWait)
	defer cancel()
	allArgs := append([]string{"-n"}, args...)
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
