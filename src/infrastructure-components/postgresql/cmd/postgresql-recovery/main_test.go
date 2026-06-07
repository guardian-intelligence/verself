package main

import (
	"slices"
	"testing"
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
