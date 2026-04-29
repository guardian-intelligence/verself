package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/verself/vm-orchestrator/vmproto"
)

// bridgeState enumerates the deterministic control-protocol states the
// guest bridge cycles through. Each state has a single allowed set of
// inbound frame types, and any deviation is reported as
// "control protocol violation in <state>" so traces and the
// verify-vm-orchestrator-live.sh smoke flow can pin failures to a
// labeled state machine rather than free-form strings.
type bridgeState int

const (
	bridgeStateAwaitLeaseInit bridgeState = iota
	bridgeStateAwaitExecRequest
	bridgeStateExecRunning
	bridgeStateAwaitExecResultAck
)

func (s bridgeState) String() string {
	switch s {
	case bridgeStateAwaitLeaseInit:
		return "await_lease_init"
	case bridgeStateAwaitExecRequest:
		return "await_exec_request"
	case bridgeStateExecRunning:
		return "exec_running"
	case bridgeStateAwaitExecResultAck:
		return "await_exec_result_ack"
	default:
		return fmt.Sprintf("bridge_state_%d", int(s))
	}
}

type outboundFrame struct {
	envelope vmproto.Envelope
	logBytes uint64
}

type agentSession struct {
	conn        io.ReadWriteCloser
	codec       *vmproto.Codec
	bootStart   time.Time
	readyAt     time.Time
	bridgeFault bridgeFaultMode

	controlQ chan outboundFrame
	logQ     chan outboundFrame
	errCh    chan error

	seq             atomic.Uint64
	stdoutBytes     atomic.Uint64
	stderrBytes     atomic.Uint64
	droppedLogBytes atomic.Uint64
	activeChildPID  atomic.Int64
	filesystems     []vmproto.FilesystemMount

	jobCancel context.CancelFunc

	checkpointMu      sync.Mutex
	checkpointWaiters map[string]chan vmproto.CheckpointResponse

	// etcOverlayApplied records which /etc/<rel> paths have already
	// been written by an etc-overlay/ from some toolchain image. A
	// second image trying to overlay the same path with *different*
	// content is a hard error; the same content (same sha256) from a
	// second image is a no-op so toolchain images that share the
	// runner-overlay-common Bazel filegroup compose cleanly. Either
	// case surfaces in lease boot logs.
	etcOverlayApplied map[string]etcOverlayEntry
}

type etcOverlayEntry struct {
	imageName string
	sha256    string
}

func runAgent(conn io.ReadWriteCloser, bootStart, readyAt time.Time, sigCh <-chan os.Signal, bridgeFault bridgeFaultMode, bootTimings vmproto.GuestBootTimings) error {
	bootTimings.AgentStartMS = time.Since(bootStart).Milliseconds()
	session := &agentSession{
		conn:        conn,
		codec:       vmproto.NewCodec(conn, conn),
		bootStart:   bootStart,
		readyAt:     readyAt,
		bridgeFault: bridgeFault,
		controlQ:    make(chan outboundFrame, vmproto.ControlQueueCapacity),
		logQ:        make(chan outboundFrame, vmproto.LogQueueCapacity),
		errCh:       make(chan error, 2),
	}
	bootTimings.AgentSessionReadyMS = time.Since(bootStart).Milliseconds()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	controlCh := make(chan vmproto.Envelope, 8)
	go session.writeLoop(ctx)
	go session.readLoop(ctx, controlCh)
	go session.heartbeatLoop(ctx)
	bootTimings.AgentIOLoopsStartedMS = time.Since(bootStart).Milliseconds()

	bootToReady := readyAt.Sub(bootStart)
	bootTimings.HelloEnqueueStartMS = time.Since(bootStart).Milliseconds()
	bootTimings.KernelBootToHelloEnqueueStartMS = kernelUptimeMS()
	bootTimings.HelloEnqueueDoneMS = bootTimings.HelloEnqueueStartMS
	if err := session.sendControl(vmproto.TypeHello, vmproto.Hello{
		BootToReadyMS: bootToReady.Milliseconds(),
		BootTimings:   &bootTimings,
	}); err != nil {
		return err
	}

	initReq, err := session.waitForLeaseInit(controlCh)
	if err != nil {
		return session.fail(err)
	}
	if err := session.applyNetwork(initReq.Network); err != nil {
		return session.fail(err)
	}
	if err := session.mountFilesystems(initReq.Filesystems); err != nil {
		return session.fail(err)
	}
	if err := setWallClock(initReq.HostWallclockUnixNS); err != nil {
		session.sendLogString("", "system", fmt.Sprintf("%s warning: set wall clock: %v\n", logPrefix, err))
	}

	localControlCtx, localControlCancel := context.WithCancel(ctx)
	stopLocalControl, err := session.startLocalControlServer(localControlCtx)
	if err != nil {
		localControlCancel()
		return session.fail(err)
	}
	defer func() {
		localControlCancel()
		stopLocalControl()
	}()

	for {
		select {
		case <-sigCh:
			return session.shutdown()
		default:
		}

		req, err := session.waitForExecRequest(controlCh)
		if err != nil {
			if errors.Is(err, errGuestShutdownRequested) {
				return session.shutdown()
			}
			return session.fail(err)
		}
		if err := session.runOneExec(req, controlCh, initReq.Network); err != nil {
			return session.fail(err)
		}
	}
}

var errGuestShutdownRequested = errors.New("guest shutdown requested")

func (s *agentSession) readLoop(ctx context.Context, controlCh chan<- vmproto.Envelope) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		env, err := s.codec.ReadEnvelope()
		if err != nil {
			if s.jobCancel != nil {
				s.jobCancel()
			}
			select {
			case s.errCh <- fmt.Errorf("read control stream: %w", err):
			default:
			}
			return
		}
		if env.Type == vmproto.TypeCheckpointResponse {
			resp, err := vmproto.DecodePayload[vmproto.CheckpointResponse](env)
			if err == nil && s.deliverCheckpointResponse(resp) {
				continue
			}
		}
		select {
		case controlCh <- env:
		case <-ctx.Done():
			return
		}
	}
}

func (s *agentSession) writeLoop(ctx context.Context) {
	for {
		var frame outboundFrame
		select {
		case <-ctx.Done():
			return
		default:
		}

		select {
		case frame = <-s.controlQ:
		default:
			select {
			case <-ctx.Done():
				return
			case frame = <-s.controlQ:
			case frame = <-s.logQ:
			}
		}

		if err := s.codec.WriteEnvelope(frame.envelope); err != nil {
			if s.jobCancel != nil {
				s.jobCancel()
			}
			select {
			case s.errCh <- fmt.Errorf("write control stream: %w", err):
			default:
			}
			return
		}
	}
}

func (s *agentSession) heartbeatLoop(ctx context.Context) {
	ticker := time.NewTicker(vmproto.HeartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := s.sendControl(vmproto.TypeHeartbeat, vmproto.Heartbeat{}); err != nil {
				select {
				case s.errCh <- err:
				default:
				}
				return
			}
		}
	}
}

func (s *agentSession) waitForLeaseInit(controlCh <-chan vmproto.Envelope) (vmproto.LeaseInit, error) {
	for {
		env, err := s.waitForControl(controlCh)
		if err != nil {
			return vmproto.LeaseInit{}, err
		}
		if err := requireControlSeq(bridgeStateAwaitLeaseInit, env); err != nil {
			return vmproto.LeaseInit{}, err
		}
		switch env.Type {
		case vmproto.TypeLeaseInit:
			req, err := vmproto.DecodePayload[vmproto.LeaseInit](env)
			if err != nil {
				return vmproto.LeaseInit{}, protocolStateError(bridgeStateAwaitLeaseInit, "decode lease_init payload: %v", err)
			}
			if req.ProtocolVersion != vmproto.ProtocolVersion {
				return vmproto.LeaseInit{}, protocolStateError(bridgeStateAwaitLeaseInit, "protocol_version mismatch: got %d want %d", req.ProtocolVersion, vmproto.ProtocolVersion)
			}
			return req, nil
		case vmproto.TypeShutdown:
			return vmproto.LeaseInit{}, errGuestShutdownRequested
		default:
			return vmproto.LeaseInit{}, unexpectedControlFrame(bridgeStateAwaitLeaseInit, env.Type, vmproto.TypeLeaseInit, vmproto.TypeShutdown)
		}
	}
}

func (s *agentSession) waitForExecRequest(controlCh <-chan vmproto.Envelope) (vmproto.ExecRequest, error) {
	for {
		env, err := s.waitForControl(controlCh)
		if err != nil {
			return vmproto.ExecRequest{}, err
		}
		if err := requireControlSeq(bridgeStateAwaitExecRequest, env); err != nil {
			return vmproto.ExecRequest{}, err
		}
		switch env.Type {
		case vmproto.TypeExecRequest:
			req, err := vmproto.DecodePayload[vmproto.ExecRequest](env)
			if err != nil {
				return vmproto.ExecRequest{}, protocolStateError(bridgeStateAwaitExecRequest, "decode exec_request payload: %v", err)
			}
			if req.ProtocolVersion != vmproto.ProtocolVersion {
				return vmproto.ExecRequest{}, protocolStateError(bridgeStateAwaitExecRequest, "protocol_version mismatch: got %d want %d", req.ProtocolVersion, vmproto.ProtocolVersion)
			}
			if rawMode, ok := req.Env[bridgeFaultEnvVar]; ok {
				mode, err := parseBridgeFaultMode(rawMode)
				if err != nil {
					return vmproto.ExecRequest{}, protocolStateError(bridgeStateAwaitExecRequest, "invalid bridge fault mode: %v", err)
				}
				s.bridgeFault = mode
			}
			if strings.TrimSpace(req.ExecID) == "" {
				return vmproto.ExecRequest{}, protocolStateError(bridgeStateAwaitExecRequest, "exec_id is required")
			}
			if len(req.Argv) == 0 {
				return vmproto.ExecRequest{}, protocolStateError(bridgeStateAwaitExecRequest, "argv is required")
			}
			return req, nil
		case vmproto.TypeFilesystemSealRequest:
			if err := s.handleFilesystemSealRequest(env); err != nil {
				return vmproto.ExecRequest{}, err
			}
			continue
		case vmproto.TypeShutdown:
			return vmproto.ExecRequest{}, errGuestShutdownRequested
		case vmproto.TypeCancel:
			continue
		default:
			return vmproto.ExecRequest{}, unexpectedControlFrame(bridgeStateAwaitExecRequest, env.Type, vmproto.TypeExecRequest, vmproto.TypeFilesystemSealRequest, vmproto.TypeShutdown, vmproto.TypeCancel)
		}
	}
}

func (s *agentSession) handleFilesystemSealRequest(env vmproto.Envelope) error {
	req, err := vmproto.DecodePayload[vmproto.FilesystemSealRequest](env)
	if err != nil {
		return protocolStateError(bridgeStateAwaitExecRequest, "decode filesystem_seal_request payload: %v", err)
	}
	if req.ProtocolVersion != vmproto.ProtocolVersion {
		return protocolStateError(bridgeStateAwaitExecRequest, "protocol_version mismatch: got %d want %d", req.ProtocolVersion, vmproto.ProtocolVersion)
	}
	sealErr := s.sealFilesystem(req.Name)
	result := vmproto.FilesystemSealResult{
		LeaseID: req.LeaseID,
		Name:    req.Name,
		Sealed:  sealErr == nil,
	}
	if sealErr != nil {
		result.Error = sealErr.Error()
	}
	if err := s.sendControl(vmproto.TypeFilesystemSealResult, result); err != nil {
		return err
	}
	return nil
}

func (s *agentSession) sealFilesystem(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("filesystem seal name is required")
	}
	idx := -1
	var fs vmproto.FilesystemMount
	for i, mounted := range s.filesystems {
		if mounted.Name == name {
			idx = i
			fs = mounted
			break
		}
	}
	if idx < 0 {
		return fmt.Errorf("filesystem %s is not mounted", name)
	}
	if fs.ReadOnly {
		return fmt.Errorf("filesystem %s is read-only", name)
	}
	syscall.Sync()
	if err := syscall.Unmount(fs.MountPath, 0); err != nil && err != syscall.EINVAL {
		return fmt.Errorf("unmount filesystem %s at %s: %w", name, fs.MountPath, err)
	}
	s.filesystems = append(s.filesystems[:idx], s.filesystems[idx+1:]...)
	return nil
}

func (s *agentSession) runOneExec(req vmproto.ExecRequest, controlCh <-chan vmproto.Envelope, network vmproto.NetworkConfig) error {
	s.stdoutBytes.Store(0)
	s.stderrBytes.Store(0)
	s.droppedLogBytes.Store(0)

	jobCtx, jobCancel := context.WithCancel(context.Background())
	if req.MaxWallSeconds > 0 {
		var cancel context.CancelFunc
		jobCtx, cancel = context.WithTimeout(jobCtx, time.Duration(req.MaxWallSeconds)*time.Second)
		jobCancel = cancel
	}
	s.jobCancel = jobCancel
	defer func() {
		jobCancel()
		s.jobCancel = nil
	}()

	env, err := buildRuntimeEnv(req.Env, network, s.filesystemMountPaths())
	if err != nil {
		return err
	}
	if err := s.sendControl(vmproto.TypeExecStarted, vmproto.ExecStarted{
		LeaseID:         req.LeaseID,
		ExecID:          req.ExecID,
		StartedUnixNS:   time.Now().UnixNano(),
		ProtocolVersion: vmproto.ProtocolVersion,
	}); err != nil {
		return err
	}

	duration, exitCode, err := s.runExecCommand(jobCtx, req, env, controlCh)
	if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
		return err
	}

	result := vmproto.ExecResult{
		LeaseID:         req.LeaseID,
		ExecID:          req.ExecID,
		ExitCode:        exitCode,
		DurationMS:      duration.Milliseconds(),
		StdoutBytes:     s.stdoutBytes.Load(),
		StderrBytes:     s.stderrBytes.Load(),
		DroppedLogBytes: s.droppedLogBytes.Load(),
	}
	resultEnv, err := s.sendResultEnvelope(result)
	if err != nil {
		return err
	}
	if err := s.waitForResultAck(controlCh, resultEnv.Seq); err != nil {
		return err
	}
	return nil
}

func (s *agentSession) waitForResultAck(controlCh <-chan vmproto.Envelope, resultSeq uint64) error {
	for {
		env, err := s.waitForControl(controlCh)
		if err != nil {
			return err
		}
		if err := requireControlSeq(bridgeStateAwaitExecResultAck, env); err != nil {
			return err
		}
		switch env.Type {
		case vmproto.TypeAck:
			ack, err := vmproto.DecodePayload[vmproto.Ack](env)
			if err != nil {
				return protocolStateError(bridgeStateAwaitExecResultAck, "decode ack payload: %v", err)
			}
			if ack.ForType != vmproto.TypeExecResult {
				return protocolStateError(bridgeStateAwaitExecResultAck, "ack for_type mismatch: got %s want %s", ack.ForType, vmproto.TypeExecResult)
			}
			if ack.ForSeq != resultSeq {
				return protocolStateError(bridgeStateAwaitExecResultAck, "ack for_seq mismatch: got %d want %d", ack.ForSeq, resultSeq)
			}
			return nil
		case vmproto.TypeCancel:
			if s.jobCancel != nil {
				s.jobCancel()
			}
		case vmproto.TypeShutdown:
			return errGuestShutdownRequested
		default:
			return unexpectedControlFrame(bridgeStateAwaitExecResultAck, env.Type, vmproto.TypeAck, vmproto.TypeCancel, vmproto.TypeShutdown)
		}
	}
}

func (s *agentSession) waitForControl(controlCh <-chan vmproto.Envelope) (vmproto.Envelope, error) {
	select {
	case env := <-controlCh:
		return env, nil
	case err := <-s.errCh:
		return vmproto.Envelope{}, err
	}
}

func (s *agentSession) nextEnvelope(msgType vmproto.MessageType, payload any) (vmproto.Envelope, error) {
	seq := s.seq.Add(1)
	return vmproto.NewEnvelope(msgType, seq, time.Since(s.bootStart).Nanoseconds(), payload)
}

func (s *agentSession) sendControl(msgType vmproto.MessageType, payload any) error {
	_, err := s.sendControlEnvelope(msgType, payload)
	return err
}

func (s *agentSession) sendControlEnvelope(msgType vmproto.MessageType, payload any) (vmproto.Envelope, error) {
	env, err := s.nextEnvelope(msgType, payload)
	if err != nil {
		return vmproto.Envelope{}, err
	}
	if err := s.enqueueControl(env); err != nil {
		return vmproto.Envelope{}, err
	}
	return env, nil
}

func (s *agentSession) sendResultEnvelope(result vmproto.ExecResult) (vmproto.Envelope, error) {
	if s.bridgeFault != bridgeFaultResultSeqZero {
		return s.sendControlEnvelope(vmproto.TypeExecResult, result)
	}

	env, err := vmproto.NewEnvelope(vmproto.TypeExecResult, 0, time.Since(s.bootStart).Nanoseconds(), result)
	if err != nil {
		return vmproto.Envelope{}, err
	}
	if err := s.enqueueControl(env); err != nil {
		return vmproto.Envelope{}, err
	}
	return env, nil
}

func (s *agentSession) enqueueControl(env vmproto.Envelope) error {
	select {
	case s.controlQ <- outboundFrame{envelope: env}:
		return nil
	case err := <-s.errCh:
		return err
	}
}

func (s *agentSession) sendLogChunk(execID, stream string, data []byte) {
	if len(data) == 0 {
		return
	}
	env, err := s.nextEnvelope(vmproto.TypeLogChunk, vmproto.LogChunk{
		ExecID: execID,
		Stream: stream,
		Data:   data,
	})
	if err != nil {
		s.droppedLogBytes.Add(uint64(len(data)))
		return
	}

	frame := outboundFrame{envelope: env, logBytes: uint64(len(data))}
	select {
	case s.logQ <- frame:
		return
	default:
	}

	select {
	case dropped := <-s.logQ:
		s.droppedLogBytes.Add(dropped.logBytes)
	default:
	}

	select {
	case s.logQ <- frame:
	default:
		s.droppedLogBytes.Add(frame.logBytes)
	}
}

func (s *agentSession) sendLogString(execID, stream, value string) {
	s.sendLogChunk(execID, stream, []byte(value))
}

func (s *agentSession) applyNetwork(cfg vmproto.NetworkConfig) error {
	if strings.TrimSpace(cfg.LinkName) == "" {
		return fmt.Errorf("network link name is required")
	}
	if strings.TrimSpace(cfg.AddressCIDR) == "" {
		return fmt.Errorf("network address_cidr is required")
	}
	if strings.TrimSpace(cfg.Gateway) == "" {
		return fmt.Errorf("network gateway is required")
	}

	steps := [][]string{
		{ipBin, "link", "set", cfg.LinkName, "up"},
		{ipBin, "addr", "flush", "dev", cfg.LinkName},
		{ipBin, "addr", "add", cfg.AddressCIDR, "dev", cfg.LinkName},
		{ipBin, "route", "replace", "default", "via", cfg.Gateway, "dev", cfg.LinkName},
	}
	for _, args := range steps {
		out, err := runCommandOutput(args[0], args[1:]...)
		if err != nil {
			return fmt.Errorf("%s: %s", strings.Join(args, " "), strings.TrimSpace(out))
		}
	}
	if len(cfg.DNS) > 0 {
		var builder strings.Builder
		for _, server := range cfg.DNS {
			server = strings.TrimSpace(server)
			if server == "" {
				continue
			}
			builder.WriteString("nameserver ")
			builder.WriteString(server)
			builder.WriteByte('\n')
		}
		if builder.Len() > 0 {
			if err := os.WriteFile("/etc/resolv.conf", []byte(builder.String()), 0o644); err != nil {
				return fmt.Errorf("write resolv.conf: %w", err)
			}
		}
	}
	return nil
}

func (s *agentSession) mountFilesystems(filesystems []vmproto.FilesystemMount) error {
	if len(filesystems) == 0 {
		return nil
	}
	mounted := make([]vmproto.FilesystemMount, 0, len(filesystems))
	for _, fs := range filesystems {
		if err := validateFilesystemMount(fs); err != nil {
			return err
		}
		if err := waitForBlockDevice(fs.DevicePath, 5*time.Second); err != nil {
			return err
		}
		if err := os.MkdirAll(fs.MountPath, 0o755); err != nil {
			return fmt.Errorf("mkdir composed filesystem mount %s: %w", fs.MountPath, err)
		}
		if !fs.ReadOnly {
			if err := prepareWritableMountPath(fs.MountPath); err != nil {
				return err
			}
		}
		flags := uintptr(syscall.MS_NOATIME)
		data := ""
		if fs.ReadOnly {
			flags |= syscall.MS_RDONLY
			data = "ro"
		}
		if err := syscall.Mount(fs.DevicePath, fs.MountPath, firstNonEmpty(fs.FSType, "ext4"), flags, data); err != nil {
			if err == syscall.EBUSY {
				mounted = append(mounted, fs)
				continue
			}
			return fmt.Errorf("mount composed filesystem %s (%s) on %s: %w", fs.Name, fs.DevicePath, fs.MountPath, err)
		}
		if !fs.ReadOnly {
			if err := removeEmptyLostFound(fs.MountPath); err != nil {
				return fmt.Errorf("prepare composed filesystem %s root %s: %w", fs.Name, fs.MountPath, err)
			}
			if err := os.Chown(fs.MountPath, runnerUID, runnerGID); err != nil {
				return fmt.Errorf("chown composed filesystem %s root %s: %w", fs.Name, fs.MountPath, err)
			}
		} else {
			// Read-only toolchain images carry an overlay contract:
			// /etc-overlay/ is copied into /etc, .verself-writable-overlays
			// lists paths to tmpfs-mount on top of the read-only base, and
			// any /etc-overlay/passwd entries get their $HOME materialised.
			// Substrate-only images and writable mounts skip this; they
			// don't have somewhere to publish overlay metadata.
			if err := s.applyToolchainOverlays(fs.Name, fs.MountPath); err != nil {
				return fmt.Errorf("apply overlays for %s: %w", fs.Name, err)
			}
		}
		fmt.Fprintf(os.Stdout, "%s mounted composed filesystem name=%s device=%s path=%s read_only=%t\n", logPrefix, fs.Name, fs.DevicePath, fs.MountPath, fs.ReadOnly)
		mounted = append(mounted, fs)
	}
	s.filesystems = mounted
	return nil
}

func removeEmptyLostFound(mountPath string) error {
	lostFound := filepath.Join(mountPath, "lost+found")
	entries, err := os.ReadDir(lostFound)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read lost+found: %w", err)
	}
	if len(entries) != 0 {
		return nil
	}
	if err := os.Remove(lostFound); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove empty lost+found: %w", err)
	}
	return nil
}

func prepareWritableMountPath(path string) error {
	// Chown the leaf to the runner uid/gid so the workload can write
	// into the freshly-mounted filesystem without first sudoing.
	// Intermediate directories stay whatever the substrate / overlay
	// installed them as — typically root:755 — because granting the
	// runner ownership of /home or /opt would be a privilege leak.
	// validateFilesystemMount has already rejected the system roots
	// (/proc, /sys, /dev, /run) and the bare /, so any path we see
	// here is a safe leaf to chown.
	clean := filepath.Clean(path)
	if clean == "/" {
		return nil
	}
	if err := os.Chown(clean, runnerUID, runnerGID); err != nil {
		return fmt.Errorf("chown composed filesystem path %s: %w", clean, err)
	}
	return nil
}

func validateFilesystemMount(fs vmproto.FilesystemMount) error {
	if strings.TrimSpace(fs.Name) == "" {
		return fmt.Errorf("composed filesystem name is required")
	}
	if strings.TrimSpace(fs.DevicePath) == "" || !strings.HasPrefix(fs.DevicePath, "/dev/") {
		return fmt.Errorf("composed filesystem %s has invalid device path %q", fs.Name, fs.DevicePath)
	}
	if strings.TrimSpace(fs.MountPath) == "" || !strings.HasPrefix(fs.MountPath, "/") {
		return fmt.Errorf("composed filesystem %s has invalid mount path %q", fs.Name, fs.MountPath)
	}
	clean := filepath.Clean(fs.MountPath)
	if clean != fs.MountPath || clean == "/" || strings.HasPrefix(clean, "/proc") || strings.HasPrefix(clean, "/sys") || strings.HasPrefix(clean, "/dev") || strings.HasPrefix(clean, "/run") {
		return fmt.Errorf("composed filesystem %s mount path %q is not allowed", fs.Name, fs.MountPath)
	}
	switch firstNonEmpty(fs.FSType, "ext4") {
	case "ext4":
		return nil
	default:
		return fmt.Errorf("composed filesystem %s uses unsupported fs_type %q", fs.Name, fs.FSType)
	}
}

func waitForBlockDevice(path string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		if stat, err := os.Stat(path); err == nil {
			if stat.Mode()&os.ModeDevice != 0 {
				return nil
			}
			return fmt.Errorf("composed filesystem device %s is not a device", path)
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("stat composed filesystem device %s: %w", path, err)
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("composed filesystem device %s did not appear", path)
		}
		time.Sleep(25 * time.Millisecond)
	}
}

func (s *agentSession) filesystemMountPaths() []string {
	if len(s.filesystems) == 0 {
		return nil
	}
	paths := make([]string, 0, len(s.filesystems))
	for _, fs := range s.filesystems {
		paths = append(paths, fs.MountPath)
	}
	return paths
}

func setWallClock(unixNS int64) error {
	if unixNS <= 0 {
		return nil
	}
	tv := syscall.NsecToTimeval(unixNS)
	return syscall.Settimeofday(&tv)
}

type commandSpec struct {
	Path    string
	Args    []string
	WorkDir string
	Env     []string
}

func (s *agentSession) runExecCommand(ctx context.Context, req vmproto.ExecRequest, env []string, controlCh <-chan vmproto.Envelope) (time.Duration, int, error) {
	spec, err := execCommand(req.Argv, req.WorkingDir, env)
	if err != nil {
		s.sendLogString(req.ExecID, "system", fmt.Sprintf("%s resolve command: %v\n", logPrefix, err))
		return 0, 127, nil
	}

	start := time.Now()
	cmd := exec.Command(spec.Path, spec.Args[1:]...)
	cmd.Dir = spec.WorkDir
	cmd.Env = spec.Env
	cmd.Stdout = agentLogWriter{session: s, execID: req.ExecID, stream: "stdout"}
	cmd.Stderr = agentLogWriter{session: s, execID: req.ExecID, stream: "stderr"}
	// Workload processes are unprivileged; PID 1 remains the guest control boundary.
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setsid:     true,
		Credential: &syscall.Credential{Uid: runnerUID, Gid: runnerGID},
	}

	if err := cmd.Start(); err != nil {
		return 0, 127, err
	}
	s.activeChildPID.Store(int64(cmd.Process.Pid))

	waitCh := make(chan error, 1)
	go func() { waitCh <- cmd.Wait() }()

	for {
		select {
		case waitErr := <-waitCh:
			s.activeChildPID.Store(0)
			return time.Since(start), exitCodeFromWait(waitErr), nil
		case env := <-controlCh:
			switch env.Type {
			case vmproto.TypeCancel:
				cancel, err := vmproto.DecodePayload[vmproto.Cancel](env)
				if err != nil {
					terminateProcessGroup(cmd.Process.Pid)
					return time.Since(start), 1, err
				}
				if cancel.ExecID == "" || cancel.ExecID == req.ExecID {
					terminateProcessGroup(cmd.Process.Pid)
					return time.Since(start), 130, context.Canceled
				}
			case vmproto.TypeShutdown:
				terminateProcessGroup(cmd.Process.Pid)
				return time.Since(start), 130, errGuestShutdownRequested
			default:
				terminateProcessGroup(cmd.Process.Pid)
				return time.Since(start), 1, unexpectedControlFrame(bridgeStateExecRunning, env.Type, vmproto.TypeCancel, vmproto.TypeShutdown)
			}
		case err := <-s.errCh:
			terminateProcessGroup(cmd.Process.Pid)
			return time.Since(start), 1, err
		case <-ctx.Done():
			terminateProcessGroup(cmd.Process.Pid)
		}
	}
}

func execCommand(argv []string, workDir string, env []string) (commandSpec, error) {
	if len(argv) == 0 {
		return commandSpec{}, fmt.Errorf("argv is required")
	}
	argv0, err := resolveCommand(argv[0])
	if err != nil {
		return commandSpec{}, err
	}
	return commandSpec{
		Path:    argv0,
		Args:    append([]string(nil), argv...),
		WorkDir: normalizeWorkDir(workDir),
		Env:     append([]string(nil), env...),
	}, nil
}

func unexpectedControlFrame(state bridgeState, got vmproto.MessageType, expected ...vmproto.MessageType) error {
	allowed := make([]string, 0, len(expected))
	for _, msgType := range expected {
		allowed = append(allowed, string(msgType))
	}
	return protocolStateError(state, "unexpected control frame type %s (expected one of: %s)", got, strings.Join(allowed, ", "))
}

func protocolStateError(state bridgeState, format string, args ...any) error {
	return fmt.Errorf("control protocol violation in %s: %s", state, fmt.Sprintf(format, args...))
}

func requireControlSeq(state bridgeState, env vmproto.Envelope) error {
	if env.Seq == 0 {
		return protocolStateError(state, "invalid control envelope seq: got %d want > 0", env.Seq)
	}
	return nil
}

type agentLogWriter struct {
	session *agentSession
	execID  string
	stream  string
}

func (w agentLogWriter) Write(data []byte) (int, error) {
	if len(data) == 0 {
		return 0, nil
	}
	chunk := append([]byte(nil), data...)
	switch w.stream {
	case "stdout":
		w.session.stdoutBytes.Add(uint64(len(data)))
	case "stderr":
		w.session.stderrBytes.Add(uint64(len(data)))
	}
	w.session.sendLogChunk(w.execID, w.stream, chunk)
	return len(data), nil
}

func terminateProcessGroup(pid int) {
	if pid <= 0 {
		return
	}
	_ = syscall.Kill(-pid, syscall.SIGTERM)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if !processExists(pid) {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	_ = syscall.Kill(-pid, syscall.SIGKILL)
}

func processExists(pid int) bool {
	if pid <= 0 {
		return false
	}
	_, err := os.Stat(fmt.Sprintf("/proc/%d", pid))
	return err == nil
}

func exitCodeFromWait(err error) int {
	if err == nil {
		return 0
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		return 1
	}
	ws, ok := exitErr.Sys().(syscall.WaitStatus)
	if !ok {
		return 1
	}
	if ws.Exited() {
		return ws.ExitStatus()
	}
	if ws.Signaled() {
		return 128 + int(ws.Signal())
	}
	return 1
}

func (s *agentSession) fail(err error) error {
	if err == nil {
		return nil
	}
	_ = s.sendControl(vmproto.TypeFatal, vmproto.Fatal{Message: err.Error()})
	return err
}

func (s *agentSession) shutdown() error {
	s.sendLogString("", "system", logPrefix+" shutdown acknowledged; syncing filesystems and rebooting to terminate the microVM\n")
	syscall.Sync()
	return syscall.Reboot(syscall.LINUX_REBOOT_CMD_RESTART)
}

func runCommandOutput(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
