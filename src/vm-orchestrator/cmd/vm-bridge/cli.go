package main

import (
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"time"

	"github.com/verself/vm-orchestrator/vmproto"
)

const bridgeClientTimeout = 30 * time.Second

func runCLI(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return usageError()
	}

	switch args[0] {
	case "snapshot":
		return runSnapshotCLI(args[1:], stdout)
	case "-h", "--help", "help":
		printUsage(stdout)
		return nil
	default:
		return usageError()
	}
}

func runSnapshotCLI(args []string, stdout io.Writer) error {
	if len(args) != 2 || args[0] != "save" {
		return usageError()
	}

	ref := strings.TrimSpace(args[1])
	if err := vmproto.ValidateCheckpointRef(ref); err != nil {
		return err
	}

	resp, err := sendLocalCheckpointRequest(vmproto.CheckpointRequest{
		RequestID: newCheckpointRequestID(),
		Operation: vmproto.CheckpointOperationSave,
		Ref:       ref,
	})
	if err != nil {
		return err
	}
	if !resp.Accepted {
		if resp.Error == "" {
			return fmt.Errorf("snapshot save %q was rejected", ref)
		}
		return fmt.Errorf("snapshot save %q was rejected: %s", ref, resp.Error)
	}

	if resp.VersionID != "" {
		fmt.Fprintf(stdout, "snapshot saved ref=%s version=%s\n", resp.Ref, resp.VersionID)
		return nil
	}
	fmt.Fprintf(stdout, "snapshot saved ref=%s\n", resp.Ref)
	return nil
}

func sendLocalCheckpointRequest(req vmproto.CheckpointRequest) (vmproto.CheckpointResponse, error) {
	socketPath := strings.TrimSpace(os.Getenv("VERSELF_VM_BRIDGE_SOCKET"))
	if socketPath == "" {
		socketPath = bridgeSocketPath
	}

	var dialer net.Dialer
	conn, err := dialer.Dial("unix", socketPath)
	if err != nil {
		return vmproto.CheckpointResponse{}, fmt.Errorf("connect vm-bridge at %s: %w", socketPath, err)
	}
	defer conn.Close()

	if err := conn.SetDeadline(time.Now().Add(bridgeClientTimeout)); err != nil {
		return vmproto.CheckpointResponse{}, fmt.Errorf("set vm-bridge deadline: %w", err)
	}

	codec := vmproto.NewCodec(conn, conn)
	env, err := vmproto.NewEnvelope(vmproto.TypeCheckpointRequest, 1, time.Now().UnixNano(), req)
	if err != nil {
		return vmproto.CheckpointResponse{}, err
	}
	if err := codec.WriteEnvelope(env); err != nil {
		return vmproto.CheckpointResponse{}, fmt.Errorf("send checkpoint request: %w", err)
	}

	respEnv, err := codec.ReadEnvelope()
	if err != nil {
		return vmproto.CheckpointResponse{}, fmt.Errorf("read checkpoint response: %w", err)
	}
	if respEnv.Type != vmproto.TypeCheckpointResponse {
		return vmproto.CheckpointResponse{}, fmt.Errorf("expected checkpoint_response, got %s", respEnv.Type)
	}
	resp, err := vmproto.DecodePayload[vmproto.CheckpointResponse](respEnv)
	if err != nil {
		return vmproto.CheckpointResponse{}, err
	}
	if resp.RequestID != req.RequestID {
		return vmproto.CheckpointResponse{}, fmt.Errorf("checkpoint response request_id mismatch: got %q want %q", resp.RequestID, req.RequestID)
	}
	return resp, nil
}

func newCheckpointRequestID() string {
	return fmt.Sprintf("%d-%d", os.Getpid(), time.Now().UnixNano())
}

func usageError() error {
	return fmt.Errorf("usage: vm-bridge snapshot save <ref>")
}

func printUsage(w io.Writer) {
	fmt.Fprintln(w, "usage: vm-bridge snapshot save <ref>")
}
