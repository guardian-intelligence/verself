package jobs

import (
	"strings"
	"time"
)

const verselfPhaseMarkerPrefix = "::verself-phase "

func sandboxPhaseRowsForRunnerLog(record ExecutionRecord, logs string) []sandboxPhaseRow {
	if strings.TrimSpace(logs) == "" {
		return nil
	}
	var (
		listeningAt    time.Time
		runningAt      time.Time
		jobCompletedAt time.Time
		markers        = map[string]time.Time{}
	)
	for _, raw := range strings.Split(logs, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		if name, ts, ok := parseVerselfPhaseMarker(line); ok {
			markers[name] = ts
			continue
		}
		if ts, rest, ok := parseGitHubRunnerTimestamp(line); ok {
			switch {
			case strings.HasPrefix(rest, "Listening for Jobs"):
				listeningAt = ts
			case strings.HasPrefix(rest, "Running job:"):
				runningAt = ts
			case strings.HasPrefix(rest, "Job ") && strings.Contains(rest, " completed with result:"):
				jobCompletedAt = ts
			}
		}
	}
	rows := make([]sandboxPhaseRow, 0, 8)
	add := func(name string, start, end time.Time, attrs sandboxPhaseAttrs) {
		if start.IsZero() || end.IsZero() || end.Before(start) {
			return
		}
		rows = append(rows, sandboxPhaseRowFromRun(record, "guest-runner-log", name, "succeeded", "", start, end, attrs))
	}
	execStarted := derefTime(record.LatestAttempt.StartedAt)
	execCompleted := derefTime(record.LatestAttempt.CompletedAt)
	bootstrapStarted := markers["runner.bootstrap.start"]
	jitFetched := markers["runner.bootstrap.jit_fetched"]
	runtimeReady := markers["runner.bootstrap.runtime_ready"]
	processStarted := markers["github.runner.process_start"]

	add("runner.bootstrap.fetch_jit", bootstrapStarted, jitFetched, nil)
	add("runner.bootstrap.prepare_runtime", jitFetched, runtimeReady, nil)
	add("runner.bootstrap.launch_runner", runtimeReady, processStarted, nil)
	add("runner.bootstrap.total", bootstrapStarted, processStarted, nil)
	add("github.runner.start_to_listen", firstNonZeroTime(processStarted, execStarted), listeningAt, nil)
	add("github.runner.assignment_wait", listeningAt, runningAt, nil)
	add("github.runner.job", runningAt, jobCompletedAt, sandboxPhaseAttrs{"github.job_name": record.Runner.JobName})
	add("github.runner.post_job_exit", jobCompletedAt, execCompleted, nil)
	return rows
}

func parseVerselfPhaseMarker(line string) (string, time.Time, bool) {
	if !strings.HasPrefix(line, verselfPhaseMarkerPrefix) {
		return "", time.Time{}, false
	}
	fields := strings.Fields(strings.TrimPrefix(line, verselfPhaseMarkerPrefix))
	var name, tsText string
	for _, field := range fields {
		key, value, ok := strings.Cut(field, "=")
		if !ok {
			continue
		}
		switch key {
		case "name":
			name = value
		case "ts":
			tsText = value
		}
	}
	if name == "" || tsText == "" {
		return "", time.Time{}, false
	}
	ts, err := time.Parse(time.RFC3339Nano, tsText)
	if err != nil {
		return "", time.Time{}, false
	}
	return name, ts.UTC(), true
}

func parseGitHubRunnerTimestamp(line string) (time.Time, string, bool) {
	if len(line) < len("2006-01-02 15:04:05Z: ") {
		return time.Time{}, "", false
	}
	if line[19] != 'Z' || line[20] != ':' || line[21] != ' ' {
		return time.Time{}, "", false
	}
	ts, err := time.Parse("2006-01-02 15:04:05Z", line[:20])
	if err != nil {
		return time.Time{}, "", false
	}
	return ts.UTC(), line[22:], true
}

func firstNonZeroTime(values ...time.Time) time.Time {
	for _, value := range values {
		if !value.IsZero() {
			return value
		}
	}
	return time.Time{}
}
