package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/forge-metal/vm-orchestrator/vmproto"
)

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

	jobCancel context.CancelFunc

	checkpointMu      sync.Mutex
	checkpointWaiters map[string]chan vmproto.CheckpointResponse
}

func runAgent(conn io.ReadWriteCloser, bootStart, readyAt time.Time, sigCh <-chan os.Signal, bridgeFault bridgeFaultMode) error {
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

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	jobCtx, jobCancel := context.WithCancel(context.Background())
	defer jobCancel()
	session.jobCancel = jobCancel

	controlCh := make(chan vmproto.Envelope, 8)
	go session.writeLoop(ctx)
	go session.readLoop(ctx, controlCh)

	go func() {
		select {
		case <-sigCh:
			session.jobCancel()
		case <-ctx.Done():
		}
	}()

	bootToReady := readyAt.Sub(bootStart)
	if err := session.sendControl(vmproto.TypeHello, vmproto.Hello{
		BootToReadyMS: bootToReady.Milliseconds(),
	}); err != nil {
		return err
	}
	go session.heartbeatLoop(ctx)

	runReq, err := session.waitForRunRequest(controlCh)
	if err != nil {
		return session.fail(err)
	}
	if err := session.applyNetwork(runReq.Network); err != nil {
		return session.fail(err)
	}
	if err := setWallClock(runReq.HostWallclockUnixNS); err != nil {
		session.sendLogString("system", fmt.Sprintf("%s warning: set wall clock: %v\n", logPrefix, err))
	}

	env, err := buildRuntimeEnv(runReq.Env, runReq.Network)
	if err != nil {
		return session.fail(err)
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

	var (
		runDuration time.Duration
		exitCode    int
	)
	runDuration, exitCode, err = session.runWorkload(jobCtx, controlCh, runReq, env)
	if err != nil {
		return session.fail(err)
	}

	result := vmproto.Result{
		ExitCode:        exitCode,
		RunDurationMS:   runDuration.Milliseconds(),
		BootToReadyMS:   bootToReady.Milliseconds(),
		StdoutBytes:     session.stdoutBytes.Load(),
		StderrBytes:     session.stderrBytes.Load(),
		DroppedLogBytes: session.droppedLogBytes.Load(),
	}
	resultEnv, err := session.sendResultEnvelope(result)
	if err != nil {
		return err
	}
	if err := session.waitForResultAck(controlCh, resultEnv.Seq); err != nil {
		return session.fail(err)
	}
	if err := session.waitForShutdown(controlCh); err != nil {
		return session.fail(err)
	}

	session.sendLogString("system", logPrefix+" shutdown acknowledged; syncing filesystems and rebooting to terminate the microVM\n")
	syscall.Sync()
	return syscall.Reboot(syscall.LINUX_REBOOT_CMD_RESTART)
}

func (s *agentSession) readLoop(ctx context.Context, controlCh chan<- vmproto.Envelope) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		env, err := s.codec.ReadEnvelope()
		if err != nil {
			s.jobCancel()
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
			s.jobCancel()
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

func (s *agentSession) waitForRunRequest(controlCh <-chan vmproto.Envelope) (vmproto.RunRequest, error) {
	for {
		env, err := s.waitForControl(controlCh)
		if err != nil {
			return vmproto.RunRequest{}, err
		}
		if err := requireControlSeq("await_run_request", env); err != nil {
			return vmproto.RunRequest{}, err
		}

		switch env.Type {
		case vmproto.TypeRunRequest:
			req, err := vmproto.DecodePayload[vmproto.RunRequest](env)
			if err != nil {
				return vmproto.RunRequest{}, protocolStateError("await_run_request", "decode run_request payload: %v", err)
			}
			if req.ProtocolVersion != vmproto.ProtocolVersion {
				return vmproto.RunRequest{}, protocolStateError(
					"await_run_request",
					"run_request protocol_version mismatch: got %d want %d",
					req.ProtocolVersion,
					vmproto.ProtocolVersion,
				)
			}
			req.WorkloadKind = vmproto.NormalizeWorkloadKind(req.WorkloadKind)
			if err := vmproto.ValidateWorkloadKind(req.WorkloadKind); err != nil {
				return vmproto.RunRequest{}, protocolStateError("await_run_request", "invalid run_request workload_kind: %v", err)
			}
			if rawMode, ok := req.Env[bridgeFaultEnvVar]; ok {
				mode, err := parseBridgeFaultMode(rawMode)
				if err != nil {
					return vmproto.RunRequest{}, protocolStateError("await_run_request", "invalid run_request bridge fault mode: %v", err)
				}
				s.bridgeFault = mode
			}
			return req, nil
		case vmproto.TypeCancel:
			s.jobCancel()
		default:
			return vmproto.RunRequest{}, unexpectedControlFrame("await_run_request", env.Type, vmproto.TypeRunRequest, vmproto.TypeCancel)
		}
	}
}

func (s *agentSession) waitForResultAck(controlCh <-chan vmproto.Envelope, resultSeq uint64) error {
	for {
		env, err := s.waitForControl(controlCh)
		if err != nil {
			return err
		}
		if err := requireControlSeq("await_result_ack", env); err != nil {
			return err
		}

		switch env.Type {
		case vmproto.TypeAck:
			ack, err := vmproto.DecodePayload[vmproto.Ack](env)
			if err != nil {
				return protocolStateError("await_result_ack", "decode ack payload: %v", err)
			}
			if ack.ForType != vmproto.TypeResult {
				return protocolStateError("await_result_ack", "ack for_type mismatch: got %s want %s", ack.ForType, vmproto.TypeResult)
			}
			if ack.ForSeq != resultSeq {
				return protocolStateError("await_result_ack", "ack for_seq mismatch: got %d want %d", ack.ForSeq, resultSeq)
			}
			return nil
		case vmproto.TypeCancel:
			s.jobCancel()
		default:
			return unexpectedControlFrame("await_result_ack", env.Type, vmproto.TypeAck, vmproto.TypeCancel)
		}
	}
}

func (s *agentSession) waitForShutdown(controlCh <-chan vmproto.Envelope) error {
	for {
		env, err := s.waitForControl(controlCh)
		if err != nil {
			return err
		}
		if err := requireControlSeq("await_shutdown", env); err != nil {
			return err
		}

		switch env.Type {
		case vmproto.TypeShutdown:
			return nil
		case vmproto.TypeCancel:
			s.jobCancel()
		default:
			return unexpectedControlFrame("await_shutdown", env.Type, vmproto.TypeShutdown, vmproto.TypeCancel)
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

func (s *agentSession) sendResultEnvelope(result vmproto.Result) (vmproto.Envelope, error) {
	if s.bridgeFault != bridgeFaultResultSeqZero {
		return s.sendControlEnvelope(vmproto.TypeResult, result)
	}

	env, err := vmproto.NewEnvelope(vmproto.TypeResult, 0, time.Since(s.bootStart).Nanoseconds(), result)
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

func (s *agentSession) sendLogChunk(stream string, data []byte) {
	if len(data) == 0 {
		return
	}
	env, err := s.nextEnvelope(vmproto.TypeLogChunk, vmproto.LogChunk{
		Stream: stream,
		Data:   data,
	})
	if err != nil {
		s.droppedLogBytes.Add(uint64(len(data)))
		return
	}

	frame := outboundFrame{
		envelope: env,
		logBytes: uint64(len(data)),
	}

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

func (s *agentSession) sendLogString(stream, value string) {
	s.sendLogChunk(stream, []byte(value))
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

func setWallClock(unixNS int64) error {
	if unixNS <= 0 {
		return nil
	}
	tv := syscall.NsecToTimeval(unixNS)
	if err := syscall.Settimeofday(&tv); err != nil {
		return err
	}
	return nil
}

func (s *agentSession) runPhase(ctx context.Context, controlCh <-chan vmproto.Envelope, label string, argv []string, workDir string, env []string) (time.Duration, int, error) {
	if len(argv) == 0 {
		return 0, 0, nil
	}
	spec, err := phaseCommand(argv, workDir, env)
	if err != nil {
		s.sendLogString("system", fmt.Sprintf("%s %s resolve command: %v\n", logPrefix, label, err))
		return 0, 127, nil
	}
	duration, exitCode, err := s.runCommand(ctx, label, spec, controlCh)
	return duration, exitCode, err
}

type commandSpec struct {
	Path    string
	Args    []string
	WorkDir string
	Env     []string
}

func phaseCommand(argv []string, workDir string, env []string) (commandSpec, error) {
	argv0, err := resolveCommand(argv[0])
	if err != nil {
		return commandSpec{}, err
	}
	return commandSpec{
		Path:    argv0,
		Args:    argv,
		WorkDir: workDir,
		Env:     env,
	}, nil
}

func (s *agentSession) runCommand(ctx context.Context, label string, spec commandSpec, controlCh <-chan vmproto.Envelope) (time.Duration, int, error) {
	start := time.Now()
	if err := s.sendControl(vmproto.TypePhaseStart, vmproto.PhaseStart{Name: label}); err != nil {
		return 0, 0, err
	}

	cmd := exec.Command(spec.Path, spec.Args[1:]...)
	cmd.Dir = spec.WorkDir
	cmd.Env = spec.Env
	cmd.Stdout = agentLogWriter{session: s, stream: "stdout"}
	cmd.Stderr = agentLogWriter{session: s, stream: "stderr"}
	// Drop customer code to an unprivileged user; vm-bridge remains the guest root boundary.
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setsid:     true,
		Credential: &syscall.Credential{Uid: runnerUID, Gid: runnerGID},
	}

	if err := cmd.Start(); err != nil {
		return 0, 127, err
	}

	s.activeChildPID.Store(int64(cmd.Process.Pid))

	waitCh := make(chan error, 1)
	go func() {
		waitCh <- cmd.Wait()
	}()

	var waitErr error
	for {
		select {
		case waitErr = <-waitCh:
			s.activeChildPID.Store(0)
			duration := time.Since(start)
			exitCode := exitCodeFromWait(waitErr)
			if err := s.sendControl(vmproto.TypePhaseEnd, vmproto.PhaseEnd{
				Name:       label,
				ExitCode:   exitCode,
				DurationMS: duration.Milliseconds(),
			}); err != nil {
				return duration, exitCode, err
			}
			return duration, exitCode, nil
		case env := <-controlCh:
			if err := s.handleRunPhaseControl(env); err != nil {
				terminateProcessGroup(cmd.Process.Pid)
				s.activeChildPID.Store(0)
				return time.Since(start), exitCodeFromWait(waitErr), err
			}
		case err := <-s.errCh:
			terminateProcessGroup(cmd.Process.Pid)
			s.activeChildPID.Store(0)
			return time.Since(start), exitCodeFromWait(waitErr), err
		case <-ctx.Done():
			terminateProcessGroup(cmd.Process.Pid)
		}
	}
}

func (s *agentSession) handleRunPhaseControl(env vmproto.Envelope) error {
	switch env.Type {
	case vmproto.TypeCancel:
		if err := requireControlSeq("run_phase", env); err != nil {
			return err
		}
		s.jobCancel()
		return nil
	default:
		return unexpectedControlFrame("run_phase", env.Type, vmproto.TypeCancel)
	}
}

func unexpectedControlFrame(state string, got vmproto.MessageType, expected ...vmproto.MessageType) error {
	allowed := make([]string, 0, len(expected))
	for _, msgType := range expected {
		allowed = append(allowed, string(msgType))
	}
	return protocolStateError(state, "unexpected control frame type %s (expected one of: %s)", got, strings.Join(allowed, ", "))
}

func protocolStateError(state string, format string, args ...any) error {
	return fmt.Errorf("control protocol violation in %s: %s", state, fmt.Sprintf(format, args...))
}

func requireControlSeq(state string, env vmproto.Envelope) error {
	if env.Seq == 0 {
		return protocolStateError(state, "invalid control envelope seq: got %d want > 0", env.Seq)
	}
	return nil
}

type agentLogWriter struct {
	session *agentSession
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
	w.session.sendLogChunk(w.stream, chunk)
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

func runCommandOutput(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}
