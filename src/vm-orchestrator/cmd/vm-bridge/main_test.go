package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/forge-metal/vm-orchestrator/vmproto"
)

func TestBuildRuntimeEnvUsesExplicitRegistry(t *testing.T) {
	t.Parallel()

	env, err := buildRuntimeEnv(map[string]string{
		"FORGE_METAL_NPM_REGISTRY": "http://10.0.0.1:4873",
	}, vmproto.NetworkConfig{})
	if err != nil {
		t.Fatalf("buildRuntimeEnv: %v", err)
	}

	values := map[string]string{}
	for _, entry := range env {
		key, value, found := strings.Cut(entry, "=")
		if !found {
			t.Fatalf("malformed env entry: %q", entry)
		}
		values[key] = value
	}

	if values["FORGE_METAL_NPM_REGISTRY"] != "http://10.0.0.1:4873" {
		t.Fatalf("FORGE_METAL_NPM_REGISTRY: got %q", values["FORGE_METAL_NPM_REGISTRY"])
	}
	if values["NPM_CONFIG_REGISTRY"] != "http://10.0.0.1:4873" {
		t.Fatalf("NPM_CONFIG_REGISTRY: got %q", values["NPM_CONFIG_REGISTRY"])
	}
	if values["npm_config_registry"] != "http://10.0.0.1:4873" {
		t.Fatalf("npm_config_registry: got %q", values["npm_config_registry"])
	}
	if values["BUN_CONFIG_REGISTRY"] != "http://10.0.0.1:4873" {
		t.Fatalf("BUN_CONFIG_REGISTRY: got %q", values["BUN_CONFIG_REGISTRY"])
	}
}

func TestBuildRuntimeEnvUsesHostServicePlane(t *testing.T) {
	t.Parallel()

	env, err := buildRuntimeEnv(nil, vmproto.NetworkConfig{
		HostServiceIP:   "10.255.0.1",
		HostServicePort: 18080,
	})
	if err != nil {
		t.Fatalf("buildRuntimeEnv: %v", err)
	}

	values := map[string]string{}
	for _, entry := range env {
		key, value, found := strings.Cut(entry, "=")
		if !found {
			t.Fatalf("malformed env entry: %q", entry)
		}
		values[key] = value
	}

	if values["FORGE_METAL_HOST_SERVICE_IP"] != "10.255.0.1" {
		t.Fatalf("FORGE_METAL_HOST_SERVICE_IP: got %q", values["FORGE_METAL_HOST_SERVICE_IP"])
	}
	if values["FORGE_METAL_HOST_SERVICE_HTTP_ORIGIN"] != "http://10.255.0.1:18080" {
		t.Fatalf("FORGE_METAL_HOST_SERVICE_HTTP_ORIGIN: got %q", values["FORGE_METAL_HOST_SERVICE_HTTP_ORIGIN"])
	}
	if values["NPM_CONFIG_REGISTRY"] != "http://10.255.0.1:4873" {
		t.Fatalf("NPM_CONFIG_REGISTRY: got %q", values["NPM_CONFIG_REGISTRY"])
	}
	if values["FORGE_METAL_VM_BRIDGE_SOCKET"] != bridgeSocketPath {
		t.Fatalf("FORGE_METAL_VM_BRIDGE_SOCKET: got %q", values["FORGE_METAL_VM_BRIDGE_SOCKET"])
	}
}

func TestBuildRuntimeEnvDoesNotForceCIOrRegistry(t *testing.T) {
	t.Parallel()

	env, err := buildRuntimeEnv(nil, vmproto.NetworkConfig{})
	if err != nil {
		t.Fatalf("buildRuntimeEnv: %v", err)
	}

	values := map[string]string{}
	for _, entry := range env {
		key, value, found := strings.Cut(entry, "=")
		if !found {
			t.Fatalf("malformed env entry: %q", entry)
		}
		values[key] = value
	}

	if _, ok := values["CI"]; ok {
		t.Fatalf("CI should be explicitly supplied by the caller, got %q", values["CI"])
	}
	if _, ok := values["NPM_CONFIG_REGISTRY"]; ok {
		t.Fatalf("NPM_CONFIG_REGISTRY should not be injected without an explicit registry or host-service plane, got %q", values["NPM_CONFIG_REGISTRY"])
	}
}

func TestNormalizeWorkDirFallsBackToWorkspace(t *testing.T) {
	t.Parallel()

	if got := normalizeWorkDir("   "); got != defaultWorkDir {
		t.Fatalf("normalizeWorkDir blank: got %q want %q", got, defaultWorkDir)
	}
	if got := normalizeWorkDir("/workspace/apps/web"); got != "/workspace/apps/web" {
		t.Fatalf("normalizeWorkDir explicit: got %q", got)
	}
}

func TestRunCLIHelp(t *testing.T) {
	t.Parallel()

	var stdout bytes.Buffer
	if err := runCLI([]string{"--help"}, &stdout, &bytes.Buffer{}); err != nil {
		t.Fatalf("runCLI help: %v", err)
	}
	if got := stdout.String(); !strings.Contains(got, "vm-bridge snapshot save <ref>") {
		t.Fatalf("help output: %q", got)
	}
}

func TestRunCLIRejectsInvalidSnapshotRefBeforeDial(t *testing.T) {
	t.Parallel()

	err := runCLI([]string{"snapshot", "save", "../host"}, &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected invalid ref error")
	}
	if strings.Contains(err.Error(), "connect vm-bridge") {
		t.Fatalf("expected validation before local socket dial, got %v", err)
	}
}

func TestWaitForRunRequestRejectsProtocolVersionMismatch(t *testing.T) {
	t.Parallel()

	session := &agentSession{
		errCh:     make(chan error, 1),
		jobCancel: func() {},
	}
	controlCh := make(chan vmproto.Envelope, 1)
	controlCh <- mustEnvelope(t, vmproto.TypeRunRequest, 1, vmproto.RunRequest{
		ProtocolVersion: vmproto.ProtocolVersion + 1,
	})

	_, err := session.waitForRunRequest(controlCh)
	if err == nil {
		t.Fatal("expected protocol version mismatch")
	}
	if !strings.Contains(err.Error(), "await_run_request") {
		t.Fatalf("expected deterministic state in error, got %v", err)
	}
	if !strings.Contains(err.Error(), "protocol_version") {
		t.Fatalf("expected protocol_version mismatch detail, got %v", err)
	}
}

func TestWaitForRunRequestRejectsUnexpectedControlFrame(t *testing.T) {
	t.Parallel()

	session := &agentSession{
		errCh:     make(chan error, 1),
		jobCancel: func() {},
	}
	controlCh := make(chan vmproto.Envelope, 1)
	controlCh <- mustEnvelope(t, vmproto.TypeHeartbeat, 1, vmproto.Heartbeat{})

	_, err := session.waitForRunRequest(controlCh)
	if err == nil {
		t.Fatal("expected protocol violation for unexpected control frame")
	}
	if !strings.Contains(err.Error(), "await_run_request") {
		t.Fatalf("expected deterministic state in error, got %v", err)
	}
	if !strings.Contains(err.Error(), string(vmproto.TypeHeartbeat)) {
		t.Fatalf("expected offending type in error, got %v", err)
	}
}

func TestWaitForResultAckRejectsMismatchedForType(t *testing.T) {
	t.Parallel()

	session := &agentSession{
		errCh:     make(chan error, 1),
		jobCancel: func() {},
	}
	controlCh := make(chan vmproto.Envelope, 1)
	controlCh <- mustEnvelope(t, vmproto.TypeAck, 2, vmproto.Ack{
		ForType: vmproto.TypePhaseEnd,
		ForSeq:  99,
	})

	err := session.waitForResultAck(controlCh, 99)
	if err == nil {
		t.Fatal("expected ack for_type violation")
	}
	if !strings.Contains(err.Error(), "await_result_ack") {
		t.Fatalf("expected deterministic state in error, got %v", err)
	}
	if !strings.Contains(err.Error(), "for_type") {
		t.Fatalf("expected for_type detail, got %v", err)
	}
}

func TestWaitForResultAckRejectsMismatchedForSeq(t *testing.T) {
	t.Parallel()

	session := &agentSession{
		errCh:     make(chan error, 1),
		jobCancel: func() {},
	}
	controlCh := make(chan vmproto.Envelope, 1)
	controlCh <- mustEnvelope(t, vmproto.TypeAck, 3, vmproto.Ack{
		ForType: vmproto.TypeResult,
		ForSeq:  42,
	})

	err := session.waitForResultAck(controlCh, 43)
	if err == nil {
		t.Fatal("expected ack for_seq violation")
	}
	if !strings.Contains(err.Error(), "await_result_ack") {
		t.Fatalf("expected deterministic state in error, got %v", err)
	}
	if !strings.Contains(err.Error(), "for_seq") {
		t.Fatalf("expected for_seq detail, got %v", err)
	}
}

func TestWaitForResultAckRejectsUnexpectedControlFrame(t *testing.T) {
	t.Parallel()

	session := &agentSession{
		errCh:     make(chan error, 1),
		jobCancel: func() {},
	}
	controlCh := make(chan vmproto.Envelope, 1)
	controlCh <- mustEnvelope(t, vmproto.TypeShutdown, 4, vmproto.Shutdown{})

	err := session.waitForResultAck(controlCh, 7)
	if err == nil {
		t.Fatal("expected shutdown-before-ack violation")
	}
	if !strings.Contains(err.Error(), "await_result_ack") {
		t.Fatalf("expected deterministic state in error, got %v", err)
	}
	if !strings.Contains(err.Error(), string(vmproto.TypeShutdown)) {
		t.Fatalf("expected offending type in error, got %v", err)
	}
}

func TestWaitForResultAckAcceptsMatchingAck(t *testing.T) {
	t.Parallel()

	session := &agentSession{
		errCh:     make(chan error, 1),
		jobCancel: func() {},
	}
	controlCh := make(chan vmproto.Envelope, 1)
	controlCh <- mustEnvelope(t, vmproto.TypeAck, 5, vmproto.Ack{
		ForType: vmproto.TypeResult,
		ForSeq:  9,
	})

	if err := session.waitForResultAck(controlCh, 9); err != nil {
		t.Fatalf("waitForResultAck: %v", err)
	}
}

func TestWaitForShutdownRejectsUnexpectedControlFrame(t *testing.T) {
	t.Parallel()

	session := &agentSession{
		errCh:     make(chan error, 1),
		jobCancel: func() {},
	}
	controlCh := make(chan vmproto.Envelope, 1)
	controlCh <- mustEnvelope(t, vmproto.TypeAck, 5, vmproto.Ack{
		ForType: vmproto.TypeResult,
		ForSeq:  9,
	})

	err := session.waitForShutdown(controlCh)
	if err == nil {
		t.Fatal("expected unexpected-frame violation")
	}
	if !strings.Contains(err.Error(), "await_shutdown") {
		t.Fatalf("expected deterministic state in error, got %v", err)
	}
}

func TestWaitForShutdownAcceptsShutdown(t *testing.T) {
	t.Parallel()

	session := &agentSession{
		errCh:     make(chan error, 1),
		jobCancel: func() {},
	}
	controlCh := make(chan vmproto.Envelope, 1)
	controlCh <- mustEnvelope(t, vmproto.TypeShutdown, 6, vmproto.Shutdown{})

	if err := session.waitForShutdown(controlCh); err != nil {
		t.Fatalf("waitForShutdown: %v", err)
	}
}

func TestHandleRunPhaseControlAcceptsCancel(t *testing.T) {
	t.Parallel()

	canceled := false
	session := &agentSession{
		errCh: make(chan error, 1),
		jobCancel: func() {
			canceled = true
		},
	}

	if err := session.handleRunPhaseControl(mustEnvelope(t, vmproto.TypeCancel, 7, vmproto.Cancel{})); err != nil {
		t.Fatalf("handleRunPhaseControl: %v", err)
	}
	if !canceled {
		t.Fatal("expected cancel callback to be invoked")
	}
}

func TestHandleRunPhaseControlRejectsUnexpectedFrame(t *testing.T) {
	t.Parallel()

	session := &agentSession{
		errCh:     make(chan error, 1),
		jobCancel: func() {},
	}

	err := session.handleRunPhaseControl(mustEnvelope(t, vmproto.TypeHeartbeat, 8, vmproto.Heartbeat{}))
	if err == nil {
		t.Fatal("expected unexpected-frame violation")
	}
	if !strings.Contains(err.Error(), "run_phase") {
		t.Fatalf("expected deterministic state in error, got %v", err)
	}
}

func mustEnvelope(t *testing.T, msgType vmproto.MessageType, seq uint64, payload any) vmproto.Envelope {
	t.Helper()

	env, err := vmproto.NewEnvelope(msgType, seq, 1, payload)
	if err != nil {
		t.Fatalf("new envelope %s: %v", msgType, err)
	}
	return env
}
