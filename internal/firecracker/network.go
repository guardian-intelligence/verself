package firecracker

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

const (
	// Fixed subnet for tracer bullet (one VM at a time).
	// Guest: 172.16.0.2/30, Host TAP: 172.16.0.1/30.
	tapIP      = "172.16.0.1/30"
	guestIP    = "172.16.0.2"
	guestCIDR  = "172.16.0.0/30"
	guestGW    = "172.16.0.1"
	guestMask  = "255.255.255.252"
	guestMAC   = "06:00:AC:10:00:02" // locally-administered, derived from 172.16.0.2
	defaultIf  = "eth0"              // host outbound interface (overridable)
)

// networkSetup holds the result of setting up VM networking.
type networkSetup struct {
	TapName string
	GuestIP string
	MAC     string
	// bootIPArg is the kernel command line arg for static IP config.
	// Format: ip=<client>::<gw>:<mask>::<device>:off
	BootIPArg string
}

// setupNetwork creates a TAP device, assigns an IP, and configures NAT.
// Returns the tap name and a cleanup function.
//
// For the tracer bullet: simple TAP + iptables NAT, one VM at a time.
// Phase 2: network namespaces with nftables for concurrent VMs.
func setupNetwork(ctx context.Context, jobID string, hostInterface string) (*networkSetup, func(), error) {
	if hostInterface == "" {
		hostInterface = detectDefaultInterface()
	}

	tapName := tapDeviceName(jobID)

	steps := []struct {
		name string
		args []string
	}{
		// Create TAP device
		{"create tap", []string{"ip", "tuntap", "add", tapName, "mode", "tap"}},
		// Assign IP to host side of TAP
		{"assign ip", []string{"ip", "addr", "add", tapIP, "dev", tapName}},
		// Bring TAP up
		{"link up", []string{"ip", "link", "set", tapName, "up"}},
		// Enable IP forwarding (idempotent)
		{"ip forward", []string{"sysctl", "-w", "net.ipv4.ip_forward=1"}},
		// NAT rule for guest traffic
		{"nat rule", []string{"iptables", "-t", "nat", "-A", "POSTROUTING",
			"-o", hostInterface, "-s", guestCIDR, "-j", "MASQUERADE"}},
	}

	completedSteps := 0
	for _, step := range steps {
		if err := runCmd(ctx, step.args[0], step.args[1:]...); err != nil {
			// Cleanup already-completed steps.
			cleanupNetwork(tapName, hostInterface, completedSteps)
			return nil, nil, fmt.Errorf("network setup %s: %w", step.name, err)
		}
		completedSteps++
	}

	cleanup := func() {
		cleanupNetwork(tapName, hostInterface, completedSteps)
	}

	bootIPArg := fmt.Sprintf("ip=%s::%s:%s::eth0:off", guestIP, guestGW, guestMask)

	return &networkSetup{
		TapName:   tapName,
		GuestIP:   guestIP,
		MAC:       guestMAC,
		BootIPArg: bootIPArg,
	}, cleanup, nil
}

// cleanupNetwork reverses network setup. Best-effort, logs errors.
func cleanupNetwork(tapName, hostInterface string, steps int) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if steps >= 5 {
		// Remove NAT rule
		runCmd(ctx, "iptables", "-t", "nat", "-D", "POSTROUTING",
			"-o", hostInterface, "-s", guestCIDR, "-j", "MASQUERADE")
	}
	if steps >= 1 {
		// Delete TAP device (also removes IP and brings it down)
		runCmd(ctx, "ip", "link", "del", tapName)
	}
}

// tapDeviceName returns a TAP device name for a job. Kernel limit is
// 15 characters for interface names, so we truncate the job ID.
func tapDeviceName(jobID string) string {
	name := "tap-" + jobID
	if len(name) > 15 {
		name = name[:15]
	}
	return name
}

// detectDefaultInterface finds the default route interface.
func detectDefaultInterface() string {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "ip", "route", "show", "default").Output()
	if err != nil {
		return defaultIf
	}
	// "default via 1.2.3.4 dev eth0 ..." -> extract field after "dev"
	fields := strings.Fields(string(out))
	for i, f := range fields {
		if f == "dev" && i+1 < len(fields) {
			return fields[i+1]
		}
	}
	return defaultIf
}

// runCmd executes a command with context. Used for ip/iptables/sysctl.
func runCmd(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %s: %s: %w", name, strings.Join(args, " "),
			strings.TrimSpace(string(out)), err)
	}
	return nil
}
