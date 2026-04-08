package vmorchestrator

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/forge-metal/vm-orchestrator/vmproto"
)

const (
	defaultGuestPoolCIDR = "172.16.0.0/16"
	defaultLeaseDir      = "/var/lib/ci/net/leases"
	defaultIf            = "eth0"
	pendingLeaseTTL      = 5 * time.Minute
)

var ErrNoNetworkSlots = errors.New("no network slots available")

// NetworkPoolConfig defines the host-managed pool used for Firecracker guests.
type NetworkPoolConfig struct {
	PoolCIDR      string
	LeaseDir      string
	HostInterface string
}

// NetworkLease is the persisted lease record for one Firecracker VM.
type NetworkLease struct {
	JobID        string    `json:"job_id"`
	SlotIndex    int       `json:"slot_index"`
	SubnetCIDR   string    `json:"subnet_cidr"`
	TapName      string    `json:"tap_name"`
	HostCIDR     string    `json:"host_cidr"`
	GuestIP      string    `json:"guest_ip"`
	GatewayIP    string    `json:"gateway_ip"`
	MAC          string    `json:"mac"`
	PID          int       `json:"pid,omitempty"`
	CreatedAtUTC time.Time `json:"created_at_utc"`
}

// Allocator manages persistent, host-wide network leases for Firecracker VMs.
type Allocator struct {
	cfg NetworkPoolConfig
}

// networkSetup is the runtime network configuration passed into Firecracker.
type networkSetup struct {
	Lease NetworkLease
}

func NewAllocator(cfg NetworkPoolConfig) *Allocator {
	return &Allocator{cfg: normalizeNetworkPoolConfig(cfg)}
}

func setupNetwork(ctx context.Context, jobID string, cfg NetworkPoolConfig, ops PrivOps) (*networkSetup, func(), error) {
	allocator := NewAllocator(cfg)
	if err := allocator.Recover(ctx, ops); err != nil {
		return nil, nil, fmt.Errorf("recover stale leases: %w", err)
	}

	lease, err := allocator.Acquire(ctx, jobID)
	if err != nil {
		return nil, nil, err
	}

	completedSteps := 0

	if err := ops.TapCreate(ctx, lease.TapName, lease.HostCIDR); err != nil {
		_ = allocator.Release(context.Background(), jobID)
		return nil, nil, fmt.Errorf("network setup create tap: %w", err)
	}
	completedSteps = 2 // TapCreate covers both tuntap add + addr add

	if err := ops.TapUp(ctx, lease.TapName); err != nil {
		cleanupNetworkOps(context.Background(), lease.TapName, completedSteps, ops)
		_ = allocator.Release(context.Background(), jobID)
		return nil, nil, fmt.Errorf("network setup link up: %w", err)
	}
	completedSteps = 3

	cleanup := func() {
		cleanupNetworkOps(context.Background(), lease.TapName, completedSteps, ops)
		_ = allocator.Release(context.Background(), jobID)
	}

	return &networkSetup{
		Lease: lease,
	}, cleanup, nil
}

func (l NetworkLease) GuestNetworkConfig() vmproto.NetworkConfig {
	return vmproto.NetworkConfig{
		AddressCIDR: fmt.Sprintf("%s/30", l.GuestIP),
		Gateway:     l.GatewayIP,
		LinkName:    defaultIf,
	}
}

// Acquire reserves a unique /30 slot for a Firecracker job.
func (a *Allocator) Acquire(ctx context.Context, jobID string) (NetworkLease, error) {
	if jobID == "" {
		return NetworkLease{}, errors.New("job ID is required")
	}
	pool, slotCount, err := a.pool()
	if err != nil {
		return NetworkLease{}, err
	}

	lockFile, releaseLock, err := a.acquireLock(ctx)
	if err != nil {
		return NetworkLease{}, err
	}
	defer lockFile.Close()
	defer func() { _ = releaseLock() }()

	existing, err := a.findLeaseByJobID(jobID)
	if err != nil {
		return NetworkLease{}, err
	}
	if existing != nil {
		return *existing, nil
	}

	for slot := 0; slot < slotCount; slot++ {
		path := a.leasePath(slot)
		if _, err := os.Stat(path); err == nil {
			continue
		} else if !errors.Is(err, os.ErrNotExist) {
			return NetworkLease{}, fmt.Errorf("stat lease %s: %w", path, err)
		}

		lease, err := deriveLease(pool, jobID, slot)
		if err != nil {
			return NetworkLease{}, err
		}
		if err := writeLeaseFile(path, lease); err != nil {
			return NetworkLease{}, err
		}
		return lease, nil
	}

	return NetworkLease{}, fmt.Errorf("%w in %s", ErrNoNetworkSlots, a.cfg.PoolCIDR)
}

// Release deletes the lease record for a Firecracker job. It is idempotent.
func (a *Allocator) Release(ctx context.Context, jobID string) error {
	lockFile, releaseLock, err := a.acquireLock(ctx)
	if err != nil {
		return err
	}
	defer lockFile.Close()
	defer func() { _ = releaseLock() }()

	lease, path, err := a.findLeaseFileByJobID(jobID)
	if err != nil {
		return err
	}
	if lease == nil {
		return nil
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove lease %s: %w", path, err)
	}
	return nil
}

// Recover reconciles stale lease files with live TAP devices and VM processes.
func (a *Allocator) Recover(ctx context.Context, ops PrivOps) error {
	lockFile, releaseLock, err := a.acquireLock(ctx)
	if err != nil {
		return err
	}
	defer lockFile.Close()
	defer func() { _ = releaseLock() }()

	entries, err := os.ReadDir(a.cfg.LeaseDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("read lease dir %s: %w", a.cfg.LeaseDir, err)
	}

	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}

		path := filepath.Join(a.cfg.LeaseDir, entry.Name())
		lease, err := readLeaseFile(path)
		if err != nil {
			return err
		}

		if lease.PID > 0 && processExists(lease.PID) {
			continue
		}

		tapExists := tapExists(lease.TapName)
		if lease.PID == 0 && time.Since(lease.CreatedAtUTC) < pendingLeaseTTL {
			continue
		}

		if tapExists {
			if ops == nil {
				return fmt.Errorf("cleanup stale tap %s: privileged ops are required", lease.TapName)
			}
			if cleanupErr := cleanupNetworkOps(ctx, lease.TapName, 3, ops); cleanupErr != nil {
				return cleanupErr
			}
		}

		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove stale lease %s: %w", path, err)
		}
	}

	return nil
}

// AttachPID records the Firecracker process PID for stale lease recovery.
func (a *Allocator) AttachPID(ctx context.Context, jobID string, pid int) error {
	lockFile, releaseLock, err := a.acquireLock(ctx)
	if err != nil {
		return err
	}
	defer lockFile.Close()
	defer func() { _ = releaseLock() }()

	lease, path, err := a.findLeaseFileByJobID(jobID)
	if err != nil {
		return err
	}
	if lease == nil {
		return fmt.Errorf("lease for job %s not found", jobID)
	}

	lease.PID = pid
	return writeLeaseFile(path, *lease)
}

func (a *Allocator) pool() (netip.Prefix, int, error) {
	pool, err := netip.ParsePrefix(a.cfg.PoolCIDR)
	if err != nil {
		return netip.Prefix{}, 0, fmt.Errorf("parse guest pool %s: %w", a.cfg.PoolCIDR, err)
	}
	pool = pool.Masked()
	if !pool.Addr().Is4() {
		return netip.Prefix{}, 0, fmt.Errorf("guest pool %s must be IPv4", a.cfg.PoolCIDR)
	}
	if pool.Bits() > 30 {
		return netip.Prefix{}, 0, fmt.Errorf("guest pool %s is too small for /30 slots", a.cfg.PoolCIDR)
	}

	slotBits := 30 - pool.Bits()
	if slotBits < 0 || slotBits > 30 {
		return netip.Prefix{}, 0, fmt.Errorf("guest pool %s yields invalid slot space", a.cfg.PoolCIDR)
	}

	return pool, 1 << slotBits, nil
}

func (a *Allocator) acquireLock(ctx context.Context) (*os.File, func() error, error) {
	if err := os.MkdirAll(a.cfg.LeaseDir, 0o755); err != nil {
		return nil, nil, fmt.Errorf("mkdir lease dir %s: %w", a.cfg.LeaseDir, err)
	}

	lockPath := filepath.Join(a.cfg.LeaseDir, ".lock")
	lockFile, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o664)
	if err != nil {
		return nil, nil, fmt.Errorf("open network lock %s: %w", lockPath, err)
	}

	for {
		if err := syscall.Flock(int(lockFile.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err == nil {
			return lockFile, func() error {
				return syscall.Flock(int(lockFile.Fd()), syscall.LOCK_UN)
			}, nil
		} else if err != syscall.EWOULDBLOCK {
			lockFile.Close()
			return nil, nil, fmt.Errorf("flock %s: %w", lockPath, err)
		}

		select {
		case <-ctx.Done():
			lockFile.Close()
			return nil, nil, ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}

func (a *Allocator) findLeaseByJobID(jobID string) (*NetworkLease, error) {
	lease, _, err := a.findLeaseFileByJobID(jobID)
	return lease, err
}

func (a *Allocator) findLeaseFileByJobID(jobID string) (*NetworkLease, string, error) {
	entries, err := os.ReadDir(a.cfg.LeaseDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, "", nil
		}
		return nil, "", fmt.Errorf("read lease dir %s: %w", a.cfg.LeaseDir, err)
	}

	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}

		path := filepath.Join(a.cfg.LeaseDir, entry.Name())
		lease, err := readLeaseFile(path)
		if err != nil {
			return nil, "", err
		}
		if lease.JobID == jobID {
			return &lease, path, nil
		}
	}

	return nil, "", nil
}

func (a *Allocator) leasePath(slot int) string {
	return filepath.Join(a.cfg.LeaseDir, fmt.Sprintf("%06d.json", slot))
}

func normalizeNetworkPoolConfig(cfg NetworkPoolConfig) NetworkPoolConfig {
	if cfg.PoolCIDR == "" {
		cfg.PoolCIDR = defaultGuestPoolCIDR
	}
	if cfg.LeaseDir == "" {
		cfg.LeaseDir = defaultLeaseDir
	}
	return cfg
}

func deriveLease(pool netip.Prefix, jobID string, slot int) (NetworkLease, error) {
	subnetPrefix, hostCIDR, guestIP, gatewayIP, err := slotAddrs(pool, slot)
	if err != nil {
		return NetworkLease{}, err
	}

	return NetworkLease{
		JobID:        jobID,
		SlotIndex:    slot,
		SubnetCIDR:   subnetPrefix.String(),
		TapName:      tapDeviceName(slot),
		HostCIDR:     hostCIDR,
		GuestIP:      guestIP.String(),
		GatewayIP:    gatewayIP.String(),
		MAC:          macForSlot(slot),
		CreatedAtUTC: time.Now().UTC(),
	}, nil
}

func slotAddrs(pool netip.Prefix, slot int) (netip.Prefix, string, netip.Addr, netip.Addr, error) {
	if slot < 0 {
		return netip.Prefix{}, "", netip.Addr{}, netip.Addr{}, fmt.Errorf("slot index %d is invalid", slot)
	}
	_, slotCount, err := (&Allocator{cfg: NetworkPoolConfig{PoolCIDR: pool.String()}}).pool()
	if err != nil {
		return netip.Prefix{}, "", netip.Addr{}, netip.Addr{}, err
	}
	if slot >= slotCount {
		return netip.Prefix{}, "", netip.Addr{}, netip.Addr{}, fmt.Errorf("slot index %d exceeds pool capacity %d", slot, slotCount)
	}

	base := ipv4ToUint32(pool.Addr())
	subnetBase := base + uint32(slot*4)
	hostAddr := uint32ToIPv4(subnetBase + 1)
	guestAddr := uint32ToIPv4(subnetBase + 2)
	subnetPrefix := netip.PrefixFrom(uint32ToIPv4(subnetBase), 30)
	return subnetPrefix, hostAddr.String() + "/30", guestAddr, hostAddr, nil
}

func macForSlot(slot int) string {
	return fmt.Sprintf("06:fc:%02x:%02x:%02x:%02x",
		byte(slot>>24),
		byte(slot>>16),
		byte(slot>>8),
		byte(slot),
	)
}

func tapDeviceName(slot int) string {
	return "fc-tap-" + strconv.FormatInt(int64(slot), 36)
}

func writeLeaseFile(path string, lease NetworkLease) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("mkdir lease dir for %s: %w", path, err)
	}
	data, err := json.MarshalIndent(lease, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal lease %s: %w", path, err)
	}
	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, append(data, '\n'), 0o644); err != nil {
		return fmt.Errorf("write lease %s: %w", tmpPath, err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("rename lease %s -> %s: %w", tmpPath, path, err)
	}
	return nil
}

func readLeaseFile(path string) (NetworkLease, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return NetworkLease{}, fmt.Errorf("read lease %s: %w", path, err)
	}
	var lease NetworkLease
	if err := json.Unmarshal(data, &lease); err != nil {
		return NetworkLease{}, fmt.Errorf("decode lease %s: %w", path, err)
	}
	return lease, nil
}

func cleanupNetworkOps(ctx context.Context, tapName string, steps int, ops PrivOps) error {
	if steps < 1 || tapName == "" {
		return nil
	}
	if !tapExists(tapName) {
		return nil
	}
	return ops.TapDelete(ctx, tapName)
}

func tapExists(name string) bool {
	if name == "" {
		return false
	}
	_, err := os.Stat(filepath.Join("/sys/class/net", name))
	return err == nil
}

func processExists(pid int) bool {
	if pid <= 0 {
		return false
	}
	_, err := os.Stat(filepath.Join("/proc", strconv.Itoa(pid)))
	return err == nil
}

// detectDefaultInterface finds the default route interface.
func detectDefaultInterface() string {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "ip", "route", "show", "default").Output()
	if err != nil {
		return defaultIf
	}
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

func ipv4ToUint32(addr netip.Addr) uint32 {
	bytes := addr.As4()
	return binary.BigEndian.Uint32(bytes[:])
}

func uint32ToIPv4(value uint32) netip.Addr {
	var bytes [4]byte
	binary.BigEndian.PutUint32(bytes[:], value)
	return netip.AddrFrom4(bytes)
}
