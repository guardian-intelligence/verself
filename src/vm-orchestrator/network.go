package vmorchestrator

import (
	"context"
	"database/sql"
	"encoding/binary"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/forge-metal/vm-orchestrator/vmproto"
	"go.opentelemetry.io/otel/attribute"
)

const (
	defaultGuestPoolCIDR   = "172.16.0.0/16"
	defaultIf              = "eth0"
	pendingLeaseTTL        = 5 * time.Minute
	defaultHostServiceIP   = "10.255.0.1"
	defaultHostServicePort = 18080
)

var ErrNoNetworkSlots = errors.New("no network slots available")

type NetworkPoolConfig struct {
	PoolCIDR      string
	StateDBPath   string
	HostInterface string
	TapOwnerUID   int
	TapOwnerGID   int
}

type NetworkLease struct {
	LeaseID               string    `json:"lease_id"`
	SlotIndex             int       `json:"slot_index"`
	Generation            uint64    `json:"generation"`
	SubnetCIDR            string    `json:"subnet_cidr"`
	TapName               string    `json:"tap_name"`
	HostCIDR              string    `json:"host_cidr"`
	GuestIP               string    `json:"guest_ip"`
	GatewayIP             string    `json:"gateway_ip"`
	MAC                   string    `json:"mac"`
	FirecrackerPID        int       `json:"firecracker_pid,omitempty"`
	FirecrackerStartTicks uint64    `json:"firecracker_start_ticks,omitempty"`
	CreatedAtUTC          time.Time `json:"created_at_utc"`
}

type Allocator struct {
	cfg           NetworkPoolConfig
	tapExistsFunc func(string) bool
}

type networkSetup struct {
	Lease NetworkLease
}

func NewAllocator(cfg NetworkPoolConfig) *Allocator {
	return &Allocator{cfg: normalizeNetworkPoolConfig(cfg), tapExistsFunc: tapExists}
}

func setupNetwork(ctx context.Context, leaseID string, cfg NetworkPoolConfig, ops PrivOps) (*networkSetup, func(), error) {
	allocator := NewAllocator(cfg)
	acquireCtx, endAcquireSpan := startStepSpan(ctx, "vmorchestrator.network.acquire",
		attribute.String("lease.id", leaseID),
		attribute.String("network.pool_cidr", allocator.cfg.PoolCIDR),
	)
	lease, err := allocator.Acquire(acquireCtx, leaseID)
	endAcquireSpan(err)
	if err != nil {
		return nil, nil, err
	}

	if allocator.tapExists(lease.TapName) {
		recoverTapCtx, endRecoverTapSpan := startStepSpan(ctx, "vmorchestrator.network.recover_selected_tap",
			attribute.String("lease.id", leaseID),
			attribute.Int("network.slot_index", lease.SlotIndex),
			attribute.String("network.tap", lease.TapName),
		)
		cleanupErr := cleanupNetworkOpsIfPresent(recoverTapCtx, lease.TapName, 3, ops, allocator.tapExists)
		endRecoverTapSpan(cleanupErr)
		if cleanupErr != nil {
			_ = allocator.Release(context.Background(), leaseID)
			return nil, nil, fmt.Errorf("recover selected network tap %s: %w", lease.TapName, cleanupErr)
		}
	}

	completedSteps := 0
	tapCreateCtx, endTapCreateSpan := startStepSpan(ctx, "vmorchestrator.network.tap_create",
		attribute.String("lease.id", leaseID),
		attribute.String("network.tap", lease.TapName),
		attribute.String("network.host_cidr", lease.HostCIDR),
	)
	if err := ops.TapCreate(tapCreateCtx, lease.TapName, lease.HostCIDR, allocator.cfg.TapOwnerUID, allocator.cfg.TapOwnerGID); err != nil {
		endTapCreateSpan(err)
		_ = allocator.Release(context.Background(), leaseID)
		return nil, nil, fmt.Errorf("network setup create tap: %w", err)
	}
	endTapCreateSpan(nil)
	completedSteps = 2

	tapUpCtx, endTapUpSpan := startStepSpan(ctx, "vmorchestrator.network.tap_up",
		attribute.String("lease.id", leaseID),
		attribute.String("network.tap", lease.TapName),
	)
	if err := ops.TapUp(tapUpCtx, lease.TapName); err != nil {
		endTapUpSpan(err)
		cleanupNetworkOps(context.Background(), lease.TapName, completedSteps, ops)
		_ = allocator.Release(context.Background(), leaseID)
		return nil, nil, fmt.Errorf("network setup link up: %w", err)
	}
	endTapUpSpan(nil)
	completedSteps = 3

	cleanup := func() {
		cleanupNetworkOps(context.Background(), lease.TapName, completedSteps, ops)
		_ = allocator.Release(context.Background(), leaseID)
	}
	return &networkSetup{Lease: lease}, cleanup, nil
}

func (l NetworkLease) GuestNetworkConfig(hostServiceIP string, hostServicePort int) vmproto.NetworkConfig {
	if hostServiceIP == "" {
		hostServiceIP = defaultHostServiceIP
	}
	if hostServicePort == 0 {
		hostServicePort = defaultHostServicePort
	}
	return vmproto.NetworkConfig{
		AddressCIDR:     fmt.Sprintf("%s/30", l.GuestIP),
		Gateway:         l.GatewayIP,
		LinkName:        defaultIf,
		HostServiceIP:   hostServiceIP,
		HostServicePort: hostServicePort,
	}
}

func (a *Allocator) Acquire(ctx context.Context, leaseID string) (NetworkLease, error) {
	if leaseID == "" {
		return NetworkLease{}, errors.New("lease ID is required")
	}
	hostStateWriteMu.Lock()
	defer hostStateWriteMu.Unlock()
	pool, slotCount, err := a.pool()
	if err != nil {
		return NetworkLease{}, err
	}
	state, err := openHostStateStore(a.cfg.StateDBPath, nil)
	if err != nil {
		return NetworkLease{}, err
	}
	defer state.close()
	tx, err := state.db.BeginTx(ctx, nil)
	if err != nil {
		return NetworkLease{}, fmt.Errorf("begin acquire network tx: %w", err)
	}
	defer rollbackTx(tx)
	if err := ensureNetworkSlotRowsTx(ctx, tx, slotCount); err != nil {
		return NetworkLease{}, err
	}
	existing, err := selectAllocatedLeaseByLeaseTx(ctx, tx, leaseID)
	if err != nil {
		return NetworkLease{}, err
	}
	if existing != nil {
		if err := tx.Commit(); err != nil {
			return NetworkLease{}, fmt.Errorf("commit acquire existing network lease tx: %w", err)
		}
		return *existing, nil
	}
	slot, generation, err := selectFreeNetworkSlotTx(ctx, tx)
	if err != nil {
		return NetworkLease{}, err
	}
	if slot < 0 {
		return NetworkLease{}, fmt.Errorf("%w in %s", ErrNoNetworkSlots, a.cfg.PoolCIDR)
	}
	lease, err := deriveLease(pool, leaseID, slot)
	if err != nil {
		return NetworkLease{}, err
	}
	lease.Generation = generation + 1
	lease.CreatedAtUTC = time.Now().UTC()
	nowUnixNano := lease.CreatedAtUTC.UnixNano()
	res, err := tx.ExecContext(ctx, `UPDATE network_slots
		 SET generation = generation + 1,
		     state = 'allocated',
		     lease_id = ?,
		     tap_name = ?,
		     subnet_cidr = ?,
		     host_cidr = ?,
		     guest_ip = ?,
		     gateway_ip = ?,
		     mac = ?,
		     firecracker_pid = 0,
		     firecracker_start_ticks = 0,
		     created_at_unix_nano = ?,
		     updated_at_unix_nano = ?
		 WHERE slot_index = ? AND state = 'free'`,
		leaseID, lease.TapName, lease.SubnetCIDR, lease.HostCIDR, lease.GuestIP, lease.GatewayIP, lease.MAC, nowUnixNano, nowUnixNano, slot)
	if err != nil {
		return NetworkLease{}, fmt.Errorf("allocate network slot %d for lease %s: %w", slot, leaseID, err)
	}
	if rows, _ := res.RowsAffected(); rows != 1 {
		return NetworkLease{}, fmt.Errorf("allocate network slot %d for lease %s: expected 1 row, got %d", slot, leaseID, rows)
	}
	if err := tx.Commit(); err != nil {
		return NetworkLease{}, fmt.Errorf("commit acquire network tx: %w", err)
	}
	return lease, nil
}

func (a *Allocator) Release(ctx context.Context, leaseID string) error {
	hostStateWriteMu.Lock()
	defer hostStateWriteMu.Unlock()
	state, err := openHostStateStore(a.cfg.StateDBPath, nil)
	if err != nil {
		return err
	}
	defer state.close()
	tx, err := state.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin release network tx: %w", err)
	}
	defer rollbackTx(tx)
	nowUnixNano := time.Now().UTC().UnixNano()
	if _, err := tx.ExecContext(ctx, `UPDATE network_slots
		 SET generation = generation + 1,
		     state = 'free',
		     lease_id = '',
		     firecracker_pid = 0,
		     firecracker_start_ticks = 0,
		     updated_at_unix_nano = ?
		 WHERE lease_id = ? AND state = 'allocated'`, nowUnixNano, leaseID); err != nil {
		return fmt.Errorf("release network lease %s: %w", leaseID, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit release network lease %s: %w", leaseID, err)
	}
	return nil
}

func (a *Allocator) Recover(ctx context.Context, ops PrivOps) error {
	hostStateWriteMu.Lock()
	defer hostStateWriteMu.Unlock()
	pool, slotCount, err := a.pool()
	if err != nil {
		return err
	}
	_ = pool
	state, err := openHostStateStore(a.cfg.StateDBPath, nil)
	if err != nil {
		return err
	}
	defer state.close()
	tx, err := state.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin network recover tx: %w", err)
	}
	if err := ensureNetworkSlotRowsTx(ctx, tx, slotCount); err != nil {
		rollbackTx(tx)
		return err
	}
	allocated, err := listAllocatedNetworkLeasesTx(ctx, tx)
	if err != nil {
		rollbackTx(tx)
		return err
	}
	freeTaps, err := listFreeNetworkSlotsWithTapTx(ctx, tx)
	if err != nil {
		rollbackTx(tx)
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit network recover scan tx: %w", err)
	}
	now := time.Now().UTC()
	for _, lease := range allocated {
		if lease.FirecrackerPID > 0 && processMatchesStartTicks(lease.FirecrackerPID, lease.FirecrackerStartTicks) {
			continue
		}
		if lease.FirecrackerPID == 0 && now.Sub(lease.CreatedAtUTC) < pendingLeaseTTL {
			continue
		}
		if a.tapExists(lease.TapName) {
			if ops == nil {
				return fmt.Errorf("cleanup stale tap %s: privileged ops are required", lease.TapName)
			}
			if err := cleanupNetworkOpsIfPresent(ctx, lease.TapName, 3, ops, a.tapExists); err != nil {
				return err
			}
		}
		releaseTx, err := state.db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("begin recover release network slot tx: %w", err)
		}
		_, err = releaseTx.ExecContext(ctx, `UPDATE network_slots
			 SET generation = generation + 1,
			     state = 'free',
			     lease_id = '',
			     firecracker_pid = 0,
			     firecracker_start_ticks = 0,
			     updated_at_unix_nano = ?
			 WHERE slot_index = ? AND generation = ? AND state = 'allocated'`,
			now.UnixNano(), lease.SlotIndex, lease.Generation)
		if err != nil {
			rollbackTx(releaseTx)
			return fmt.Errorf("clear stale network slot %d: %w", lease.SlotIndex, err)
		}
		if err := releaseTx.Commit(); err != nil {
			return fmt.Errorf("commit recover release network slot %d: %w", lease.SlotIndex, err)
		}
	}
	for _, slot := range freeTaps {
		if slot.TapName == "" {
			slot.TapName = tapDeviceName(slot.SlotIndex)
		}
		if !a.tapExists(slot.TapName) {
			continue
		}
		if ops == nil {
			return fmt.Errorf("cleanup stale free tap %s: privileged ops are required", slot.TapName)
		}
		cleanupCtx, endCleanupSpan := startStepSpan(ctx, "vmorchestrator.network.recover_free_tap",
			attribute.Int("network.slot_index", slot.SlotIndex),
			attribute.Int64("network.generation", int64(slot.Generation)),
			attribute.String("network.tap", slot.TapName),
		)
		cleanupErr := cleanupNetworkOpsIfPresent(cleanupCtx, slot.TapName, 3, ops, a.tapExists)
		endCleanupSpan(cleanupErr)
		if cleanupErr != nil {
			return cleanupErr
		}
	}
	return nil
}

func (a *Allocator) AttachPID(ctx context.Context, leaseID string, pid int) error {
	startTicks, err := processStartTicks(pid)
	if err != nil {
		return fmt.Errorf("read process start ticks for pid %d: %w", pid, err)
	}
	hostStateWriteMu.Lock()
	defer hostStateWriteMu.Unlock()
	state, err := openHostStateStore(a.cfg.StateDBPath, nil)
	if err != nil {
		return err
	}
	defer state.close()
	tx, err := state.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin attach pid tx: %w", err)
	}
	defer rollbackTx(tx)
	nowUnixNano := time.Now().UTC().UnixNano()
	res, err := tx.ExecContext(ctx, `UPDATE network_slots
		 SET firecracker_pid = ?,
		     firecracker_start_ticks = ?,
		     updated_at_unix_nano = ?
		 WHERE lease_id = ? AND state = 'allocated'`,
		pid, startTicks, nowUnixNano, leaseID)
	if err != nil {
		return fmt.Errorf("attach pid for lease %s: %w", leaseID, err)
	}
	if rows, _ := res.RowsAffected(); rows == 0 {
		return fmt.Errorf("network lease %s not found", leaseID)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit attach pid for lease %s: %w", leaseID, err)
	}
	return nil
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
	return pool, 1 << (30 - pool.Bits()), nil
}

func totalGuestSlots(poolCIDR string) int {
	_, slots, err := (&Allocator{cfg: NetworkPoolConfig{PoolCIDR: poolCIDR}}).pool()
	if err != nil {
		return 0
	}
	return slots
}

func ensureNetworkSlotRowsTx(ctx context.Context, tx *sql.Tx, slotCount int) error {
	var existing int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM network_slots`).Scan(&existing); err != nil {
		return fmt.Errorf("count network slot rows: %w", err)
	}
	if existing < slotCount {
		for slot := existing; slot < slotCount; slot++ {
			if _, err := tx.ExecContext(ctx, `INSERT INTO network_slots (slot_index, generation, state, tap_name) VALUES (?, 0, 'free', ?) ON CONFLICT(slot_index) DO NOTHING`, slot, tapDeviceName(slot)); err != nil {
				return fmt.Errorf("ensure network slot row %d: %w", slot, err)
			}
		}
	}
	var activeBeyond int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM network_slots WHERE slot_index >= ? AND state = 'allocated'`, slotCount).Scan(&activeBeyond); err != nil {
		return fmt.Errorf("count allocated slots outside pool: %w", err)
	}
	if activeBeyond > 0 {
		return fmt.Errorf("guest pool shrink would orphan %d allocated slots", activeBeyond)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM network_slots WHERE slot_index >= ?`, slotCount); err != nil {
		return fmt.Errorf("trim free slots outside pool: %w", err)
	}
	return nil
}

func selectAllocatedLeaseByLeaseTx(ctx context.Context, tx *sql.Tx, leaseID string) (*NetworkLease, error) {
	row := tx.QueryRowContext(ctx, `SELECT slot_index, generation, subnet_cidr, tap_name, host_cidr, guest_ip, gateway_ip, mac, firecracker_pid, firecracker_start_ticks, created_at_unix_nano FROM network_slots WHERE state = 'allocated' AND lease_id = ?`, leaseID)
	var lease NetworkLease
	var createdUnixNS int64
	if err := row.Scan(&lease.SlotIndex, &lease.Generation, &lease.SubnetCIDR, &lease.TapName, &lease.HostCIDR, &lease.GuestIP, &lease.GatewayIP, &lease.MAC, &lease.FirecrackerPID, &lease.FirecrackerStartTicks, &createdUnixNS); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("query existing network lease %s: %w", leaseID, err)
	}
	lease.LeaseID = leaseID
	lease.CreatedAtUTC = unixNanoTime(createdUnixNS)
	return &lease, nil
}

func selectFreeNetworkSlotTx(ctx context.Context, tx *sql.Tx) (int, uint64, error) {
	row := tx.QueryRowContext(ctx, `SELECT slot_index, generation FROM network_slots WHERE state = 'free' ORDER BY slot_index ASC LIMIT 1`)
	var slot int
	var generation uint64
	if err := row.Scan(&slot, &generation); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return -1, 0, nil
		}
		return -1, 0, fmt.Errorf("query free network slot: %w", err)
	}
	return slot, generation, nil
}

func listAllocatedNetworkLeasesTx(ctx context.Context, tx *sql.Tx) ([]NetworkLease, error) {
	rows, err := tx.QueryContext(ctx, `SELECT lease_id, slot_index, generation, subnet_cidr, tap_name, host_cidr, guest_ip, gateway_ip, mac, firecracker_pid, firecracker_start_ticks, created_at_unix_nano FROM network_slots WHERE state = 'allocated'`)
	if err != nil {
		return nil, fmt.Errorf("query allocated network slots: %w", err)
	}
	defer rows.Close()
	out := []NetworkLease{}
	for rows.Next() {
		var lease NetworkLease
		var createdUnixNS int64
		if err := rows.Scan(&lease.LeaseID, &lease.SlotIndex, &lease.Generation, &lease.SubnetCIDR, &lease.TapName, &lease.HostCIDR, &lease.GuestIP, &lease.GatewayIP, &lease.MAC, &lease.FirecrackerPID, &lease.FirecrackerStartTicks, &createdUnixNS); err != nil {
			return nil, fmt.Errorf("scan allocated network slot: %w", err)
		}
		lease.CreatedAtUTC = unixNanoTime(createdUnixNS)
		out = append(out, lease)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate allocated network slots: %w", err)
	}
	return out, nil
}

type freeNetworkSlotTap struct {
	SlotIndex  int
	Generation uint64
	TapName    string
}

func listFreeNetworkSlotsWithTapTx(ctx context.Context, tx *sql.Tx) ([]freeNetworkSlotTap, error) {
	rows, err := tx.QueryContext(ctx, `SELECT slot_index, generation, tap_name FROM network_slots WHERE state = 'free' ORDER BY slot_index ASC`)
	if err != nil {
		return nil, fmt.Errorf("query free network slots: %w", err)
	}
	defer rows.Close()
	out := []freeNetworkSlotTap{}
	for rows.Next() {
		var slot freeNetworkSlotTap
		if err := rows.Scan(&slot.SlotIndex, &slot.Generation, &slot.TapName); err != nil {
			return nil, fmt.Errorf("scan free network slot: %w", err)
		}
		if slot.TapName == "" {
			slot.TapName = tapDeviceName(slot.SlotIndex)
		}
		out = append(out, slot)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate free network slots: %w", err)
	}
	return out, nil
}

func normalizeNetworkPoolConfig(cfg NetworkPoolConfig) NetworkPoolConfig {
	if cfg.PoolCIDR == "" {
		cfg.PoolCIDR = defaultGuestPoolCIDR
	}
	if cfg.StateDBPath == "" {
		cfg.StateDBPath = defaultStateDBPath
	}
	return cfg
}

func deriveLease(pool netip.Prefix, leaseID string, slot int) (NetworkLease, error) {
	subnetPrefix, hostCIDR, guestIP, gatewayIP, err := slotAddrs(pool, slot)
	if err != nil {
		return NetworkLease{}, err
	}
	return NetworkLease{
		LeaseID:    leaseID,
		SlotIndex:  slot,
		SubnetCIDR: subnetPrefix.String(),
		TapName:    tapDeviceName(slot),
		HostCIDR:   hostCIDR,
		GuestIP:    guestIP.String(),
		GatewayIP:  gatewayIP.String(),
		MAC:        macForSlot(slot),
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
	return fmt.Sprintf("06:fc:%02x:%02x:%02x:%02x", byte(slot>>24), byte(slot>>16), byte(slot>>8), byte(slot))
}

func tapDeviceName(slot int) string {
	return "fc-tap-" + strconv.FormatInt(int64(slot), 36)
}

func cleanupNetworkOps(ctx context.Context, tapName string, steps int, ops PrivOps) error {
	return cleanupNetworkOpsIfPresent(ctx, tapName, steps, ops, tapExists)
}

func cleanupNetworkOpsIfPresent(ctx context.Context, tapName string, steps int, ops PrivOps, tapExistsFunc func(string) bool) error {
	if steps < 1 || tapName == "" {
		return nil
	}
	if tapExistsFunc == nil {
		tapExistsFunc = tapExists
	}
	if !tapExistsFunc(tapName) {
		return nil
	}
	return ops.TapDelete(ctx, tapName)
}

func (a *Allocator) tapExists(name string) bool {
	if a == nil || a.tapExistsFunc == nil {
		return tapExists(name)
	}
	return a.tapExistsFunc(name)
}

func tapExists(name string) bool {
	if name == "" {
		return false
	}
	_, err := os.Stat(filepath.Join("/sys/class/net", name))
	return err == nil
}

func processMatchesStartTicks(pid int, expectedStartTicks uint64) bool {
	if pid <= 0 || expectedStartTicks == 0 {
		return false
	}
	startTicks, err := processStartTicks(pid)
	if err != nil {
		return false
	}
	return startTicks == expectedStartTicks
}

func processStartTicks(pid int) (uint64, error) {
	if pid <= 0 {
		return 0, fmt.Errorf("pid must be positive")
	}
	data, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "stat"))
	if err != nil {
		return 0, err
	}
	line := strings.TrimSpace(string(data))
	closing := strings.LastIndex(line, ")")
	if closing == -1 || closing+2 >= len(line) {
		return 0, fmt.Errorf("unexpected /proc stat format for pid %d", pid)
	}
	rest := strings.Fields(line[closing+2:])
	const startTicksIndexInRest = 19
	if len(rest) <= startTicksIndexInRest {
		return 0, fmt.Errorf("unexpected /proc stat field count for pid %d", pid)
	}
	value, err := strconv.ParseUint(rest[startTicksIndexInRest], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse process start ticks for pid %d: %w", pid, err)
	}
	return value, nil
}

func runCmd(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %s: %s: %w", name, strings.Join(args, " "), strings.TrimSpace(string(out)), err)
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
