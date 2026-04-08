package vmorchestrator

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"time"
)

func streamGuestTelemetry(ctx context.Context, udsPath, jobID string, observer RunObserver, logger *slog.Logger) error {
	observer = normalizeRunObserver(observer)

	conn, reader, err := connectGuestBridge(ctx, udsPath, guestTelemetryPort)
	if err != nil {
		return err
	}
	defer conn.Close()

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

		if logger != nil {
			switch {
			case event.Hello != nil:
				logger.Info("guest telemetry hello received", "job_id", jobID, "boot_id", event.Hello.BootID)
			case event.Sample != nil:
				logger.Debug("guest telemetry sample received", "job_id", jobID, "seq", event.Sample.Seq)
			}
		}
		observer.OnTelemetryEvent(event)
	}
}
