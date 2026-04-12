package vmorchestrator

import (
	"context"
	"errors"
	"net/netip"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestDeriveLease(t *testing.T) {
	t.Parallel()

	pool := netip.MustParsePrefix("172.16.0.0/16")
	lease, err := deriveLease(pool, "job-1", 1)
	if err != nil {
		t.Fatalf("deriveLease: %v", err)
	}

	if lease.SubnetCIDR != "172.16.0.4/30" {
		t.Fatalf("unexpected subnet: %s", lease.SubnetCIDR)
	}
	if lease.HostCIDR != "172.16.0.5/30" {
		t.Fatalf("unexpected host cidr: %s", lease.HostCIDR)
	}
	if lease.GuestIP != "172.16.0.6" {
		t.Fatalf("unexpected guest IP: %s", lease.GuestIP)
	}
	if lease.GatewayIP != "172.16.0.5" {
		t.Fatalf("unexpected gateway IP: %s", lease.GatewayIP)
	}
	if lease.MAC != "06:fc:00:00:00:01" {
		t.Fatalf("unexpected MAC: %s", lease.MAC)
	}
}

func TestTapDeviceNameFitsKernelLimit(t *testing.T) {
	t.Parallel()

	name := tapDeviceName(1 << 20)
	if len(name) > 15 {
		t.Fatalf("tap name %q exceeds kernel limit", name)
	}
}

func TestAllocatorAcquireRelease(t *testing.T) {
	t.Parallel()

	allocator := testAllocator(t, "172.16.0.0/30")
	lease, err := allocator.Acquire(context.Background(), "job-1")
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if lease.SlotIndex != 0 {
		t.Fatalf("unexpected slot index: %d", lease.SlotIndex)
	}

	state, runID, err := readSlotState(allocator.cfg.StateDBPath, 0)
	if err != nil {
		t.Fatalf("readSlotState: %v", err)
	}
	if state != "allocated" || runID != "job-1" {
		t.Fatalf("slot state after acquire: state=%q run_id=%q", state, runID)
	}

	if err := allocator.Release(context.Background(), "job-1"); err != nil {
		t.Fatalf("Release: %v", err)
	}

	state, runID, err = readSlotState(allocator.cfg.StateDBPath, 0)
	if err != nil {
		t.Fatalf("readSlotState after release: %v", err)
	}
	if state != "free" || runID != "" {
		t.Fatalf("slot state after release: state=%q run_id=%q", state, runID)
	}
}

func TestAllocatorReusesExistingLeaseForJob(t *testing.T) {
	t.Parallel()

	allocator := testAllocator(t, "172.16.0.0/29")
	first, err := allocator.Acquire(context.Background(), "job-1")
	if err != nil {
		t.Fatalf("Acquire first: %v", err)
	}
	second, err := allocator.Acquire(context.Background(), "job-1")
	if err != nil {
		t.Fatalf("Acquire second: %v", err)
	}
	if first.SlotIndex != second.SlotIndex {
		t.Fatalf("expected same slot, got %d and %d", first.SlotIndex, second.SlotIndex)
	}
	if first.Generation != second.Generation {
		t.Fatalf("expected same generation, got %d and %d", first.Generation, second.Generation)
	}
}

func TestAllocatorNoSlots(t *testing.T) {
	t.Parallel()

	allocator := testAllocator(t, "172.16.0.0/30")
	if _, err := allocator.Acquire(context.Background(), "job-1"); err != nil {
		t.Fatalf("Acquire job-1: %v", err)
	}
	if _, err := allocator.Acquire(context.Background(), "job-2"); !errors.Is(err, ErrNoNetworkSlots) {
		t.Fatalf("expected ErrNoNetworkSlots, got %v", err)
	}
}

func TestAllocatorParallelUniqueSlots(t *testing.T) {
	t.Parallel()

	allocator := testAllocator(t, "172.16.0.0/24")
	const jobs = 24

	var (
		wg    sync.WaitGroup
		mu    sync.Mutex
		slots = make(map[int]string, jobs)
	)

	for i := 0; i < jobs; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			runID := "job-" + string(rune('a'+i))
			lease, err := allocator.Acquire(context.Background(), runID)
			if err != nil {
				t.Errorf("Acquire %s: %v", runID, err)
				return
			}

			mu.Lock()
			defer mu.Unlock()
			if owner, exists := slots[lease.SlotIndex]; exists {
				t.Errorf("slot %d allocated to both %s and %s", lease.SlotIndex, owner, runID)
				return
			}
			slots[lease.SlotIndex] = runID
		}(i)
	}

	wg.Wait()
	if len(slots) != jobs {
		t.Fatalf("expected %d unique slots, got %d", jobs, len(slots))
	}
}

func TestAllocatorRecoverRemovesStaleLease(t *testing.T) {
	t.Parallel()

	allocator := testAllocator(t, "172.16.0.0/29")
	if _, err := allocator.Acquire(context.Background(), "job-1"); err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if err := forceSlotCreatedAt(allocator.cfg.StateDBPath, 0, time.Now().UTC().Add(-pendingLeaseTTL-time.Minute)); err != nil {
		t.Fatalf("forceSlotCreatedAt: %v", err)
	}

	if err := allocator.Recover(context.Background(), nil); err != nil {
		t.Fatalf("Recover: %v", err)
	}

	state, runID, err := readSlotState(allocator.cfg.StateDBPath, 0)
	if err != nil {
		t.Fatalf("readSlotState: %v", err)
	}
	if state != "free" || runID != "" {
		t.Fatalf("expected stale lease to be released, got state=%q run_id=%q", state, runID)
	}
}

func TestAllocatorRecoverKeepsRecentPendingLease(t *testing.T) {
	t.Parallel()

	allocator := testAllocator(t, "172.16.0.0/29")
	if _, err := allocator.Acquire(context.Background(), "job-1"); err != nil {
		t.Fatalf("Acquire: %v", err)
	}

	if err := allocator.Recover(context.Background(), nil); err != nil {
		t.Fatalf("Recover: %v", err)
	}

	state, runID, err := readSlotState(allocator.cfg.StateDBPath, 0)
	if err != nil {
		t.Fatalf("readSlotState: %v", err)
	}
	if state != "allocated" || runID != "job-1" {
		t.Fatalf("expected recent pending lease to remain, got state=%q run_id=%q", state, runID)
	}
}

func TestAllocatorRecoverKeepsLivePID(t *testing.T) {
	t.Parallel()

	allocator := testAllocator(t, "172.16.0.0/29")
	if _, err := allocator.Acquire(context.Background(), "job-1"); err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if err := allocator.AttachPID(context.Background(), "job-1", os.Getpid()); err != nil {
		t.Fatalf("AttachPID: %v", err)
	}

	if err := allocator.Recover(context.Background(), nil); err != nil {
		t.Fatalf("Recover: %v", err)
	}

	state, runID, err := readSlotState(allocator.cfg.StateDBPath, 0)
	if err != nil {
		t.Fatalf("readSlotState: %v", err)
	}
	if state != "allocated" || runID != "job-1" {
		t.Fatalf("expected live lease to remain, got state=%q run_id=%q", state, runID)
	}
}

func TestAllocatorAttachPIDPersistsStartTicks(t *testing.T) {
	t.Parallel()

	allocator := testAllocator(t, "172.16.0.0/29")
	if _, err := allocator.Acquire(context.Background(), "job-1"); err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if err := allocator.AttachPID(context.Background(), "job-1", os.Getpid()); err != nil {
		t.Fatalf("AttachPID: %v", err)
	}

	pid, ticks, err := readSlotPIDMetadata(allocator.cfg.StateDBPath, 0)
	if err != nil {
		t.Fatalf("readSlotPIDMetadata: %v", err)
	}
	if pid != os.Getpid() {
		t.Fatalf("persisted pid = %d, want %d", pid, os.Getpid())
	}
	if ticks == 0 {
		t.Fatal("persisted process start ticks must be non-zero")
	}
}

func TestGuestNetworkConfig(t *testing.T) {
	t.Parallel()

	lease := NetworkLease{
		RunID:     "job-1",
		TapName:   "tap0",
		GuestIP:   "172.16.0.6",
		GatewayIP: "172.16.0.5",
	}

	cfg := lease.GuestNetworkConfig("10.255.0.1", 18080)
	if cfg.LinkName != defaultIf {
		t.Fatalf("link_name: got %q want %q", cfg.LinkName, defaultIf)
	}
	if cfg.AddressCIDR != "172.16.0.6/30" {
		t.Fatalf("address_cidr: got %q want %q", cfg.AddressCIDR, "172.16.0.6/30")
	}
	if cfg.Gateway != "172.16.0.5" {
		t.Fatalf("gateway: got %q want %q", cfg.Gateway, "172.16.0.5")
	}
	if cfg.HostServiceIP != "10.255.0.1" {
		t.Fatalf("host_service_ip: got %q want %q", cfg.HostServiceIP, "10.255.0.1")
	}
	if cfg.HostServicePort != 18080 {
		t.Fatalf("host_service_port: got %d want %d", cfg.HostServicePort, 18080)
	}
}

func testAllocator(t *testing.T, cidr string) *Allocator {
	t.Helper()
	stateDir := t.TempDir()
	return NewAllocator(NetworkPoolConfig{
		PoolCIDR:    cidr,
		StateDBPath: filepath.Join(stateDir, "state.db"),
	})
}

func readSlotState(stateDBPath string, slot int) (string, string, error) {
	db, err := openStateDB(stateDBPath)
	if err != nil {
		return "", "", err
	}
	defer db.Close()

	var state, runID string
	if err := db.QueryRow(
		`SELECT state, run_id FROM network_slots WHERE slot_index = ?`,
		slot,
	).Scan(&state, &runID); err != nil {
		return "", "", err
	}
	return state, runID, nil
}

func readSlotPIDMetadata(stateDBPath string, slot int) (int, uint64, error) {
	db, err := openStateDB(stateDBPath)
	if err != nil {
		return 0, 0, err
	}
	defer db.Close()

	var (
		pid   int
		ticks uint64
	)
	if err := db.QueryRow(
		`SELECT firecracker_pid, firecracker_start_ticks FROM network_slots WHERE slot_index = ?`,
		slot,
	).Scan(&pid, &ticks); err != nil {
		return 0, 0, err
	}
	return pid, ticks, nil
}

func forceSlotCreatedAt(stateDBPath string, slot int, when time.Time) error {
	db, err := openStateDB(stateDBPath)
	if err != nil {
		return err
	}
	defer db.Close()

	_, err = db.Exec(`UPDATE network_slots SET created_at_unix_nano = ?, updated_at_unix_nano = ? WHERE slot_index = ?`, when.UnixNano(), when.UnixNano(), slot)
	return err
}
