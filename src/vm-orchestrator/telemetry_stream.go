package vmorchestrator

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"time"
)

var ErrTelemetryHelloFirst = errors.New("telemetry stream first frame must be hello")

func streamGuestTelemetry(ctx context.Context, udsPath, jobID string, observer RunObserver, logger *slog.Logger) error {
	observer = normalizeRunObserver(observer)

	conn, reader, err := connectGuestBridge(ctx, udsPath, guestTelemetryPort)
	if err != nil {
		return err
	}
	defer conn.Close()

	return consumeGuestTelemetryStream(ctx, reader, jobID, observer, logger)
}

func consumeGuestTelemetryStream(ctx context.Context, reader io.Reader, jobID string, observer RunObserver, logger *slog.Logger) error {
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
		event.JobID = jobID
		event.ReceivedAtUnix = time.Now().UTC()

		emitFrame, diagnostic, err := validator.validate(event)
		if err != nil {
			return err
		}
		if emitFrame {
			logTelemetryFrame(logger, jobID, event)
			observer.OnTelemetryEvent(event)
		}
		if diagnostic != nil {
			logTelemetryDiagnostic(logger, jobID, *diagnostic)
			observer.OnTelemetryEvent(TelemetryEvent{
				JobID:          jobID,
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

func logTelemetryFrame(logger *slog.Logger, jobID string, event TelemetryEvent) {
	if logger == nil {
		return
	}
	switch {
	case event.Hello != nil:
		logger.Info("guest telemetry hello received", "job_id", jobID, "boot_id", event.Hello.BootID, "seq", event.Hello.Seq)
	case event.Sample != nil:
		logger.Debug("guest telemetry sample received", "job_id", jobID, "seq", event.Sample.Seq)
	}
}

func logTelemetryDiagnostic(logger *slog.Logger, jobID string, diagnostic TelemetryDiagnostic) {
	if logger == nil {
		return
	}
	logger.Warn(
		"guest telemetry stream diagnostic",
		"job_id", jobID,
		"kind", string(diagnostic.Kind),
		"expected_seq", diagnostic.ExpectedSeq,
		"observed_seq", diagnostic.ObservedSeq,
		"missing_samples", diagnostic.MissingSamples,
	)
}
