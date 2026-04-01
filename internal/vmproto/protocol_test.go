package vmproto

import (
	"bytes"
	"errors"
	"io"
	"testing"
)

func TestCodecRoundTrip(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	codec := NewCodec(&buf, &buf)

	wantPayload := RunRequest{
		JobID:           "job-1",
		RunCommand:      []string{"npm", "test"},
		Network:         NetworkConfig{AddressCIDR: "172.16.0.2/30", Gateway: "172.16.0.1", LinkName: "eth0"},
		ProtocolVersion: ProtocolVersion,
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
	if gotPayload.JobID != wantPayload.JobID {
		t.Fatalf("job_id: got %q want %q", gotPayload.JobID, wantPayload.JobID)
	}
	if gotPayload.Network.AddressCIDR != wantPayload.Network.AddressCIDR {
		t.Fatalf("network.address_cidr: got %q want %q", gotPayload.Network.AddressCIDR, wantPayload.Network.AddressCIDR)
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
