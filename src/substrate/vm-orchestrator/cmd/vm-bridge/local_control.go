package main

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"

	"github.com/verself/vm-orchestrator/vmproto"
)

const (
	bridgeSocketDir  = "/run/verself"
	bridgeSocketPath = bridgeSocketDir + "/vm-bridge.sock"
)

func (s *agentSession) startLocalControlServer(ctx context.Context) (func(), error) {
	if err := os.MkdirAll(bridgeSocketDir, 0o755); err != nil {
		return nil, fmt.Errorf("mkdir %s: %w", bridgeSocketDir, err)
	}
	if err := os.RemoveAll(bridgeSocketPath); err != nil {
		return nil, fmt.Errorf("remove stale %s: %w", bridgeSocketPath, err)
	}

	listener, err := net.Listen("unix", bridgeSocketPath)
	if err != nil {
		return nil, fmt.Errorf("listen %s: %w", bridgeSocketPath, err)
	}
	if err := os.Chown(bridgeSocketPath, runnerUID, runnerGID); err != nil {
		_ = listener.Close()
		return nil, fmt.Errorf("chown %s: %w", bridgeSocketPath, err)
	}
	if err := os.Chmod(bridgeSocketPath, 0o660); err != nil {
		_ = listener.Close()
		return nil, fmt.Errorf("chmod %s: %w", bridgeSocketPath, err)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			conn, err := listener.Accept()
			if err != nil {
				select {
				case <-ctx.Done():
				default:
					s.sendLogString("", "system", fmt.Sprintf("%s local control accept failed: %v\n", logPrefix, err))
				}
				return
			}
			go s.handleLocalControlConn(ctx, conn)
		}
	}()

	stop := func() {
		_ = listener.Close()
		<-done
		_ = os.Remove(bridgeSocketPath)
		cleanupEmptySocketDir(bridgeSocketDir)
	}
	return stop, nil
}

func (s *agentSession) handleLocalControlConn(parent context.Context, conn net.Conn) {
	defer func() { _ = conn.Close() }()

	if err := conn.SetDeadline(time.Now().Add(bridgeClientTimeout)); err != nil {
		return
	}

	codec := vmproto.NewCodec(conn, conn)
	env, err := codec.ReadEnvelope()
	if err != nil {
		return
	}
	switch env.Type {
	case vmproto.TypeFilesystemMountRequest:
	default:
		writeLocalFilesystemMountResult(codec, vmproto.FilesystemMountResult{
			Mounted: false,
			Error:   fmt.Sprintf("unsupported local request type %s", env.Type),
		})
		return
	}

	req, err := vmproto.DecodePayload[vmproto.FilesystemMountRequest](env)
	if err != nil {
		writeLocalFilesystemMountResult(codec, vmproto.FilesystemMountResult{
			Mounted: false,
			Error:   err.Error(),
		})
		return
	}
	_ = parent
	result := s.mountFilesystem(req.Filesystem)
	writeLocalFilesystemMountResult(codec, result)
}

func writeLocalFilesystemMountResult(codec *vmproto.Codec, resp vmproto.FilesystemMountResult) {
	env, err := vmproto.NewEnvelope(vmproto.TypeFilesystemMountResult, 1, time.Now().UnixNano(), resp)
	if err != nil {
		return
	}
	_ = codec.WriteEnvelope(env)
}

func cleanupEmptySocketDir(path string) {
	if filepath.Clean(path) == "/" {
		return
	}
	_ = os.Remove(path)
}
