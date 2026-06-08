package main

import (
	"encoding/json"
	"slices"
	"testing"
	"unicode/utf8"
)

func TestPgBackRestProcessMaxOnlyForParallelActions(t *testing.T) {
	parallelActions := map[string]bool{
		actionBackup:       true,
		actionCheck:        false,
		actionInfo:         false,
		actionRestore:      true,
		actionStanzaCreate: false,
	}

	for action, want := range parallelActions {
		t.Run(action, func(t *testing.T) {
			cfg := config{
				action:     action,
				configPath: "/run/postgresql/pgbackrest.conf",
				stanza:     "gamma",
				processMax: 2,
			}
			args := commonPgBackRestArgs(cfg, actionUsesProcessMax(action))
			got := slices.Contains(args, "--process-max=2")
			if got != want {
				t.Fatalf("process-max presence = %v, want %v; args=%v", got, want, args)
			}
		})
	}
}

func TestSanitizeUTF8(t *testing.T) {
	// pgBackRest observed emitting a lone 0xc5 byte inside backup metadata,
	// which crashes strict-UTF-8 JSON consumers. The sanitized output must be
	// valid UTF-8 and still parse as JSON.
	raw := "[{\"backup\":[{\"label\":\"20260101-000000F\xc5\"}]}]"
	if utf8.ValidString(raw) {
		t.Fatal("test fixture should contain invalid UTF-8")
	}
	got := sanitizeUTF8(raw)
	if !utf8.ValidString(got) {
		t.Fatalf("sanitized output is not valid UTF-8: %q", got)
	}
	var payload any
	if err := json.Unmarshal([]byte(got), &payload); err != nil {
		t.Fatalf("sanitized output is not valid JSON: %v", err)
	}
}

func TestSanitizeUTF8PreservesValidInput(t *testing.T) {
	valid := "[{\"backup\":[]}]"
	if got := sanitizeUTF8(valid); got != valid {
		t.Fatalf("valid input mutated: got %q want %q", got, valid)
	}
}
