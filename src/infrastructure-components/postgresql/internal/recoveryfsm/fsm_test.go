package recoveryfsm

import (
	"testing"

	"pgregory.net/rapid"
)

func TestEmptyDataDirectoryUsesBackupBeforeFreshBootstrap(t *testing.T) {
	plan, err := BuildPlan(Facts{
		Config:           ConfigValid,
		DataDirectory:    DataDirectoryEmpty,
		Postmaster:       PostmasterStopped,
		BackupRepository: BackupRepositoryReachableBacked,
	})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Terminal != StateHealthy {
		t.Fatalf("terminal = %s, want %s", plan.Terminal, StateHealthy)
	}
	if !plan.Contains(StateRestoreBackup) {
		t.Fatalf("expected restore plan, got %#v", plan.Transitions)
	}
	if plan.Contains(StateInitializeFreshCluster) {
		t.Fatalf("empty data with backups must not init fresh: %#v", plan.Transitions)
	}
}

func TestInvalidDataDirectoryRequiresExplicitDestructiveRestore(t *testing.T) {
	plan, err := BuildPlan(Facts{
		Config:           ConfigValid,
		DataDirectory:    DataDirectoryInvalidCluster,
		Postmaster:       PostmasterStopped,
		BackupRepository: BackupRepositoryReachableBacked,
	})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Terminal != StateBlocked {
		t.Fatalf("terminal = %s, want %s", plan.Terminal, StateBlocked)
	}
	if plan.Contains(StateRestoreBackup) {
		t.Fatalf("invalid data without destructive permission must not restore: %#v", plan.Transitions)
	}
}

func TestReachableEmptyRepositoryFreshBootstrapEstablishesBackup(t *testing.T) {
	plan, err := BuildPlan(Facts{
		Config:           ConfigValid,
		DataDirectory:    DataDirectoryEmpty,
		Postmaster:       PostmasterStopped,
		BackupRepository: BackupRepositoryReachableEmpty,
	})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Terminal != StateHealthy {
		t.Fatalf("terminal = %s, want %s", plan.Terminal, StateHealthy)
	}
	if !plan.Contains(StateInitializeFreshCluster) || !plan.Contains(StateEstablishBackupDiscipline) {
		t.Fatalf("fresh bootstrap must init and establish backup discipline: %#v", plan.Transitions)
	}
}

func TestRecoveryPlansTerminateAndPreserveSafety(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		facts := Facts{
			Config:                    rapid.SampledFrom([]ConfigStatus{ConfigValid, ConfigInvalid}).Draw(t, "config"),
			DataDirectory:             rapid.SampledFrom([]DataDirectoryStatus{DataDirectoryEmpty, DataDirectoryValidCluster, DataDirectoryInvalidCluster}).Draw(t, "data_directory"),
			Postmaster:                rapid.SampledFrom([]PostmasterStatus{PostmasterStopped, PostmasterRunning, PostmasterStalePID}).Draw(t, "postmaster"),
			BackupRepository:          rapid.SampledFrom([]BackupRepositoryStatus{BackupRepositoryNotConfigured, BackupRepositoryUnreachable, BackupRepositoryReachableEmpty, BackupRepositoryReachableBacked}).Draw(t, "backup_repository"),
			DestructiveRestoreAllowed: rapid.Bool().Draw(t, "destructive_restore_allowed"),
		}

		plan, err := BuildPlan(facts)
		if err != nil {
			t.Fatalf("BuildPlan(%#v): %v", facts, err)
		}
		if plan.Terminal != StateHealthy && plan.Terminal != StateBlocked {
			t.Fatalf("non-terminal plan for %#v: %#v", facts, plan)
		}
		if len(plan.Transitions) == 0 || len(plan.Transitions) > 16 {
			t.Fatalf("unexpected transition count for %#v: %#v", facts, plan)
		}

		if facts.DataDirectory == DataDirectoryValidCluster && plan.Contains(StateRestoreBackup) {
			t.Fatalf("valid PGDATA must never be overwritten by restore: %#v", plan.Transitions)
		}
		if facts.DataDirectory != DataDirectoryEmpty && plan.Contains(StateInitializeFreshCluster) {
			t.Fatalf("initdb must only run for empty PGDATA: %#v", plan.Transitions)
		}
		if facts.DataDirectory == DataDirectoryInvalidCluster && !facts.DestructiveRestoreAllowed && plan.Contains(StateRestoreBackup) {
			t.Fatalf("invalid PGDATA restore requires explicit destructive permission: %#v", plan.Transitions)
		}
		if facts.DataDirectory == DataDirectoryEmpty && facts.BackupRepository == BackupRepositoryUnreachable && plan.Contains(StateInitializeFreshCluster) {
			t.Fatalf("empty PGDATA must not bootstrap fresh while repository reachability is unknown: %#v", plan.Transitions)
		}
		if plan.Terminal == StateHealthy && !plan.Contains(StateReconcileCatalog) {
			t.Fatalf("healthy PostgreSQL must reconcile roles/databases: %#v", plan.Transitions)
		}
		if plan.Terminal == StateHealthy && facts.BackupRepository == BackupRepositoryReachableEmpty && !plan.Contains(StateEstablishBackupDiscipline) {
			t.Fatalf("healthy fresh PostgreSQL must establish the first backup: %#v", plan.Transitions)
		}
	})
}
