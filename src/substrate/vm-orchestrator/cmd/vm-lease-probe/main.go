// vm-lease-probe drives concurrent AcquireLease -> StartExec -> WaitExec ->
// ReleaseLease cycles against a vm-orchestrator socket and reports per-phase
// latencies. It is an operator load/health probe for the VM substrate; it does
// not go through sandbox-rental-service, billing, or GitHub.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	vmorchestrator "github.com/verself/vm-orchestrator"
)

type cycleResult struct {
	Index        int    `json:"index"`
	LeaseID      string `json:"lease_id,omitempty"`
	Activation   string `json:"activation_mode,omitempty"`
	AcquireMS    int64  `json:"acquire_ms"`
	ReadyMS      int64  `json:"ready_ms"`
	ExecMS       int64  `json:"exec_ms"`
	ReleaseMS    int64  `json:"release_ms"`
	ExitCode     int    `json:"exit_code"`
	Output       string `json:"output,omitempty"`
	Err          string `json:"error,omitempty"`
	FailedPhase  string `json:"failed_phase,omitempty"`
	TotalCycleMS int64  `json:"total_cycle_ms"`
}

func main() {
	socket := flag.String("socket", "/run/vm-orchestrator/api.sock", "vm-orchestrator unix socket")
	count := flag.Int("count", 1, "concurrent lease cycles")
	vcpus := flag.Uint("vcpus", 2, "vCPUs per lease")
	memoryMiB := flag.Uint("memory-mib", 1024, "guest RAM MiB per lease")
	rootDiskGiB := flag.Uint("root-disk-gib", 4, "root zvol GiB per lease")
	orgID := flag.String("org", "verself-substrate-probe", "storage namespace org id")
	quotaGiB := flag.Uint64("org-quota-gib", 128, "storage namespace quota GiB")
	command := flag.String("command", "uname -a", "command run inside each guest")
	ttlSeconds := flag.Uint64("ttl-seconds", 300, "lease TTL seconds")
	runID := flag.String("run-id", fmt.Sprintf("%d", time.Now().UnixNano()), "idempotency key prefix")
	golden := flag.Bool("golden", false, "also checkpoint a golden VM and restore from its snapshot")
	flag.Parse()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	client, err := vmorchestrator.NewClient(ctx, *socket)
	if err != nil {
		fatal(err)
	}
	defer client.Close()

	capacity, err := client.GetCapacity(ctx)
	if err != nil {
		fatal(fmt.Errorf("get capacity: %w", err))
	}
	emit("capacity", capacity)

	runtime, err := client.EnsureOrgRuntime(ctx, vmorchestrator.OrgRuntimeShape{
		StorageNamespace: vmorchestrator.StorageNamespace{
			OrgID:      *orgID,
			QuotaBytes: *quotaGiB << 30,
		},
	})
	if err != nil {
		fatal(fmt.Errorf("ensure org runtime: %w", err))
	}
	emit("org_runtime", runtime)

	results := make([]cycleResult, *count)
	var wg sync.WaitGroup
	for i := range *count {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i] = runCycle(ctx, client, i, cycleSpec{
				key:       fmt.Sprintf("probe-%s-%d", *runID, i),
				vcpus:     uint32(*vcpus),
				memoryMiB: uint32(*memoryMiB),
				rootGiB:   uint32(*rootDiskGiB),
				orgID:     *orgID,
				quota:     *quotaGiB << 30,
				command:   *command,
				ttl:       *ttlSeconds,
			})
		}(i)
	}
	wg.Wait()

	if *golden {
		emit("golden_roundtrip", goldenRoundtrip(ctx, client, cycleSpec{
			key:       fmt.Sprintf("probe-golden-%s", *runID),
			vcpus:     uint32(*vcpus),
			memoryMiB: uint32(*memoryMiB),
			rootGiB:   uint32(*rootDiskGiB),
			orgID:     *orgID,
			quota:     *quotaGiB << 30,
			ttl:       *ttlSeconds,
		}))
	}

	failures := 0
	acquire := make([]int64, 0, len(results))
	exec := make([]int64, 0, len(results))
	for _, r := range results {
		emit("cycle", r)
		if r.Err != "" {
			failures++
			continue
		}
		acquire = append(acquire, r.AcquireMS)
		exec = append(exec, r.ExecMS)
	}
	ready := make([]int64, 0, len(results))
	for _, r := range results {
		if r.Err == "" {
			ready = append(ready, r.ReadyMS)
		}
	}
	emit("summary", map[string]any{
		"count":          len(results),
		"failures":       failures,
		"acquire_ms_p50": percentile(acquire, 50),
		"acquire_ms_max": percentile(acquire, 100),
		"ready_ms_p50":   percentile(ready, 50),
		"ready_ms_max":   percentile(ready, 100),
		"exec_ms_p50":    percentile(exec, 50),
		"exec_ms_max":    percentile(exec, 100),
	})
	if failures > 0 {
		os.Exit(1)
	}
}

type cycleSpec struct {
	key       string
	vcpus     uint32
	memoryMiB uint32
	rootGiB   uint32
	orgID     string
	quota     uint64
	command   string
	ttl       uint64
}

func runCycle(ctx context.Context, client *vmorchestrator.Client, index int, spec cycleSpec) cycleResult {
	res := cycleResult{Index: index}
	cycleStart := time.Now()

	acquireStart := time.Now()
	lease, err := client.AcquireLease(ctx, spec.key, vmorchestrator.LeaseSpec{
		Resources: vmorchestrator.VMResources{
			VCPUs:       spec.vcpus,
			MemoryMiB:   spec.memoryMiB,
			RootDiskGiB: spec.rootGiB,
		},
		TTLSeconds:  spec.ttl,
		TrustClass:  "trusted",
		NetworkMode: "nat",
		StorageNamespace: vmorchestrator.StorageNamespace{
			OrgID:      spec.orgID,
			QuotaBytes: spec.quota,
		},
	})
	res.AcquireMS = time.Since(acquireStart).Milliseconds()
	if err != nil {
		res.Err = err.Error()
		res.FailedPhase = "acquire"
		return res
	}
	res.LeaseID = lease.LeaseID
	res.Activation = string(lease.Activation.Mode)
	defer func() {
		releaseStart := time.Now()
		if err := client.ReleaseLease(context.WithoutCancel(ctx), lease.LeaseID, spec.key); err != nil && res.Err == "" {
			res.Err = err.Error()
			res.FailedPhase = "release"
		}
		res.ReleaseMS = time.Since(releaseStart).Milliseconds()
		res.TotalCycleMS = time.Since(cycleStart).Milliseconds()
	}()

	readyStart := time.Now()
	ready, err := waitLeaseReady(ctx, client, lease.LeaseID)
	res.ReadyMS = time.Since(readyStart).Milliseconds()
	if err != nil {
		res.Err = err.Error()
		res.FailedPhase = "wait_ready"
		return res
	}
	res.Activation = string(ready.Activation.Mode)

	execStart := time.Now()
	started, err := client.StartExec(ctx, lease.LeaseID, spec.key, vmorchestrator.ExecSpec{
		Argv:           []string{"sh", "-c", spec.command},
		MaxWallSeconds: 120,
	})
	if err != nil {
		res.Err = err.Error()
		res.FailedPhase = "start_exec"
		return res
	}
	done, err := client.WaitExec(ctx, lease.LeaseID, started.ExecID, true)
	res.ExecMS = time.Since(execStart).Milliseconds()
	if err != nil {
		res.Err = err.Error()
		res.FailedPhase = "wait_exec"
		return res
	}
	res.ExitCode = done.ExitCode
	res.Output = done.Output
	if done.ExitCode != 0 {
		res.Err = fmt.Sprintf("exec exit code %d", done.ExitCode)
		res.FailedPhase = "exec"
	}
	return res
}

// goldenRoundtrip proves the warm path: boot a VM, leave a marker in guest
// memory state, checkpoint it as a golden snapshot, then activate a new lease
// from that snapshot and confirm the marker (and the running guest clock)
// survived the restore.
func goldenRoundtrip(ctx context.Context, client *vmorchestrator.Client, spec cycleSpec) map[string]any {
	out := map[string]any{}
	fail := func(phase string, err error) map[string]any {
		out["error"] = err.Error()
		out["failed_phase"] = phase
		return out
	}

	srcKey := spec.key + "-src"
	src, err := client.AcquireLease(ctx, srcKey, vmorchestrator.LeaseSpec{
		Resources:        vmorchestrator.VMResources{VCPUs: spec.vcpus, MemoryMiB: spec.memoryMiB, RootDiskGiB: spec.rootGiB},
		TTLSeconds:       spec.ttl,
		TrustClass:       "trusted",
		NetworkMode:      "nat",
		StorageNamespace: vmorchestrator.StorageNamespace{OrgID: spec.orgID, QuotaBytes: spec.quota},
	})
	if err != nil {
		return fail("acquire_source", err)
	}
	defer func() { _ = client.ReleaseLease(context.WithoutCancel(ctx), src.LeaseID, srcKey) }()
	if _, err := waitLeaseReady(ctx, client, src.LeaseID); err != nil {
		return fail("source_ready", err)
	}
	marker := "golden-" + spec.key
	exec, err := client.StartExec(ctx, src.LeaseID, srcKey, vmorchestrator.ExecSpec{
		Argv:           []string{"sh", "-c", fmt.Sprintf("echo %s > /tmp/golden-marker && cut -d. -f1 /proc/uptime", marker)},
		MaxWallSeconds: 60,
	})
	if err != nil {
		return fail("source_exec", err)
	}
	seeded, err := client.WaitExec(ctx, src.LeaseID, exec.ExecID, true)
	if err != nil || seeded.ExitCode != 0 {
		return fail("source_exec_wait", fmt.Errorf("exit=%d err=%v", seeded.ExitCode, err))
	}
	out["uptime_at_checkpoint"] = strings.TrimSpace(seeded.Output)

	checkpointStart := time.Now()
	checkpoint, err := client.CheckpointGoldenVM(ctx, src.LeaseID, srcKey, spec.key+"-op", spec.key+"-ckpt")
	out["checkpoint_ms"] = time.Since(checkpointStart).Milliseconds()
	if err != nil {
		return fail("checkpoint", err)
	}
	out["snapshot_key"] = checkpoint.SnapshotKey
	out["state_bytes"] = checkpoint.StateBytes
	out["memory_bytes"] = checkpoint.MemoryBytes

	restoreKey := spec.key + "-restore"
	restoreStart := time.Now()
	restored, err := client.AcquireLease(ctx, restoreKey, vmorchestrator.LeaseSpec{
		Resources:        vmorchestrator.VMResources{VCPUs: spec.vcpus, MemoryMiB: spec.memoryMiB, RootDiskGiB: spec.rootGiB},
		TTLSeconds:       spec.ttl,
		TrustClass:       "trusted",
		NetworkMode:      "nat",
		StorageNamespace: vmorchestrator.StorageNamespace{OrgID: spec.orgID, QuotaBytes: spec.quota},
		GoldenVM: vmorchestrator.GoldenVMActivation{
			SnapshotID:         checkpoint.CheckpointID,
			SnapshotKey:        checkpoint.SnapshotKey,
			RootSnapshotRef:    checkpoint.RootSnapshotRef,
			RootSnapshotGUID:   checkpoint.RootSnapshotGUID,
			VMStateArtifactRef: checkpoint.VMStateArtifactRef,
			MemoryArtifactRef:  checkpoint.MemoryArtifactRef,
		},
	})
	if err != nil {
		return fail("acquire_restore", err)
	}
	defer func() { _ = client.ReleaseLease(context.WithoutCancel(ctx), restored.LeaseID, restoreKey) }()
	ready, err := waitLeaseReady(ctx, client, restored.LeaseID)
	out["restore_ready_ms"] = time.Since(restoreStart).Milliseconds()
	if err != nil {
		return fail("restore_ready", err)
	}
	out["activation_mode"] = string(ready.Activation.Mode)

	verify, err := client.StartExec(ctx, restored.LeaseID, restoreKey, vmorchestrator.ExecSpec{
		Argv:           []string{"sh", "-c", "cat /tmp/golden-marker && cut -d. -f1 /proc/uptime"},
		MaxWallSeconds: 60,
	})
	if err != nil {
		return fail("restore_exec", err)
	}
	verified, err := client.WaitExec(ctx, restored.LeaseID, verify.ExecID, true)
	if err != nil || verified.ExitCode != 0 {
		return fail("restore_exec_wait", fmt.Errorf("exit=%d err=%v", verified.ExitCode, err))
	}
	out["restore_output"] = strings.TrimSpace(verified.Output)
	out["marker_survived"] = strings.Contains(verified.Output, marker)
	return out
}

func waitLeaseReady(ctx context.Context, client *vmorchestrator.Client, leaseID string) (vmorchestrator.LeaseRecord, error) {
	waitCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		lease, err := client.GetLease(waitCtx, leaseID)
		if err != nil {
			return vmorchestrator.LeaseRecord{}, fmt.Errorf("get lease while waiting for readiness: %w", err)
		}
		switch lease.State {
		case vmorchestrator.LeaseStateReady:
			return lease, nil
		case vmorchestrator.LeaseStateAcquiring:
		default:
			return vmorchestrator.LeaseRecord{}, fmt.Errorf("lease %s state %q before ready (reason %q)", leaseID, lease.State, lease.TerminalReason)
		}
		select {
		case <-waitCtx.Done():
			return vmorchestrator.LeaseRecord{}, waitCtx.Err()
		case <-ticker.C:
		}
	}
}

func percentile(values []int64, pct int) int64 {
	if len(values) == 0 {
		return -1
	}
	sorted := make([]int64, len(values))
	copy(sorted, values)
	sort.Slice(sorted, func(a, b int) bool { return sorted[a] < sorted[b] })
	if pct >= 100 {
		return sorted[len(sorted)-1]
	}
	idx := len(sorted) * pct / 100
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}

func emit(kind string, payload any) {
	body, err := json.Marshal(map[string]any{"kind": kind, "data": payload})
	if err != nil {
		fatal(err)
	}
	fmt.Println(string(body))
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
