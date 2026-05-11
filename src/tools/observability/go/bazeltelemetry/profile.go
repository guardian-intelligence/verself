package bazeltelemetry

import (
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

var keptProfileCategories = map[string]string{
	"critical path component":        "critical_path",
	"skyframe evaluator":             "skyframe",
	"Fetching repository":            "repo_fetch",
	"package creation":               "package",
	"Starlark user function call":    "starlark_user",
	"Starlark builtin function call": "starlark_builtin",
	"Starlark thread context":        "starlark_thread",
	"Conflict checking":              "conflict_check",
}

type profile struct {
	OtherData struct {
		ProfileStartTs int64 `json:"profile_start_ts"`
	} `json:"otherData"`
	TraceEvents []profileEvent `json:"traceEvents"`
}

type profileEvent struct {
	Cat  string         `json:"cat"`
	Name string         `json:"name"`
	Ph   string         `json:"ph"`
	Ts   int64          `json:"ts"`
	Dur  int64          `json:"dur"`
	Args map[string]any `json:"args"`
}

func ParseProfile(path string, minDurationMs int64) ([]ProfileSpan, error) {
	prof, err := readProfile(path)
	if err != nil {
		return nil, err
	}
	if prof.OtherData.ProfileStartTs == 0 {
		return nil, fmt.Errorf("profile has no otherData.profile_start_ts")
	}
	base := time.UnixMilli(prof.OtherData.ProfileStartTs)
	minDurationMicros := minDurationMs * 1000
	out := make([]ProfileSpan, 0, len(prof.TraceEvents)/8)
	for _, e := range prof.TraceEvents {
		if e.Ph != "X" {
			continue
		}
		spanKind, ok := keptProfileCategories[e.Cat]
		if !ok {
			continue
		}
		// Package creation is the exact BUILD.bazel identifier surface; keep
		// sub-millisecond packages even when the generic phase floor is higher.
		if e.Cat != "package creation" && e.Dur < minDurationMicros {
			continue
		}
		start := base.Add(time.Duration(e.Ts) * time.Microsecond)
		span := ProfileSpan{
			SpanKind:   spanKind,
			Category:   e.Cat,
			EventName:  e.Name,
			StartedAt:  start,
			DurationMs: e.Dur / 1000,
		}
		if e.Cat == "package creation" {
			parts := PackageParts(e.Name)
			span.Package = parts.Package
			span.ExternalRepo = parts.Repository
			span.BuildFile = parts.BuildFile
		}
		out = append(out, span)
	}
	return out, nil
}

func readProfile(path string) (*profile, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open profile: %w", err)
	}
	defer func() { _ = f.Close() }()

	var r io.Reader = f
	if strings.HasSuffix(path, ".gz") {
		gz, err := gzip.NewReader(f)
		if err != nil {
			return nil, fmt.Errorf("gunzip profile: %w", err)
		}
		defer func() { _ = gz.Close() }()
		r = gz
	}
	var p profile
	if err := json.NewDecoder(r).Decode(&p); err != nil {
		return nil, fmt.Errorf("decode profile: %w", err)
	}
	return &p, nil
}
