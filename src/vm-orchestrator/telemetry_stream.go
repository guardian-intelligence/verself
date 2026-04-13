package vmorchestrator

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strconv"
	"strings"
	"time"
)

var ErrTelemetryHelloFirst = errors.New("telemetry stream first frame must be hello")

const telemetryFaultProfileEnvVar = "FORGE_METAL_TELEMETRY_FAULT_PROFILE"

func streamGuestTelemetry(ctx context.Context, udsPath, runID string, observer RunObserver, logger *slog.Logger, faultProfile *telemetryFaultProfile) error {
	observer = normalizeRunObserver(observer)

	conn, reader, err := connectGuestBridge(ctx, udsPath, guestTelemetryPort)
	if err != nil {
		return err
	}
	defer conn.Close()

	return consumeGuestTelemetryStream(ctx, reader, runID, observer, logger, faultProfile)
}

type telemetryFaultProfileKind uint8

const (
	telemetryFaultProfileKindGapOnce telemetryFaultProfileKind = iota + 1
	telemetryFaultProfileKindRegressionOnce
)

type telemetryFaultProfile struct {
	kind      telemetryFaultProfileKind
	targetSeq uint32
	injected  bool
	seqDelta  int8
}

func parseTelemetryFaultProfile(raw string) (*telemetryFaultProfile, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}

	switch {
	case strings.HasPrefix(raw, "gap_once@"):
		seq, err := parseTelemetryFaultSeq(strings.TrimPrefix(raw, "gap_once@"))
		if err != nil || seq == ^uint32(0) {
			return nil, fmt.Errorf("unsupported telemetry fault profile: %q", raw)
		}
		return &telemetryFaultProfile{
			kind:      telemetryFaultProfileKindGapOnce,
			targetSeq: seq,
			seqDelta:  1,
		}, nil
	case strings.HasPrefix(raw, "regression_once@"):
		seq, err := parseTelemetryFaultSeq(strings.TrimPrefix(raw, "regression_once@"))
		if err != nil || seq == 0 {
			return nil, fmt.Errorf("unsupported telemetry fault profile: %q", raw)
		}
		return &telemetryFaultProfile{
			kind:      telemetryFaultProfileKindRegressionOnce,
			targetSeq: seq,
			seqDelta:  -1,
		}, nil
	default:
		return nil, fmt.Errorf("unsupported telemetry fault profile: %q", raw)
	}
}

func parseTelemetryFaultSeq(raw string) (uint32, error) {
	if raw == "" {
		return 0, errors.New("missing seq")
	}
	n, err := strconv.ParseUint(raw, 10, 32)
	if err != nil {
		return 0, err
	}
	return uint32(n), nil
}

func injectTelemetryFault(profile *telemetryFaultProfile, event *TelemetryEvent) {
	if profile == nil || event == nil || event.Sample == nil {
		return
	}
	if !profile.injected {
		if event.Sample.Seq != profile.targetSeq {
			return
		}
		profile.injected = true
	}

	if profile.seqDelta == 0 {
		return
	}

	switch profile.seqDelta {
	case 1:
		if event.Sample.Seq == ^uint32(0) {
			return
		}
		event.Sample.Seq++
	case -1:
		if event.Sample.Seq == 0 {
			return
		}
		event.Sample.Seq--
	}
}

func consumeGuestTelemetryStream(ctx context.Context, reader io.Reader, runID string, observer RunObserver, logger *slog.Logger, faultProfile *telemetryFaultProfile) error {
	validator := telemetryStreamValidator{}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		event, err := ReadTelemetryFrame(reader)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
				return nil
			}
			return err
		}
		event.RunID = runID
		event.ReceivedAtUnix = time.Now().UTC()
		injectTelemetryFault(faultProfile, &event)

		emitFrame, diagnostic, err := validator.validate(event)
		if err != nil {
			return err
		}
		if emitFrame {
			logTelemetryFrame(ctx, logger, runID, event)
			observer.OnTelemetryEvent(event)
		}
		if diagnostic != nil {
			logTelemetryDiagnostic(ctx, logger, runID, *diagnostic)
			observer.OnTelemetryEvent(TelemetryEvent{
				RunID:          runID,
				ReceivedAtUnix: event.ReceivedAtUnix,
				Diagnostic:     diagnostic,
			})
		}
	}
}

type telemetryStreamValidator struct {
	seenFirst         bool
	expectedSampleSeq uint32
}

func (v *telemetryStreamValidator) validate(event TelemetryEvent) (emitFrame bool, diagnostic *TelemetryDiagnostic, err error) {
	if !v.seenFirst {
		v.seenFirst = true
		if event.Hello == nil {
			return false, nil, fmt.Errorf("%w: got %s", ErrTelemetryHelloFirst, telemetryEventKind(event))
		}
	}

	if event.Hello != nil {
		v.expectedSampleSeq = event.Hello.Seq + 1
		return true, nil, nil
	}
	if event.Sample == nil {
		return true, nil, nil
	}

	observed := event.Sample.Seq
	expected := v.expectedSampleSeq

	switch {
	case observed == expected:
		v.expectedSampleSeq = observed + 1
		return true, nil, nil
	case observed > expected:
		v.expectedSampleSeq = observed + 1
		return true, &TelemetryDiagnostic{
			Kind:           TelemetryDiagnosticKindGap,
			ExpectedSeq:    expected,
			ObservedSeq:    observed,
			MissingSamples: observed - expected,
		}, nil
	default:
		return false, &TelemetryDiagnostic{
			Kind:           TelemetryDiagnosticKindRegression,
			ExpectedSeq:    expected,
			ObservedSeq:    observed,
			MissingSamples: 0,
		}, nil
	}
}

func telemetryEventKind(event TelemetryEvent) string {
	switch {
	case event.Hello != nil:
		return "hello"
	case event.Sample != nil:
		return "sample"
	default:
		return "unknown"
	}
}

func logTelemetryFrame(ctx context.Context, logger *slog.Logger, runID string, event TelemetryEvent) {
	if logger == nil {
		return
	}
	switch {
	case event.Hello != nil:
		logger.InfoContext(ctx, "guest telemetry hello received", "run_id", runID, "boot_id", event.Hello.BootID, "seq", event.Hello.Seq)
	case event.Sample != nil:
		logger.DebugContext(ctx, "guest telemetry sample received", "run_id", runID, "seq", event.Sample.Seq)
	}
}

func logTelemetryDiagnostic(ctx context.Context, logger *slog.Logger, runID string, diagnostic TelemetryDiagnostic) {
	if logger == nil {
		return
	}
	logger.WarnContext(ctx,
		"guest telemetry stream diagnostic",
		"run_id", runID,
		"kind", string(diagnostic.Kind),
		"expected_seq", diagnostic.ExpectedSeq,
		"observed_seq", diagnostic.ObservedSeq,
		"missing_samples", diagnostic.MissingSamples,
	)
}
