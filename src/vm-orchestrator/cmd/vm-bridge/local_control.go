package main

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"syscall"
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
		listener.Close()
		return nil, fmt.Errorf("chown %s: %w", bridgeSocketPath, err)
	}
	if err := os.Chmod(bridgeSocketPath, 0o660); err != nil {
		listener.Close()
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
		listener.Close()
		<-done
		_ = os.Remove(bridgeSocketPath)
		cleanupEmptySocketDir(bridgeSocketDir)
	}
	return stop, nil
}

func (s *agentSession) handleLocalControlConn(parent context.Context, conn net.Conn) {
	defer conn.Close()

	if err := conn.SetDeadline(time.Now().Add(bridgeClientTimeout)); err != nil {
		return
	}

	codec := vmproto.NewCodec(conn, conn)
	env, err := codec.ReadEnvelope()
	if err != nil {
		return
	}
	if env.Type != vmproto.TypeCheckpointRequest {
		writeLocalCheckpointResponse(codec, vmproto.CheckpointResponse{
			Accepted: false,
			Error:    fmt.Sprintf("unsupported local request type %s", env.Type),
		})
		return
	}

	req, err := vmproto.DecodePayload[vmproto.CheckpointRequest](env)
	if err != nil {
		writeLocalCheckpointResponse(codec, vmproto.CheckpointResponse{
			Accepted: false,
			Error:    err.Error(),
		})
		return
	}

	ctx, cancel := context.WithTimeout(parent, bridgeClientTimeout)
	defer cancel()

	resp := s.requestCheckpoint(ctx, req)
	writeLocalCheckpointResponse(codec, resp)
}

func writeLocalCheckpointResponse(codec *vmproto.Codec, resp vmproto.CheckpointResponse) {
	env, err := vmproto.NewEnvelope(vmproto.TypeCheckpointResponse, 1, time.Now().UnixNano(), resp)
	if err != nil {
		return
	}
	_ = codec.WriteEnvelope(env)
}

func (s *agentSession) requestCheckpoint(ctx context.Context, req vmproto.CheckpointRequest) vmproto.CheckpointResponse {
	if req.Operation == "" {
		req.Operation = vmproto.CheckpointOperationSave
	}
	if req.RequestID == "" {
		req.RequestID = newCheckpointRequestID()
	}

	resp := vmproto.CheckpointResponse{
		RequestID: req.RequestID,
		Operation: req.Operation,
		Ref:       req.Ref,
	}
	if err := vmproto.ValidateCheckpointRequest(req); err != nil {
		resp.Error = err.Error()
		return resp
	}

	ch := make(chan vmproto.CheckpointResponse, 1)
	if err := s.addCheckpointWaiter(req.RequestID, ch); err != nil {
		resp.Error = err.Error()
		return resp
	}
	defer s.removeCheckpointWaiter(req.RequestID)

	syscall.Sync()
	if err := s.sendControl(vmproto.TypeCheckpointRequest, req); err != nil {
		resp.Error = err.Error()
		return resp
	}

	select {
	case out := <-ch:
		return out
	case <-ctx.Done():
		resp.Error = ctx.Err().Error()
		return resp
	}
}

func (s *agentSession) addCheckpointWaiter(requestID string, ch chan vmproto.CheckpointResponse) error {
	s.checkpointMu.Lock()
	defer s.checkpointMu.Unlock()
	if s.checkpointWaiters == nil {
		s.checkpointWaiters = make(map[string]chan vmproto.CheckpointResponse)
	}
	if _, exists := s.checkpointWaiters[requestID]; exists {
		return fmt.Errorf("duplicate checkpoint request_id %q", requestID)
	}
	s.checkpointWaiters[requestID] = ch
	return nil
}

func (s *agentSession) removeCheckpointWaiter(requestID string) {
	s.checkpointMu.Lock()
	delete(s.checkpointWaiters, requestID)
	s.checkpointMu.Unlock()
}

func (s *agentSession) deliverCheckpointResponse(resp vmproto.CheckpointResponse) bool {
	s.checkpointMu.Lock()
	ch, ok := s.checkpointWaiters[resp.RequestID]
	if ok {
		delete(s.checkpointWaiters, resp.RequestID)
	}
	s.checkpointMu.Unlock()
	if !ok {
		return false
	}

	select {
	case ch <- resp:
	default:
	}
	return true
}

func cleanupEmptySocketDir(path string) {
	if filepath.Clean(path) == "/" {
		return
	}
	_ = os.Remove(path)
}
