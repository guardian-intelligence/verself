package vmorchestrator

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"strings"
	"testing"
)

func TestConsumeGuestTelemetryStreamHelloFirstRequired(t *testing.T) {
	t.Parallel()

	observer := &captureTelemetryObserver{}
	stream := telemetryTestStream(
		telemetrySampleFrame(7),
	)

	err := consumeGuestTelemetryStream(context.Background(), stream, "job-1", observer, nil, nil)
	if !errors.Is(err, ErrTelemetryHelloFirst) {
		t.Fatalf("error = %v, want %v", err, ErrTelemetryHelloFirst)
	}
	if got := len(observer.events); got != 0 {
		t.Fatalf("observed events = %d, want 0", got)
	}
}

func TestConsumeGuestTelemetryStreamMonotonicSequence(t *testing.T) {
	t.Parallel()

	observer := &captureTelemetryObserver{}
	stream := telemetryTestStream(
		telemetryHelloFrame(10),
		telemetrySampleFrame(11),
		telemetrySampleFrame(12),
	)

	if err := consumeGuestTelemetryStream(context.Background(), stream, "job-1", observer, nil, nil); err != nil {
		t.Fatalf("consume stream: %v", err)
	}

	if got := len(observer.events); got != 3 {
		t.Fatalf("observed events = %d, want 3", got)
	}
	if observer.events[0].Hello == nil || observer.events[0].Hello.Seq != 10 {
		t.Fatalf("event[0] hello seq mismatch: %#v", observer.events[0].Hello)
	}
	if observer.events[1].Sample == nil || observer.events[1].Sample.Seq != 11 {
		t.Fatalf("event[1] sample seq mismatch: %#v", observer.events[1].Sample)
	}
	if observer.events[2].Sample == nil || observer.events[2].Sample.Seq != 12 {
		t.Fatalf("event[2] sample seq mismatch: %#v", observer.events[2].Sample)
	}
	for i, event := range observer.events {
		if event.Diagnostic != nil {
			t.Fatalf("event[%d] diagnostic = %#v, want nil", i, event.Diagnostic)
		}
	}
}

func TestConsumeGuestTelemetryStreamGapDiagnostic(t *testing.T) {
	t.Parallel()

	observer := &captureTelemetryObserver{}
	stream := telemetryTestStream(
		telemetryHelloFrame(10),
		telemetrySampleFrame(11),
		telemetrySampleFrame(13),
	)

	if err := consumeGuestTelemetryStream(context.Background(), stream, "job-1", observer, nil, nil); err != nil {
		t.Fatalf("consume stream: %v", err)
	}

	if got := len(observer.events); got != 4 {
		t.Fatalf("observed events = %d, want 4", got)
	}
	diag := observer.events[3].Diagnostic
	if diag == nil {
		t.Fatalf("event[3] diagnostic = nil, want non-nil")
	}
	if diag.Kind != TelemetryDiagnosticKindGap {
		t.Fatalf("diagnostic kind = %q, want %q", diag.Kind, TelemetryDiagnosticKindGap)
	}
	if diag.ExpectedSeq != 12 || diag.ObservedSeq != 13 || diag.MissingSamples != 1 {
		t.Fatalf("diagnostic mismatch: %+v", *diag)
	}
}

func TestConsumeGuestTelemetryStreamRegressionDiagnostic(t *testing.T) {
	t.Parallel()

	observer := &captureTelemetryObserver{}
	stream := telemetryTestStream(
		telemetryHelloFrame(10),
		telemetrySampleFrame(11),
		telemetrySampleFrame(11),
	)

	if err := consumeGuestTelemetryStream(context.Background(), stream, "job-1", observer, nil, nil); err != nil {
		t.Fatalf("consume stream: %v", err)
	}

	if got := len(observer.events); got != 3 {
		t.Fatalf("observed events = %d, want 3", got)
	}
	if samples := countTelemetrySamples(observer.events); samples != 1 {
		t.Fatalf("sample event count = %d, want 1", samples)
	}
	diag := observer.events[2].Diagnostic
	if diag == nil {
		t.Fatalf("event[2] diagnostic = nil, want non-nil")
	}
	if diag.Kind != TelemetryDiagnosticKindRegression {
		t.Fatalf("diagnostic kind = %q, want %q", diag.Kind, TelemetryDiagnosticKindRegression)
	}
	if diag.ExpectedSeq != 12 || diag.ObservedSeq != 11 || diag.MissingSamples != 0 {
		t.Fatalf("diagnostic mismatch: %+v", *diag)
	}
}

func TestConsumeGuestTelemetryStreamTruncatedFrameReturnsNil(t *testing.T) {
	t.Parallel()

	observer := &captureTelemetryObserver{}
	var payload bytes.Buffer
	hello := telemetryHelloFrame(10)
	sample := telemetrySampleFrame(11)
	payload.Write(hello[:])
	payload.Write(sample[:17])

	if err := consumeGuestTelemetryStream(context.Background(), &payload, "job-1", observer, nil, nil); err != nil {
		t.Fatalf("consume stream: %v", err)
	}
	if got := len(observer.events); got != 1 {
		t.Fatalf("observed events = %d, want 1", got)
	}
	if observer.events[0].Hello == nil || observer.events[0].Hello.Seq != 10 {
		t.Fatalf("event[0] hello seq mismatch: %#v", observer.events[0].Hello)
	}
}

func TestConsumeGuestTelemetryStreamInjectsGapFault(t *testing.T) {
	t.Parallel()

	profile, err := parseTelemetryFaultProfile("gap_once@12")
	if err != nil {
		t.Fatalf("parseTelemetryFaultProfile: %v", err)
	}

	observer := &captureTelemetryObserver{}
	stream := telemetryTestStream(
		telemetryHelloFrame(10),
		telemetrySampleFrame(11),
		telemetrySampleFrame(12),
		telemetrySampleFrame(13),
		telemetrySampleFrame(14),
	)

	if err := consumeGuestTelemetryStream(context.Background(), stream, "job-1", observer, nil, profile); err != nil {
		t.Fatalf("consume stream: %v", err)
	}

	if got := diagnosticCount(observer.events, TelemetryDiagnosticKindGap); got != 1 {
		t.Fatalf("gap diagnostics = %d, want 1", got)
	}
	if got := diagnosticCount(observer.events, TelemetryDiagnosticKindRegression); got != 0 {
		t.Fatalf("regression diagnostics = %d, want 0", got)
	}
}

func TestConsumeGuestTelemetryStreamInjectsRegressionFault(t *testing.T) {
	t.Parallel()

	profile, err := parseTelemetryFaultProfile("regression_once@12")
	if err != nil {
		t.Fatalf("parseTelemetryFaultProfile: %v", err)
	}

	observer := &captureTelemetryObserver{}
	stream := telemetryTestStream(
		telemetryHelloFrame(10),
		telemetrySampleFrame(11),
		telemetrySampleFrame(12),
		telemetrySampleFrame(13),
		telemetrySampleFrame(14),
	)

	if err := consumeGuestTelemetryStream(context.Background(), stream, "job-1", observer, nil, profile); err != nil {
		t.Fatalf("consume stream: %v", err)
	}

	if got := diagnosticCount(observer.events, TelemetryDiagnosticKindRegression); got != 1 {
		t.Fatalf("regression diagnostics = %d, want 1", got)
	}
	if got := diagnosticCount(observer.events, TelemetryDiagnosticKindGap); got != 0 {
		t.Fatalf("gap diagnostics = %d, want 0", got)
	}
}

func TestParseTelemetryFaultProfileRejectsInvalidValues(t *testing.T) {
	t.Parallel()

	_, err := parseTelemetryFaultProfile("totally_bad")
	if err == nil {
		t.Fatal("expected invalid fault profile error")
	}
	if !strings.Contains(err.Error(), "unsupported telemetry fault profile") {
		t.Fatalf("unexpected parse error: %v", err)
	}
}

type captureTelemetryObserver struct {
	events []TelemetryEvent
}

func (o *captureTelemetryObserver) OnGuestLogChunk(string, string)            {}
func (o *captureTelemetryObserver) OnGuestPhaseStart(string, string)          {}
func (o *captureTelemetryObserver) OnGuestPhaseEnd(string, PhaseResult)       {}
func (o *captureTelemetryObserver) OnGuestCheckpoint(string, CheckpointEvent) {}
func (o *captureTelemetryObserver) OnTelemetryEvent(event TelemetryEvent) {
	o.events = append(o.events, event)
}

func telemetryTestStream(frames ...[guestTelemetryFrameLen]byte) *bytes.Reader {
	payload := make([]byte, 0, len(frames)*guestTelemetryFrameLen)
	for _, frame := range frames {
		payload = append(payload, frame[:]...)
	}
	return bytes.NewReader(payload)
}

func telemetryHelloFrame(seq uint32) [guestTelemetryFrameLen]byte {
	return telemetryFrame(TelemetryFrameKindHello, seq)
}

func telemetrySampleFrame(seq uint32) [guestTelemetryFrameLen]byte {
	return telemetryFrame(TelemetryFrameKindSample, seq)
}

func telemetryFrame(kind TelemetryFrameKind, seq uint32) [guestTelemetryFrameLen]byte {
	var frame [guestTelemetryFrameLen]byte
	binary.LittleEndian.PutUint32(frame[0:4], guestTelemetryMagic)
	binary.LittleEndian.PutUint16(frame[4:6], guestTelemetryVersion)
	binary.LittleEndian.PutUint16(frame[6:8], uint16(kind))
	binary.LittleEndian.PutUint32(frame[8:12], seq)
	return frame
}

func countTelemetrySamples(events []TelemetryEvent) int {
	count := 0
	for _, event := range events {
		if event.Sample != nil {
			count++
		}
	}
	return count
}

func diagnosticCount(events []TelemetryEvent, kind TelemetryDiagnosticKind) int {
	count := 0
	for _, event := range events {
		if event.Diagnostic == nil {
			continue
		}
		if event.Diagnostic.Kind == kind {
			count++
		}
	}
	return count
}
