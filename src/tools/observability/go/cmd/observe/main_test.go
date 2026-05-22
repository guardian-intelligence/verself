package main

import (
	"strings"
	"testing"
	"time"
)

func TestParseSinceRelativeDurations(t *testing.T) {
	now := time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC)
	tests := map[string]time.Time{
		"90m":   now.Add(-90 * time.Minute),
		"1h30m": now.Add(-90 * time.Minute),
		"2h":    now.Add(-2 * time.Hour),
		"7d":    now.Add(-7 * 24 * time.Hour),
		"2w":    now.Add(-14 * 24 * time.Hour),
	}
	for input, want := range tests {
		got, err := parseSince(input, now)
		if err != nil {
			t.Fatalf("parseSince(%q): %v", input, err)
		}
		if !got.Equal(want) {
			t.Fatalf("parseSince(%q) = %s, want %s", input, got, want)
		}
	}
}

func TestParseSinceAbsoluteTimestampsAreUTC(t *testing.T) {
	now := time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC)
	tests := map[string]time.Time{
		"2026-05-22T10:30:00Z": time.Date(2026, 5, 22, 10, 30, 0, 0, time.UTC),
		"2026-05-22 10:30:00":  time.Date(2026, 5, 22, 10, 30, 0, 0, time.UTC),
		"2026-05-22":           time.Date(2026, 5, 22, 0, 0, 0, 0, time.UTC),
	}
	for input, want := range tests {
		got, err := parseSince(input, now)
		if err != nil {
			t.Fatalf("parseSince(%q): %v", input, err)
		}
		if !got.Equal(want) {
			t.Fatalf("parseSince(%q) = %s, want %s", input, got, want)
		}
	}
}

func TestParseUntilUsesSameGrammarAsSince(t *testing.T) {
	now := time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC)
	got, err := parseUntil("1h30m", now)
	if err != nil {
		t.Fatalf("parseUntil relative: %v", err)
	}
	if want := now.Add(-90 * time.Minute); !got.Equal(want) {
		t.Fatalf("parseUntil relative = %s, want %s", got, want)
	}
	got, err = parseUntil("2026-05-22 10:30:00", now)
	if err != nil {
		t.Fatalf("parseUntil absolute: %v", err)
	}
	if want := time.Date(2026, 5, 22, 10, 30, 0, 0, time.UTC); !got.Equal(want) {
		t.Fatalf("parseUntil absolute = %s, want %s", got, want)
	}
}

func TestParseSinceRejectsMalformedRelativeDuration(t *testing.T) {
	now := time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC)
	for _, input := range []string{"0m", "1x", "1h30"} {
		if _, err := parseSince(input, now); err == nil {
			t.Fatalf("parseSince(%q) succeeded, want error", input)
		}
	}
}

func TestParseConfigRejectsSinceWithMinutes(t *testing.T) {
	t.Setenv("SINCE", "")
	t.Setenv("UNTIL", "")
	t.Setenv("MINUTES", "")
	_, err := parseConfig([]string{"--what=logs", "--since=2h", "--minutes=30"})
	if err == nil {
		t.Fatal("parseConfig accepted --since with --minutes")
	}
	if !strings.Contains(err.Error(), "--since and --minutes are mutually exclusive") {
		t.Fatalf("parseConfig error = %q", err)
	}
}

func TestParseConfigAllowsUntilWithMinutes(t *testing.T) {
	now := time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC)
	t.Setenv("SINCE", "")
	t.Setenv("UNTIL", "")
	t.Setenv("MINUTES", "")
	cfg, err := parseConfigAt([]string{"--what=logs", "--until=2h", "--minutes=30"}, now)
	if err != nil {
		t.Fatalf("parseConfigAt: %v", err)
	}
	wantUntil := now.Add(-2 * time.Hour)
	if !cfg.until.Equal(wantUntil) {
		t.Fatalf("until = %s, want %s", cfg.until, wantUntil)
	}
	if wantSince := wantUntil.Add(-30 * time.Minute); !cfg.since.Equal(wantSince) {
		t.Fatalf("since = %s, want %s", cfg.since, wantSince)
	}
}

func TestParseConfigAllowsSinceUntilWindow(t *testing.T) {
	now := time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC)
	t.Setenv("SINCE", "")
	t.Setenv("UNTIL", "")
	t.Setenv("MINUTES", "")
	cfg, err := parseConfigAt([]string{"--what=logs", "--since=4h", "--until=2h"}, now)
	if err != nil {
		t.Fatalf("parseConfigAt: %v", err)
	}
	if want := now.Add(-4 * time.Hour); !cfg.since.Equal(want) {
		t.Fatalf("since = %s, want %s", cfg.since, want)
	}
	if want := now.Add(-2 * time.Hour); !cfg.until.Equal(want) {
		t.Fatalf("until = %s, want %s", cfg.until, want)
	}
}

func TestParseConfigRejectsSinceAfterUntil(t *testing.T) {
	now := time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC)
	t.Setenv("SINCE", "")
	t.Setenv("UNTIL", "")
	t.Setenv("MINUTES", "")
	_, err := parseConfigAt([]string{"--what=logs", "--since=1h", "--until=2h"}, now)
	if err == nil {
		t.Fatal("parseConfigAt accepted --since after --until")
	}
	if !strings.Contains(err.Error(), "--since must be before or equal to --until") {
		t.Fatalf("parseConfigAt error = %q", err)
	}
}

func TestParseConfigRejectsFutureUntil(t *testing.T) {
	now := time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC)
	t.Setenv("SINCE", "")
	t.Setenv("UNTIL", "")
	t.Setenv("MINUTES", "")
	_, err := parseConfigAt([]string{"--what=logs", "--until=2026-05-22T13:00:02Z"}, now)
	if err == nil {
		t.Fatal("parseConfigAt accepted future --until")
	}
	if !strings.Contains(err.Error(), "--until must not be in the future") {
		t.Fatalf("parseConfigAt error = %q", err)
	}
}

func TestBuildQueriesCarriesWindowParameters(t *testing.T) {
	since := time.Date(2026, 5, 22, 10, 30, 0, 123456789, time.UTC)
	until := time.Date(2026, 5, 22, 11, 30, 0, 0, time.UTC)
	queries, err := buildQueries(config{
		what:  "logs",
		since: since,
		until: until,
		limit: 10,
	})
	if err != nil {
		t.Fatalf("buildQueries: %v", err)
	}
	if len(queries) != 1 {
		t.Fatalf("buildQueries returned %d queries, want 1", len(queries))
	}
	if got := queries[0].params["since"]; got != "2026-05-22T10:30:00.123456789Z" {
		t.Fatalf("since param = %q", got)
	}
	if got := queries[0].params["until"]; got != "2026-05-22T11:30:00Z" {
		t.Fatalf("until param = %q", got)
	}
	if !queries[0].windowed {
		t.Fatal("logs query was not marked windowed")
	}
	if !strings.Contains(queries[0].sql, "parseDateTime64BestEffort({since:String}") {
		t.Fatalf("logs SQL does not use the since parameter:\n%s", queries[0].sql)
	}
	if !strings.Contains(queries[0].sql, "parseDateTime64BestEffort({until:String}") {
		t.Fatalf("logs SQL does not use the until parameter:\n%s", queries[0].sql)
	}
}
