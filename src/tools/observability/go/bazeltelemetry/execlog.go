package bazeltelemetry

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"
)

type spawnExec struct {
	Mnemonic      string        `json:"mnemonic"`
	TargetLabel   string        `json:"targetLabel"`
	Runner        string        `json:"runner"`
	CacheHit      bool          `json:"cacheHit"`
	ExitCode      int32         `json:"exitCode"`
	Status        string        `json:"status"`
	ActualOutputs []spawnOutput `json:"actualOutputs"`
	Metrics       *spawnMetrics `json:"metrics"`
}

type spawnOutput struct {
	Path string `json:"path"`
}

type spawnMetrics struct {
	TotalTime string `json:"totalTime"`
	StartTime string `json:"startTime"`
}

func ParseExecutionLog(path string) ([]Spawn, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open execution log: %w", err)
	}
	defer func() { _ = f.Close() }()

	dec := json.NewDecoder(f)
	out := make([]Spawn, 0, 256)
	for {
		var sp spawnExec
		if err := dec.Decode(&sp); err != nil {
			if errors.Is(err, io.EOF) {
				return out, nil
			}
			return nil, fmt.Errorf("decode spawn entry %d: %w", len(out), err)
		}
		converted, err := convertSpawn(sp)
		if err != nil {
			return nil, fmt.Errorf("spawn %q %q: %w", sp.TargetLabel, sp.Mnemonic, err)
		}
		out = append(out, converted)
	}
}

func convertSpawn(sp spawnExec) (Spawn, error) {
	start, err := ParseSpawnStartTime(sp.Metrics)
	if err != nil {
		return Spawn{}, fmt.Errorf("startTime: %w", err)
	}
	durMs, err := ParseSpawnDurationMs(sp.Metrics)
	if err != nil {
		return Spawn{}, fmt.Errorf("totalTime: %w", err)
	}
	parts := ParseLabel(sp.TargetLabel)
	out := Spawn{
		TargetLabel: sp.TargetLabel,
		Package:     parts.Package,
		BuildFile:   parts.BuildFile,
		RuleName:    parts.RuleName,
		Mnemonic:    sp.Mnemonic,
		Runner:      sp.Runner,
		CacheHit:    sp.CacheHit,
		ExitCode:    sp.ExitCode,
		Status:      sp.Status,
		OutputCount: len(sp.ActualOutputs),
		StartedAt:   start,
		DurationMs:  durMs,
	}
	if len(sp.ActualOutputs) > 0 {
		out.OutputFirst = sp.ActualOutputs[0].Path
	}
	return out, nil
}

func ParseSpawnStartTime(m *spawnMetrics) (time.Time, error) {
	if m == nil || m.StartTime == "" {
		return time.Time{}, errors.New("missing")
	}
	return time.Parse(time.RFC3339Nano, m.StartTime)
}

func ParseSpawnDurationMs(m *spawnMetrics) (int64, error) {
	if m == nil || m.TotalTime == "" {
		return 0, nil
	}
	raw := strings.TrimSuffix(m.TotalTime, "s")
	if raw == "" {
		return 0, fmt.Errorf("malformed totalTime %q", m.TotalTime)
	}
	secs, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0, fmt.Errorf("parse %q: %w", m.TotalTime, err)
	}
	return int64(secs * 1000), nil
}
