package vmproto

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"testing"
)

func TestCodecRoundTrip(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	codec := NewCodec(&buf, &buf)

	wantPayload := RunRequest{
		RunID:             "job-1",
		WorkloadKind:      WorkloadKindForgejoWorkflow,
		RunnerClass:       "metal-4vcpu-ubuntu-2404",
		RunCommand:        []string{"true"},
		RunWorkDir:        "/workspace",
		WorkflowYAML:      "name: ci\n",
		WorkflowEnv:       map[string]string{"CI": "true"},
		WorkflowSecrets:   map[string]string{"TOKEN": "secret"},
		WorkflowEventName: "push",
		WorkflowInputs:    map[string]string{"name": "forge-metal"},
		GitHubJITConfig:   "encoded-jit",
		Network:           NetworkConfig{AddressCIDR: "172.16.0.2/30", Gateway: "172.16.0.1", LinkName: "eth0"},
		ProtocolVersion:   ProtocolVersion,
	}
	env, err := NewEnvelope(TypeRunRequest, 7, 1234, wantPayload)
	if err != nil {
		t.Fatalf("NewEnvelope: %v", err)
	}
	if err := codec.WriteEnvelope(env); err != nil {
		t.Fatalf("WriteEnvelope: %v", err)
	}

	gotEnv, err := codec.ReadEnvelope()
	if err != nil {
		t.Fatalf("ReadEnvelope: %v", err)
	}
	if gotEnv.Type != TypeRunRequest {
		t.Fatalf("type: got %s want %s", gotEnv.Type, TypeRunRequest)
	}
	gotPayload, err := DecodePayload[RunRequest](gotEnv)
	if err != nil {
		t.Fatalf("DecodePayload: %v", err)
	}
	if gotPayload.RunID != wantPayload.RunID {
		t.Fatalf("run_id: got %q want %q", gotPayload.RunID, wantPayload.RunID)
	}
	if gotPayload.Network.AddressCIDR != wantPayload.Network.AddressCIDR {
		t.Fatalf("network.address_cidr: got %q want %q", gotPayload.Network.AddressCIDR, wantPayload.Network.AddressCIDR)
	}
	if gotPayload.RunWorkDir != wantPayload.RunWorkDir {
		t.Fatalf("run work dir: got %q want %q", gotPayload.RunWorkDir, wantPayload.RunWorkDir)
	}
	if gotPayload.WorkloadKind != wantPayload.WorkloadKind || gotPayload.RunnerClass != wantPayload.RunnerClass {
		t.Fatalf("workload fields: got %q/%q want %q/%q", gotPayload.WorkloadKind, gotPayload.RunnerClass, wantPayload.WorkloadKind, wantPayload.RunnerClass)
	}
	if gotPayload.WorkflowYAML != wantPayload.WorkflowYAML || gotPayload.WorkflowEventName != wantPayload.WorkflowEventName {
		t.Fatalf("workflow fields not preserved: %#v", gotPayload)
	}
	if gotPayload.WorkflowEnv["CI"] != "true" || gotPayload.WorkflowSecrets["TOKEN"] != "secret" || gotPayload.WorkflowInputs["name"] != "forge-metal" {
		t.Fatalf("workflow maps not preserved: %#v", gotPayload)
	}
	if gotPayload.GitHubJITConfig != wantPayload.GitHubJITConfig {
		t.Fatalf("github jit config: got %q want %q", gotPayload.GitHubJITConfig, wantPayload.GitHubJITConfig)
	}
}

func TestValidateWorkloadKind(t *testing.T) {
	t.Parallel()

	for _, kind := range []string{"", WorkloadKindDirect, WorkloadKindForgejoWorkflow, WorkloadKindGitHubRunner} {
		if err := ValidateWorkloadKind(kind); err != nil {
			t.Fatalf("ValidateWorkloadKind(%q): %v", kind, err)
		}
	}
	if got := NormalizeWorkloadKind(""); got != WorkloadKindDirect {
		t.Fatalf("NormalizeWorkloadKind blank: got %q want %q", got, WorkloadKindDirect)
	}
	if err := ValidateWorkloadKind("python_in_space"); err == nil {
		t.Fatal("ValidateWorkloadKind accepted an unknown kind")
	}
}

func TestReadEnvelopeRejectsWrongVersion(t *testing.T) {
	t.Parallel()

	body := []byte(`{"v":99,"type":"heartbeat"}`)
	var buf bytes.Buffer
	buf.Write([]byte{0, 0, 0, byte(len(body))})
	buf.Write(body)

	_, err := NewCodec(&buf, io.Discard).ReadEnvelope()
	if err == nil || !errors.Is(err, io.EOF) && err.Error() != "unsupported protocol version 99" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestReadEnvelopeRejectsOversizedFrame(t *testing.T) {
	t.Parallel()

	var header [4]byte
	binary.BigEndian.PutUint32(header[:], 1024*1024+1)
	buf := bytes.NewBuffer(header[:])

	_, err := NewCodec(buf, io.Discard).ReadEnvelope()
	if !errors.Is(err, ErrFrameTooLarge) {
		t.Fatalf("expected oversized frame error, got %v", err)
	}
}

func TestWriteEnvelopeRejectsOversizedFrame(t *testing.T) {
	t.Parallel()

	env, err := NewEnvelope(TypeLogChunk, 1, 1, LogChunk{
		Stream: "stdout",
		Data:   bytes.Repeat([]byte{'x'}, 1024*1024),
	})
	if err != nil {
		t.Fatalf("NewEnvelope: %v", err)
	}

	err = NewCodec(io.Reader(bytes.NewReader(nil)), io.Discard).WriteEnvelope(env)
	if !errors.Is(err, ErrFrameTooLarge) {
		t.Fatalf("expected oversized frame error, got %v", err)
	}
}

func TestValidateCheckpointRef(t *testing.T) {
	t.Parallel()

	for _, value := range []string{"pg-demo", "deps.v1", "db_seed:2026", "A_1"} {
		if err := ValidateCheckpointRef(value); err != nil {
			t.Fatalf("expected %q to be valid: %v", value, err)
		}
	}

	for _, value := range []string{"", "../host", "has/slash", "-starts-with-dash", "has space"} {
		if err := ValidateCheckpointRef(value); err == nil {
			t.Fatalf("expected %q to be invalid", value)
		}
	}
}
