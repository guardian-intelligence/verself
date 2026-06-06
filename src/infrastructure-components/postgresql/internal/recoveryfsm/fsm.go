package recoveryfsm

import (
	"errors"
	"fmt"
)

type State string

const (
	StateCheckConfig               State = "check_config"
	StateCheckDataDirectory        State = "check_data_directory"
	StateCheckPostmaster           State = "check_postmaster"
	StateCheckBackupRepository     State = "check_backup_repository"
	StateStartExistingCluster      State = "start_existing_cluster"
	StateRestoreBackup             State = "restore_backup"
	StateInitializeFreshCluster    State = "initialize_fresh_cluster"
	StateWaitReady                 State = "wait_ready"
	StateReconcileCatalog          State = "reconcile_catalog"
	StateVerifyBackupDiscipline    State = "verify_backup_discipline"
	StateEstablishBackupDiscipline State = "establish_backup_discipline"
	StateHealthy                   State = "healthy"
	StateBlocked                   State = "blocked"
)

type ConfigStatus string

const (
	ConfigValid   ConfigStatus = "valid"
	ConfigInvalid ConfigStatus = "invalid"
)

type DataDirectoryStatus string

const (
	DataDirectoryEmpty          DataDirectoryStatus = "empty"
	DataDirectoryValidCluster   DataDirectoryStatus = "valid_cluster"
	DataDirectoryInvalidCluster DataDirectoryStatus = "invalid_cluster"
)

type PostmasterStatus string

const (
	PostmasterStopped  PostmasterStatus = "stopped"
	PostmasterRunning  PostmasterStatus = "running"
	PostmasterStalePID PostmasterStatus = "stale_pid"
)

type BackupRepositoryStatus string

const (
	BackupRepositoryNotConfigured   BackupRepositoryStatus = "not_configured"
	BackupRepositoryUnreachable     BackupRepositoryStatus = "unreachable"
	BackupRepositoryReachableEmpty  BackupRepositoryStatus = "reachable_empty"
	BackupRepositoryReachableBacked BackupRepositoryStatus = "reachable_backed"
)

type Facts struct {
	// Check: validate the CRD and rendered paths before touching cluster data.
	Config ConfigStatus

	// Check: classify PGDATA before any initdb/restore decision to prevent
	// initialization over an existing PostgreSQL cluster.
	DataDirectory DataDirectoryStatus

	// Check: distinguish a running postmaster from a stale pid/socket so
	// recovery can adopt live PostgreSQL or clean up dead process metadata.
	Postmaster PostmasterStatus

	// Check: verify pgBackRest repository configuration and inventory before
	// choosing restore, fresh bootstrap, or backup-health failure.
	BackupRepository BackupRepositoryStatus

	// Check: only an explicit operator/component policy can replace a corrupt
	// data directory. Valid clusters are never overwritten by this FSM.
	DestructiveRestoreAllowed bool
}

type Transition struct {
	From   State  `json:"from"`
	To     State  `json:"to"`
	Check  string `json:"check"`
	Reason string `json:"reason"`
}

type Plan struct {
	Transitions []Transition `json:"transitions"`
	Terminal    State        `json:"terminal"`
}

func (p Plan) Contains(state State) bool {
	for _, transition := range p.Transitions {
		if transition.From == state || transition.To == state {
			return true
		}
	}
	return p.Terminal == state
}

func BuildPlan(facts Facts) (Plan, error) {
	if err := facts.validate(); err != nil {
		return Plan{}, err
	}
	state := StateCheckConfig
	plan := Plan{}
	for i := 0; i < 16; i++ {
		if state == StateHealthy || state == StateBlocked {
			plan.Terminal = state
			return plan, nil
		}
		transition := Advance(state, facts)
		plan.Transitions = append(plan.Transitions, transition)
		state = transition.To
	}
	return Plan{}, errors.New("postgresql recovery FSM did not terminate")
}

func Advance(state State, facts Facts) Transition {
	switch state {
	case StateCheckConfig:
		if facts.Config == ConfigInvalid {
			return blocked(state, "config", "PostgreSQLCluster config is invalid")
		}
		return next(state, StateCheckDataDirectory, "config", "PostgreSQLCluster config is valid")

	case StateCheckDataDirectory:
		switch facts.DataDirectory {
		case DataDirectoryValidCluster:
			return next(state, StateCheckPostmaster, "data_directory", "PGDATA contains a valid PostgreSQL cluster")
		case DataDirectoryEmpty:
			return next(state, StateCheckBackupRepository, "data_directory", "PGDATA is empty")
		case DataDirectoryInvalidCluster:
			return next(state, StateCheckBackupRepository, "data_directory", "PGDATA is present but not a valid PostgreSQL cluster")
		}

	case StateCheckPostmaster:
		switch facts.Postmaster {
		case PostmasterRunning:
			return next(state, StateReconcileCatalog, "postmaster", "existing PostgreSQL postmaster is running")
		case PostmasterStopped:
			return next(state, StateStartExistingCluster, "postmaster", "valid cluster is stopped")
		case PostmasterStalePID:
			return next(state, StateStartExistingCluster, "postmaster", "valid cluster has stale postmaster metadata")
		}

	case StateCheckBackupRepository:
		switch facts.DataDirectory {
		case DataDirectoryEmpty:
			return emptyDataDirectoryTransition(state, facts.BackupRepository)
		case DataDirectoryInvalidCluster:
			return invalidDataDirectoryTransition(state, facts)
		case DataDirectoryValidCluster:
			return next(state, StateCheckPostmaster, "backup_repository", "valid PGDATA does not need restore selection")
		}

	case StateStartExistingCluster:
		return next(state, StateWaitReady, "start", "start existing PostgreSQL cluster")

	case StateRestoreBackup:
		return next(state, StateWaitReady, "restore", "restore base backup and configure WAL replay")

	case StateInitializeFreshCluster:
		return next(state, StateWaitReady, "initdb", "initialize a fresh PostgreSQL cluster")

	case StateWaitReady:
		return next(state, StateReconcileCatalog, "readiness", "PostgreSQL accepts local control-plane connections")

	case StateReconcileCatalog:
		return next(state, StateVerifyBackupDiscipline, "catalog", "roles and databases are reconciled from the CRD")

	case StateVerifyBackupDiscipline:
		switch facts.BackupRepository {
		case BackupRepositoryReachableBacked:
			return next(state, StateHealthy, "backup_repository", "pgBackRest repository has at least one usable backup")
		case BackupRepositoryReachableEmpty:
			return next(state, StateEstablishBackupDiscipline, "backup_repository", "repository is reachable but no backup exists yet")
		case BackupRepositoryUnreachable:
			return blocked(state, "backup_repository", "pgBackRest repository is unreachable")
		case BackupRepositoryNotConfigured:
			return blocked(state, "backup_repository", "pgBackRest repository is not configured")
		}

	case StateEstablishBackupDiscipline:
		return next(state, StateHealthy, "backup_repository", "stanza check and initial full backup completed")
	}
	return Transition{
		From:   state,
		To:     StateBlocked,
		Check:  "fsm",
		Reason: fmt.Sprintf("unknown PostgreSQL recovery state %q", state),
	}
}

func emptyDataDirectoryTransition(state State, repo BackupRepositoryStatus) Transition {
	switch repo {
	case BackupRepositoryReachableBacked:
		return next(state, StateRestoreBackup, "backup_repository", "empty PGDATA will be restored from pgBackRest")
	case BackupRepositoryReachableEmpty:
		return next(state, StateInitializeFreshCluster, "backup_repository", "empty PGDATA and reachable empty repository allow fresh bootstrap")
	case BackupRepositoryUnreachable:
		return blocked(state, "backup_repository", "empty PGDATA cannot recover while pgBackRest is unreachable")
	case BackupRepositoryNotConfigured:
		return blocked(state, "backup_repository", "empty PGDATA cannot become production-healthy without pgBackRest configuration")
	default:
		return blocked(state, "backup_repository", "unknown backup repository state")
	}
}

func invalidDataDirectoryTransition(state State, facts Facts) Transition {
	if facts.BackupRepository != BackupRepositoryReachableBacked {
		return blocked(state, "data_directory", "invalid PGDATA requires a usable backup before replacement")
	}
	if !facts.DestructiveRestoreAllowed {
		return blocked(state, "data_directory", "invalid PGDATA replacement requires explicit destructive recovery permission")
	}
	return next(state, StateRestoreBackup, "data_directory", "invalid PGDATA will be replaced from pgBackRest backup")
}

func next(from, to State, check, reason string) Transition {
	return Transition{From: from, To: to, Check: check, Reason: reason}
}

func blocked(from State, check, reason string) Transition {
	return next(from, StateBlocked, check, reason)
}

func (facts Facts) validate() error {
	if facts.Config != ConfigValid && facts.Config != ConfigInvalid {
		return fmt.Errorf("invalid config status %q", facts.Config)
	}
	switch facts.DataDirectory {
	case DataDirectoryEmpty, DataDirectoryValidCluster, DataDirectoryInvalidCluster:
	default:
		return fmt.Errorf("invalid data directory status %q", facts.DataDirectory)
	}
	switch facts.Postmaster {
	case PostmasterStopped, PostmasterRunning, PostmasterStalePID:
	default:
		return fmt.Errorf("invalid postmaster status %q", facts.Postmaster)
	}
	switch facts.BackupRepository {
	case BackupRepositoryNotConfigured, BackupRepositoryUnreachable, BackupRepositoryReachableEmpty, BackupRepositoryReachableBacked:
	default:
		return fmt.Errorf("invalid backup repository status %q", facts.BackupRepository)
	}
	return nil
}
