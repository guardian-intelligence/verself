package bazeltelemetry

import (
	"os"
	"time"
)

type ArtifactPaths struct {
	Profile      string
	BEP          string
	ExecutionLog string
}

func Collect(inv Invocation, paths ArtifactPaths, minProfileDurationMs int64) Upload {
	now := time.Now().UTC()
	upload := Upload{
		SchemaVersion: SchemaVersion,
		Invocation:    inv,
		Events: []Event{
			{ObservedAt: inv.StartedAt, EventName: "bazel.invocation.started", Result: "accepted"},
		},
	}

	if stat, err := os.Stat(paths.Profile); err == nil {
		upload.Invocation.ProfileBytes = stat.Size()
	}
	if stat, err := os.Stat(paths.BEP); err == nil {
		upload.Invocation.BEPBytes = stat.Size()
	}
	if stat, err := os.Stat(paths.ExecutionLog); err == nil {
		upload.Invocation.ExecutionLogBytes = stat.Size()
	}

	start := time.Now()
	if spans, err := ParseProfile(paths.Profile, minProfileDurationMs); err != nil {
		upload.Events = append(upload.Events, Event{ObservedAt: time.Now().UTC(), EventName: "bazel.profile.parse", Result: "failed", Reason: err.Error(), DurationMs: elapsedMs(start)})
	} else {
		upload.ProfileSpans = spans
		upload.Events = append(upload.Events, Event{ObservedAt: time.Now().UTC(), EventName: "bazel.profile.parse", Result: "succeeded", DurationMs: elapsedMs(start)})
	}

	start = time.Now()
	if targets, err := ParseBEP(paths.BEP); err != nil {
		upload.Events = append(upload.Events, Event{ObservedAt: time.Now().UTC(), EventName: "bazel.bep.parse", Result: "failed", Reason: err.Error(), DurationMs: elapsedMs(start)})
	} else {
		upload.Targets = targets
		upload.Events = append(upload.Events, Event{ObservedAt: time.Now().UTC(), EventName: "bazel.bep.parse", Result: "succeeded", DurationMs: elapsedMs(start)})
	}

	start = time.Now()
	if spawns, err := ParseExecutionLog(paths.ExecutionLog); err != nil {
		upload.Events = append(upload.Events, Event{ObservedAt: time.Now().UTC(), EventName: "bazel.execlog.parse", Result: "failed", Reason: err.Error(), DurationMs: elapsedMs(start)})
	} else {
		upload.Spawns = spawns
		upload.Events = append(upload.Events, Event{ObservedAt: time.Now().UTC(), EventName: "bazel.execlog.parse", Result: "succeeded", DurationMs: elapsedMs(start)})
	}

	result := "succeeded"
	if inv.ExitCode != 0 {
		result = "failed"
	}
	upload.Events = append(upload.Events, Event{
		ObservedAt: now,
		EventName:  "bazel.invocation.finished",
		Result:     result,
		DurationMs: inv.DurationMs,
	})
	return upload
}

func elapsedMs(start time.Time) int64 {
	return time.Since(start).Milliseconds()
}
