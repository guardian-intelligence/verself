//go:build integration

package firecracker

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
)

const (
	smelterRequestMagic   uint32 = 0x48534d00
	smelterRequestVersion uint16 = 1
	smelterRequestSize           = 32

	smelterPacketMagic       uint32 = 0x48534d01
	smelterPacketVersion     uint16 = 1
	smelterPacketSize               = 176
	smelterPacketPayloadSize        = 128

	smelterRequestPing     uint16 = 1
	smelterRequestSnapshot uint16 = 2

	smelterPacketPong        uint16 = 1
	smelterPacketHello       uint16 = 2
	smelterPacketSample      uint16 = 3
	smelterPacketSnapshotEnd uint16 = 4

	smelterFrameMagic   uint32 = 0x46505600
	smelterFrameVersion uint16 = 1
	smelterFrameHello   uint16 = 1
	smelterFrameSample  uint16 = 2
)

type smelterSnapshot struct {
	VMs map[string]*smelterVM
}

type smelterVM struct {
	JobID            string
	StreamGeneration uint32
	Hello            *smelterHello
	Sample           *smelterSample
}

type smelterHello struct {
	Seq        uint32
	Flags      uint32
	MonoNS     uint64
	WallNS     uint64
	BootID     string
	MemTotalKB uint64
}

type smelterSample struct {
	Seq            uint32
	Flags          uint32
	MonoNS         uint64
	WallNS         uint64
	CPUUserTicks   uint64
	CPUSystemTicks uint64
	CPUIdleTicks   uint64
	Load1Centis    uint32
	Load5Centis    uint32
	Load15Centis   uint32
	ProcsRunning   uint16
	ProcsBlocked   uint16
	MemAvailableKB uint64
	IOReadBytes    uint64
	IOWriteBytes   uint64
	NetRXBytes     uint64
	NetTXBytes     uint64
	PSICPUPct100   uint16
	PSIMemPct100   uint16
	PSIIOPct100    uint16
}

type smelterPacket struct {
	Kind             uint16
	HostSeq          uint64
	ObservedWallNS   uint64
	JobID            uuid.UUID
	StreamGeneration uint32
	Flags            uint32
	Payload          [smelterPacketPayloadSize]byte
}

func TestHomesteadSmelterReportsRunningVMs(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Fatal("integration test requires root; run via sudo")
	}
	if _, err := exec.LookPath("zig"); err != nil {
		t.Skip("zig not available in PATH")
	}

	cfg := DefaultConfig()
	cfg.VCPUs = 1
	cfg.MemoryMiB = 512

	requireFirecrackerIntegrationPrereqs(t, cfg)

	smelterBin := buildHomesteadSmelterHostBinary(t)
	controlSock := filepath.Join(t.TempDir(), "homestead-smelter.sock")
	jailerScanRoot := filepath.Join(cfg.JailerRoot, "firecracker")

	var hostLogs bytes.Buffer
	smelterCmd := exec.Command(smelterBin,
		"serve",
		"--listen-uds", controlSock,
		"--jailer-root", jailerScanRoot,
		"--port", "10790",
	)
	smelterCmd.Stdout = &hostLogs
	smelterCmd.Stderr = &hostLogs
	if err := smelterCmd.Start(); err != nil {
		t.Fatalf("start homestead-smelter host: %v", err)
	}
	defer stopProcess(t, smelterCmd)

	waitForSmelterPing(t, controlSock, 10*time.Second, &hostLogs)

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	orch := New(cfg, logger)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	jobIDs := []string{uuid.NewString(), uuid.NewString()}
	results := make(chan runOutcome, len(jobIDs))

	for _, jobID := range jobIDs {
		job := JobConfig{
			JobID:      jobID,
			RunCommand: []string{"sh", "-lc", "echo smelter-start && sleep 6 && echo smelter-done"},
			RunWorkDir: "/workspace",
			Env: map[string]string{
				"CI": "true",
			},
		}

		go func(job JobConfig) {
			result, err := orch.Run(ctx, job)
			results <- runOutcome{jobID: job.JobID, result: result, err: err}
		}(job)
	}

	if _, err := waitForRunningJobLeases(ctx, cfg.NetworkLeaseDir, jobIDs, len(jobIDs)); err != nil {
		cancel()
		t.Fatalf("wait for running VMs: %v", err)
	}

	snapshot := waitForSmelterJobs(t, ctx, controlSock, jobIDs, &hostLogs)
	for _, jobID := range jobIDs {
		vm, ok := smelterVMByID(snapshot, jobID)
		if !ok {
			t.Fatalf("snapshot missing job %s: %+v", jobID, snapshot)
		}
		if vm.StreamGeneration == 0 {
			t.Fatalf("snapshot reported zero stream generation for %s: %+v", jobID, vm)
		}
		if vm.Hello == nil || vm.Sample == nil {
			t.Fatalf("snapshot missing telemetry for %s: %+v", jobID, vm)
		}
		if vm.Hello.BootID == "" || vm.Hello.MemTotalKB == 0 {
			t.Fatalf("snapshot reported invalid hello frame for %s: %+v", jobID, vm.Hello)
		}
		if vm.Sample.Seq == 0 {
			t.Fatalf("snapshot sample sequence did not advance for %s: %+v", jobID, vm.Sample)
		}
		if vm.Sample.MemAvailableKB == 0 {
			t.Fatalf("snapshot reported invalid memory sample for %s: %+v", jobID, vm.Sample)
		}
		if vm.Sample.WallNS == 0 || vm.Sample.MonoNS == 0 {
			t.Fatalf("snapshot reported invalid timestamps for %s: %+v", jobID, vm.Sample)
		}
	}

	for range jobIDs {
		outcome := <-results
		if outcome.err != nil {
			t.Fatalf("job %s failed: %v", outcome.jobID, outcome.err)
		}
		if outcome.result.ExitCode != 0 {
			t.Fatalf("job %s exited with %d\nlogs:\n%s\nhost logs:\n%s", outcome.jobID, outcome.result.ExitCode, outcome.result.Logs, hostLogs.String())
		}
	}
}

func buildHomesteadSmelterHostBinary(t *testing.T) string {
	t.Helper()

	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}
	smelterDir := filepath.Clean(filepath.Join(wd, "..", "..", "homestead-smelter"))

	cmd := exec.Command("zig", "build", "-Doptimize=ReleaseSafe", "-Dtarget=x86_64-linux-musl")
	cmd.Dir = smelterDir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build homestead-smelter host: %v\n%s", err, string(output))
	}

	return filepath.Join(smelterDir, "zig-out", "bin", "homestead-smelter-host")
}

func stopProcess(t *testing.T, cmd *exec.Cmd) {
	t.Helper()
	if cmd.Process == nil {
		return
	}
	_ = cmd.Process.Kill()
	_, _ = cmd.Process.Wait()
}

func waitForSmelterPing(t *testing.T, controlSock string, timeout time.Duration, hostLogs *bytes.Buffer) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("unix", controlSock, 250*time.Millisecond)
		if err != nil {
			time.Sleep(100 * time.Millisecond)
			continue
		}

		if err := smelterWriteRequest(conn, smelterRequestPing); err != nil {
			conn.Close()
			time.Sleep(100 * time.Millisecond)
			continue
		}

		packet, err := smelterReadPacket(conn)
		conn.Close()
		if err != nil {
			time.Sleep(100 * time.Millisecond)
			continue
		}
		if packet.Kind == smelterPacketPong {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}

	t.Fatalf("homestead-smelter host did not answer ping\nhost logs:\n%s", hostLogs.String())
}

func waitForSmelterJobs(t *testing.T, ctx context.Context, controlSock string, jobIDs []string, hostLogs *bytes.Buffer) smelterSnapshot {
	t.Helper()

	for {
		snapshot, err := smelterSnapshotRequest(controlSock)
		if err == nil && snapshotContainsJobs(snapshot, jobIDs) {
			return snapshot
		}

		select {
		case <-ctx.Done():
			if err != nil {
				t.Fatalf("wait for smelter snapshot: %v\nhost logs:\n%s", err, hostLogs.String())
			}
			t.Fatalf("wait for smelter snapshot timed out: %+v\nhost logs:\n%s", snapshot, hostLogs.String())
		case <-time.After(250 * time.Millisecond):
		}
	}
}

func smelterSnapshotRequest(controlSock string) (smelterSnapshot, error) {
	conn, err := net.DialTimeout("unix", controlSock, 500*time.Millisecond)
	if err != nil {
		return smelterSnapshot{}, err
	}
	defer conn.Close()

	if err := smelterWriteRequest(conn, smelterRequestSnapshot); err != nil {
		return smelterSnapshot{}, err
	}

	snapshot := smelterSnapshot{VMs: map[string]*smelterVM{}}
	for {
		packet, err := smelterReadPacket(conn)
		if err != nil {
			return smelterSnapshot{}, err
		}

		switch packet.Kind {
		case smelterPacketHello:
			hello, err := decodeSmelterHello(packet.Payload)
			if err != nil {
				return smelterSnapshot{}, err
			}
			vm := snapshot.ensureVM(packet.JobID.String())
			vm.StreamGeneration = packet.StreamGeneration
			vm.Hello = &hello
		case smelterPacketSample:
			sample, err := decodeSmelterSample(packet.Payload)
			if err != nil {
				return smelterSnapshot{}, err
			}
			vm := snapshot.ensureVM(packet.JobID.String())
			vm.StreamGeneration = packet.StreamGeneration
			vm.Sample = &sample
		case smelterPacketSnapshotEnd:
			return snapshot, nil
		default:
			return smelterSnapshot{}, fmt.Errorf("unexpected packet kind %d", packet.Kind)
		}
	}
}

func (s *smelterSnapshot) ensureVM(jobID string) *smelterVM {
	if vm, ok := s.VMs[jobID]; ok {
		return vm
	}
	vm := &smelterVM{JobID: jobID}
	s.VMs[jobID] = vm
	return vm
}

func snapshotContainsJobs(snapshot smelterSnapshot, jobIDs []string) bool {
	for _, jobID := range jobIDs {
		vm, ok := smelterVMByID(snapshot, jobID)
		if !ok || vm.Hello == nil || vm.Sample == nil {
			return false
		}
	}
	return true
}

func smelterVMByID(snapshot smelterSnapshot, jobID string) (*smelterVM, bool) {
	vm, ok := snapshot.VMs[jobID]
	return vm, ok
}

func smelterWriteRequest(conn net.Conn, kind uint16) error {
	var buf [smelterRequestSize]byte
	binary.LittleEndian.PutUint32(buf[0:4], smelterRequestMagic)
	binary.LittleEndian.PutUint16(buf[4:6], smelterRequestVersion)
	binary.LittleEndian.PutUint16(buf[6:8], kind)
	_, err := conn.Write(buf[:])
	return err
}

func smelterReadPacket(conn net.Conn) (smelterPacket, error) {
	var buf [smelterPacketSize]byte
	if _, err := io.ReadFull(conn, buf[:]); err != nil {
		return smelterPacket{}, err
	}

	if got := binary.LittleEndian.Uint32(buf[0:4]); got != smelterPacketMagic {
		return smelterPacket{}, fmt.Errorf("invalid packet magic: 0x%x", got)
	}
	if got := binary.LittleEndian.Uint16(buf[4:6]); got != smelterPacketVersion {
		return smelterPacket{}, fmt.Errorf("invalid packet version: %d", got)
	}

	var packet smelterPacket
	packet.Kind = binary.LittleEndian.Uint16(buf[6:8])
	packet.HostSeq = binary.LittleEndian.Uint64(buf[8:16])
	packet.ObservedWallNS = binary.LittleEndian.Uint64(buf[16:24])
	copy(packet.JobID[:], buf[24:40])
	packet.StreamGeneration = binary.LittleEndian.Uint32(buf[40:44])
	packet.Flags = binary.LittleEndian.Uint32(buf[44:48])
	copy(packet.Payload[:], buf[48:])
	return packet, nil
}

func decodeSmelterHello(payload [smelterPacketPayloadSize]byte) (smelterHello, error) {
	if err := decodeGuestFrameHeader(payload, smelterFrameHello); err != nil {
		return smelterHello{}, err
	}

	var bootID uuid.UUID
	copy(bootID[:], payload[32:48])
	return smelterHello{
		Seq:        binary.LittleEndian.Uint32(payload[8:12]),
		Flags:      binary.LittleEndian.Uint32(payload[12:16]),
		MonoNS:     binary.LittleEndian.Uint64(payload[16:24]),
		WallNS:     binary.LittleEndian.Uint64(payload[24:32]),
		BootID:     bootID.String(),
		MemTotalKB: binary.LittleEndian.Uint64(payload[48:56]),
	}, nil
}

func decodeSmelterSample(payload [smelterPacketPayloadSize]byte) (smelterSample, error) {
	if err := decodeGuestFrameHeader(payload, smelterFrameSample); err != nil {
		return smelterSample{}, err
	}

	return smelterSample{
		Seq:            binary.LittleEndian.Uint32(payload[8:12]),
		Flags:          binary.LittleEndian.Uint32(payload[12:16]),
		MonoNS:         binary.LittleEndian.Uint64(payload[16:24]),
		WallNS:         binary.LittleEndian.Uint64(payload[24:32]),
		CPUUserTicks:   binary.LittleEndian.Uint64(payload[32:40]),
		CPUSystemTicks: binary.LittleEndian.Uint64(payload[40:48]),
		CPUIdleTicks:   binary.LittleEndian.Uint64(payload[48:56]),
		Load1Centis:    binary.LittleEndian.Uint32(payload[56:60]),
		Load5Centis:    binary.LittleEndian.Uint32(payload[60:64]),
		Load15Centis:   binary.LittleEndian.Uint32(payload[64:68]),
		ProcsRunning:   binary.LittleEndian.Uint16(payload[68:70]),
		ProcsBlocked:   binary.LittleEndian.Uint16(payload[70:72]),
		MemAvailableKB: binary.LittleEndian.Uint64(payload[72:80]),
		IOReadBytes:    binary.LittleEndian.Uint64(payload[80:88]),
		IOWriteBytes:   binary.LittleEndian.Uint64(payload[88:96]),
		NetRXBytes:     binary.LittleEndian.Uint64(payload[96:104]),
		NetTXBytes:     binary.LittleEndian.Uint64(payload[104:112]),
		PSICPUPct100:   binary.LittleEndian.Uint16(payload[112:114]),
		PSIMemPct100:   binary.LittleEndian.Uint16(payload[114:116]),
		PSIIOPct100:    binary.LittleEndian.Uint16(payload[116:118]),
	}, nil
}

func decodeGuestFrameHeader(payload [smelterPacketPayloadSize]byte, wantKind uint16) error {
	if got := binary.LittleEndian.Uint32(payload[0:4]); got != smelterFrameMagic {
		return fmt.Errorf("invalid guest frame magic: 0x%x", got)
	}
	if got := binary.LittleEndian.Uint16(payload[4:6]); got != smelterFrameVersion {
		return fmt.Errorf("invalid guest frame version: %d", got)
	}
	if got := binary.LittleEndian.Uint16(payload[6:8]); got != wantKind {
		return fmt.Errorf("unexpected guest frame kind: %d", got)
	}
	return nil
}
