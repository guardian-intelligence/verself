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

	if err := allocator.Release(context.Background(), "job-1"); err != nil {
		t.Fatalf("Release: %v", err)
	}

	if _, err := os.Stat(filepath.Join(allocator.cfg.LeaseDir, "000000.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected lease file to be removed, got err=%v", err)
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
			jobID := "job-" + string(rune('a'+i))
			lease, err := allocator.Acquire(context.Background(), jobID)
			if err != nil {
				t.Errorf("Acquire %s: %v", jobID, err)
				return
			}

			mu.Lock()
			defer mu.Unlock()
			if owner, exists := slots[lease.SlotIndex]; exists {
				t.Errorf("slot %d allocated to both %s and %s", lease.SlotIndex, owner, jobID)
				return
			}
			slots[lease.SlotIndex] = jobID
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
	lease, err := allocator.Acquire(context.Background(), "job-1")
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	lease.CreatedAtUTC = time.Now().UTC().Add(-pendingLeaseTTL - time.Minute)
	if err := writeLeaseFile(filepath.Join(allocator.cfg.LeaseDir, "000000.json"), lease); err != nil {
		t.Fatalf("writeLeaseFile: %v", err)
	}

	if err := allocator.Recover(context.Background(), nil); err != nil {
		t.Fatalf("Recover: %v", err)
	}

	if _, err := os.Stat(filepath.Join(allocator.cfg.LeaseDir, "000000.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected stale lease to be removed, got err=%v lease=%+v", err, lease)
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

	if _, err := os.Stat(filepath.Join(allocator.cfg.LeaseDir, "000000.json")); err != nil {
		t.Fatalf("expected recent pending lease to remain, got err=%v", err)
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

	if _, err := os.Stat(filepath.Join(allocator.cfg.LeaseDir, "000000.json")); err != nil {
		t.Fatalf("expected live lease to remain, got err=%v", err)
	}
}

func TestGuestNetworkConfig(t *testing.T) {
	t.Parallel()

	lease := NetworkLease{
		JobID:     "job-1",
		TapName:   "tap0",
		GuestIP:   "172.16.0.6",
		GatewayIP: "172.16.0.5",
	}

	cfg := lease.GuestNetworkConfig()
	if cfg.LinkName != defaultIf {
		t.Fatalf("link_name: got %q want %q", cfg.LinkName, defaultIf)
	}
	if cfg.AddressCIDR != "172.16.0.6/30" {
		t.Fatalf("address_cidr: got %q want %q", cfg.AddressCIDR, "172.16.0.6/30")
	}
	if cfg.Gateway != "172.16.0.5" {
		t.Fatalf("gateway: got %q want %q", cfg.Gateway, "172.16.0.5")
	}
}

func testAllocator(t *testing.T, cidr string) *Allocator {
	t.Helper()
	return NewAllocator(NetworkPoolConfig{
		PoolCIDR: cidr,
		LeaseDir: t.TempDir(),
	})
}
