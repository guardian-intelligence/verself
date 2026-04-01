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

	"github.com/forge-metal/forge-metal/internal/vmproto"
)

type outboundFrame struct {
	envelope vmproto.Envelope
	logBytes uint64
}

type agentSession struct {
	conn      io.ReadWriteCloser
	codec     *vmproto.Codec
	bootStart time.Time
	readyAt   time.Time

	controlQ chan outboundFrame
	logQ     chan outboundFrame
	errCh    chan error

	seq             atomic.Uint64
	stdoutBytes     atomic.Uint64
	stderrBytes     atomic.Uint64
	droppedLogBytes atomic.Uint64
	activeChildPID  atomic.Int64

	jobCancel context.CancelFunc
}

func runAgent(conn io.ReadWriteCloser, bootStart, readyAt time.Time, sigCh <-chan os.Signal) error {
	session := &agentSession{
		conn:      conn,
		codec:     vmproto.NewCodec(conn, conn),
		bootStart: bootStart,
		readyAt:   readyAt,
		controlQ:  make(chan outboundFrame, vmproto.ControlQueueCapacity),
		logQ:      make(chan outboundFrame, vmproto.LogQueueCapacity),
		errCh:     make(chan error, 2),
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
		return err
	}
	if err := session.applyNetwork(runReq.Network); err != nil {
		return session.fail(err)
	}
	if err := setWallClock(runReq.HostWallclockUnixNS); err != nil {
		session.sendLogString("system", fmt.Sprintf("[init] warning: set wall clock: %v\n", err))
	}

	env, err := buildRuntimeEnv(runReq.Env)
	if err != nil {
		return session.fail(err)
	}

	serviceStartDuration, err := session.startServices(jobCtx, controlCh, runReq.Services, env)
	if err != nil {
		return session.fail(err)
	}

	prepareDuration, exitCode, err := session.runPhase(jobCtx, controlCh, "prepare", runReq.PrepareCommand, normalizeWorkDir(runReq.PrepareWorkDir), env)
	if err != nil {
		return session.fail(err)
	}

	var runDuration time.Duration
	if exitCode == 0 {
		runDuration, exitCode, err = session.runPhase(jobCtx, controlCh, "run", runReq.RunCommand, normalizeWorkDir(runReq.RunWorkDir), env)
		if err != nil {
			return session.fail(err)
		}
	}

	result := vmproto.Result{
		ExitCode:               exitCode,
		PrepareDurationMS:      prepareDuration.Milliseconds(),
		RunDurationMS:          runDuration.Milliseconds(),
		ServiceStartDurationMS: serviceStartDuration.Milliseconds(),
		BootToReadyMS:          bootToReady.Milliseconds(),
		StdoutBytes:            session.stdoutBytes.Load(),
		StderrBytes:            session.stderrBytes.Load(),
		DroppedLogBytes:        session.droppedLogBytes.Load(),
	}
	if err := session.sendControl(vmproto.TypeResult, result); err != nil {
		return err
	}

	ackedResult := false
	for {
		env, err := session.waitForControl(controlCh)
		if err != nil {
			return err
		}

		switch env.Type {
		case vmproto.TypeAck:
			ack, err := vmproto.DecodePayload[vmproto.Ack](env)
			if err != nil {
				return err
			}
			if ack.ForType == vmproto.TypeResult {
				ackedResult = true
			}
		case vmproto.TypeShutdown:
			if !ackedResult {
				continue
			}
			syscall.Sync()
			return syscall.Reboot(syscall.LINUX_REBOOT_CMD_RESTART)
		case vmproto.TypeCancel:
			session.jobCancel()
		default:
		}
	}
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

		switch env.Type {
		case vmproto.TypeRunRequest:
			req, err := vmproto.DecodePayload[vmproto.RunRequest](env)
			if err != nil {
				return vmproto.RunRequest{}, err
			}
			return req, nil
		case vmproto.TypeCancel:
			s.jobCancel()
		default:
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
	env, err := s.nextEnvelope(msgType, payload)
	if err != nil {
		return err
	}
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

func (s *agentSession) startServices(ctx context.Context, controlCh <-chan vmproto.Envelope, services []string, env []string) (time.Duration, error) {
	start := time.Now()
	if len(services) == 0 {
		return 0, nil
	}
	phaseExitCode := 0
	if err := s.sendControl(vmproto.TypePhaseStart, vmproto.PhaseStart{Name: "services"}); err != nil {
		return 0, err
	}
	defer func() {
		_ = s.sendControl(vmproto.TypePhaseEnd, vmproto.PhaseEnd{
			Name:       "services",
			ExitCode:   phaseExitCode,
			DurationMS: time.Since(start).Milliseconds(),
		})
	}()

	for _, service := range services {
		select {
		case env := <-controlCh:
			if env.Type == vmproto.TypeCancel {
				s.jobCancel()
			}
		default:
		}

		switch service {
		case "postgres":
			if err := preparePostgresRuntime(); err != nil {
				phaseExitCode = 1
				return 0, err
			}
			_, exitCode, err := s.runCommand(ctx, "service:postgres", postgresCommand(env), controlCh)
			if err != nil {
				phaseExitCode = exitCode
				return 0, err
			}
			if exitCode != 0 {
				phaseExitCode = exitCode
				return 0, fmt.Errorf("service postgres exited with code %d", exitCode)
			}
		default:
			phaseExitCode = 1
			return 0, fmt.Errorf("unsupported service %q", service)
		}
	}
	return time.Since(start), nil
}

func (s *agentSession) runPhase(ctx context.Context, controlCh <-chan vmproto.Envelope, label string, argv []string, workDir string, env []string) (time.Duration, int, error) {
	if len(argv) == 0 {
		return 0, 0, nil
	}
	spec, err := phaseCommand(argv, workDir, env)
	if err != nil {
		s.sendLogString("system", fmt.Sprintf("[init] %s resolve command: %v\n", label, err))
		return 0, 127, nil
	}
	duration, exitCode, err := s.runCommand(ctx, label, spec, controlCh)
	return duration, exitCode, err
}

type commandSpec struct {
	Path       string
	Args       []string
	WorkDir    string
	Env        []string
	Credential *syscall.Credential
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

func postgresCommand(env []string) commandSpec {
	return commandSpec{
		Path:    mustResolveCommand("pg_ctl"),
		Args:    []string{"pg_ctl", "start", "-D", "/var/lib/postgresql/data", "-l", "/tmp/pg.log", "-w"},
		WorkDir: "/var/lib/postgresql",
		Env:     envWithHome(env, "/var/lib/postgresql"),
		Credential: &syscall.Credential{
			Uid: postgresUID,
			Gid: postgresGID,
		},
	}
}

func preparePostgresRuntime() error {
	if err := os.MkdirAll("/run/postgresql", 0o775); err != nil {
		return fmt.Errorf("mkdir /run/postgresql: %w", err)
	}
	if err := os.Chown("/run/postgresql", postgresUID, postgresGID); err != nil {
		return fmt.Errorf("chown /run/postgresql: %w", err)
	}
	return nil
}

func mustResolveCommand(name string) string {
	path, err := resolveCommand(name)
	if err != nil {
		fatal("resolve command "+name, err)
	}
	return path
}

func (s *agentSession) runCommand(ctx context.Context, label string, spec commandSpec, controlCh <-chan vmproto.Envelope) (time.Duration, int, error) {
	start := time.Now()
	if err := s.sendControl(vmproto.TypePhaseStart, vmproto.PhaseStart{Name: label}); err != nil {
		return 0, 0, err
	}

	cmd := exec.Command(spec.Path, spec.Args[1:]...)
	cmd.Dir = spec.WorkDir
	cmd.Env = spec.Env
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if spec.Credential != nil {
		cmd.SysProcAttr.Credential = spec.Credential
	}

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return 0, 0, err
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return 0, 0, err
	}
	if err := cmd.Start(); err != nil {
		return 0, 127, err
	}

	s.activeChildPID.Store(int64(cmd.Process.Pid))

	var pumpWG sync.WaitGroup
	pumpWG.Add(2)
	go s.pumpStream("stdout", stdoutPipe, &pumpWG)
	go s.pumpStream("stderr", stderrPipe, &pumpWG)

	waitCh := make(chan error, 1)
	go func() {
		waitCh <- cmd.Wait()
	}()

	var waitErr error
	for {
		select {
		case waitErr = <-waitCh:
			pumpWG.Wait()
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
			if env.Type == vmproto.TypeCancel {
				s.jobCancel()
			}
		case err := <-s.errCh:
			terminateProcessGroup(cmd.Process.Pid)
			pumpWG.Wait()
			s.activeChildPID.Store(0)
			return time.Since(start), exitCodeFromWait(waitErr), err
		case <-ctx.Done():
			terminateProcessGroup(cmd.Process.Pid)
		}
	}
}

func (s *agentSession) pumpStream(stream string, reader io.ReadCloser, wg *sync.WaitGroup) {
	defer wg.Done()
	defer reader.Close()

	buf := make([]byte, 32*1024)
	for {
		n, err := reader.Read(buf)
		if n > 0 {
			chunk := append([]byte(nil), buf[:n]...)
			switch stream {
			case "stdout":
				s.stdoutBytes.Add(uint64(n))
			case "stderr":
				s.stderrBytes.Add(uint64(n))
			}
			s.sendLogChunk(stream, chunk)
		}
		if err != nil {
			if err != io.EOF {
				s.sendLogString("system", fmt.Sprintf("[init] stream %s error: %v\n", stream, err))
			}
			return
		}
	}
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
