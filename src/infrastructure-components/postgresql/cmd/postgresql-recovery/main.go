package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/verself/infrastructure-components/postgresql/internal/recoveryfsm"
)

const (
	actionBackup       = "backup"
	actionCheck        = "check"
	actionInfo         = "info"
	actionPlan         = "plan"
	actionRestore      = "restore"
	actionStanzaCreate = "stanza-create"

	defaultConfigPath  = "/etc/pgbackrest/pgbackrest.conf"
	defaultRuntimeRoot = "/var/lib/postgresql/runtime/current"
)

type config struct {
	action                    string
	stanza                    string
	configPath                string
	runtimeRoot               string
	pgData                    string
	processMax                int
	startFast                 bool
	repo                      int
	backupType                string
	restoreType               string
	restoreTarget             string
	targetAction              string
	destructiveRestoreAllowed bool
	planFacts                 recoveryfsm.Facts
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "postgresql-recovery: "+err.Error())
		os.Exit(1)
	}
}

func run(args []string) error {
	cfg, err := parseConfig(args)
	if err != nil {
		return err
	}
	switch cfg.action {
	case actionPlan:
		return printPlan(cfg.planFacts)
	case actionInfo:
		return runPgBackRestInfo(cfg)
	case actionCheck:
		return runPgBackRest(cfg, []string{"check"})
	case actionStanzaCreate:
		return runPgBackRest(cfg, []string{"stanza-create"})
	case actionBackup:
		return runPgBackRest(cfg, backupArgs(cfg))
	case actionRestore:
		if err := checkRestoreAllowed(cfg); err != nil {
			return err
		}
		return runPgBackRest(cfg, restoreArgs(cfg))
	default:
		return fmt.Errorf("unsupported --action %q", cfg.action)
	}
}

func parseConfig(args []string) (config, error) {
	cfg := config{
		configPath:   defaultConfigPath,
		runtimeRoot:  defaultRuntimeRoot,
		processMax:   2,
		backupType:   "incr",
		restoreType:  "default",
		targetAction: "promote",
		planFacts: recoveryfsm.Facts{
			Config:           recoveryfsm.ConfigValid,
			DataDirectory:    recoveryfsm.DataDirectoryEmpty,
			Postmaster:       recoveryfsm.PostmasterStopped,
			BackupRepository: recoveryfsm.BackupRepositoryReachableEmpty,
		},
	}
	fs := flag.NewFlagSet("postgresql-recovery", flag.ContinueOnError)
	fs.StringVar(&cfg.action, "action", "", "Action: plan, info, stanza-create, check, backup, or restore.")
	fs.StringVar(&cfg.stanza, "stanza", "", "pgBackRest stanza.")
	fs.StringVar(&cfg.configPath, "config", cfg.configPath, "pgBackRest config path.")
	fs.StringVar(&cfg.runtimeRoot, "runtime-root", cfg.runtimeRoot, "PostgreSQL runtime root containing usr/bin/pgbackrest.")
	fs.StringVar(&cfg.pgData, "pgdata", "", "PostgreSQL data directory.")
	fs.IntVar(&cfg.processMax, "process-max", cfg.processMax, "pgBackRest process-max.")
	fs.BoolVar(&cfg.startFast, "start-fast", true, "Pass --start-fast to pgBackRest backup.")
	fs.IntVar(&cfg.repo, "repo", cfg.repo, "Optional pgBackRest repository number.")
	fs.StringVar(&cfg.backupType, "backup-type", cfg.backupType, "Backup type: full, diff, or incr.")
	fs.StringVar(&cfg.restoreType, "restore-type", cfg.restoreType, "Restore type: default, immediate, time, lsn, name, xid, standby, preserve, none.")
	fs.StringVar(&cfg.restoreTarget, "restore-target", cfg.restoreTarget, "Restore target for time, lsn, name, or xid restore.")
	fs.StringVar(&cfg.targetAction, "target-action", cfg.targetAction, "Restore target action: promote, pause, or shutdown.")
	fs.BoolVar(&cfg.destructiveRestoreAllowed, "destructive-restore-allowed", false, "Allow restore to replace non-empty PGDATA.")

	var planConfig, planData, planPostmaster, planBackup string
	fs.StringVar(&planConfig, "plan-config", string(cfg.planFacts.Config), "FSM config fact: valid or invalid.")
	fs.StringVar(&planData, "plan-data-directory", string(cfg.planFacts.DataDirectory), "FSM data-directory fact: empty, valid_cluster, or invalid_cluster.")
	fs.StringVar(&planPostmaster, "plan-postmaster", string(cfg.planFacts.Postmaster), "FSM postmaster fact: stopped, running, or stale_pid.")
	fs.StringVar(&planBackup, "plan-backup-repository", string(cfg.planFacts.BackupRepository), "FSM backup-repository fact: not_configured, unreachable, reachable_empty, or reachable_backed.")
	fs.BoolVar(&cfg.planFacts.DestructiveRestoreAllowed, "plan-destructive-restore-allowed", false, "FSM destructive restore permission fact.")
	if err := fs.Parse(args); err != nil {
		return config{}, err
	}
	if fs.NArg() != 0 {
		return config{}, fmt.Errorf("unexpected positional args: %s", strings.Join(fs.Args(), " "))
	}
	cfg.planFacts.Config = recoveryfsm.ConfigStatus(planConfig)
	cfg.planFacts.DataDirectory = recoveryfsm.DataDirectoryStatus(planData)
	cfg.planFacts.Postmaster = recoveryfsm.PostmasterStatus(planPostmaster)
	cfg.planFacts.BackupRepository = recoveryfsm.BackupRepositoryStatus(planBackup)
	if err := cfg.validate(); err != nil {
		return config{}, err
	}
	return cfg, nil
}

func (cfg config) validate() error {
	if strings.TrimSpace(cfg.action) == "" {
		return errors.New("--action is required")
	}
	if cfg.action == actionPlan {
		_, err := recoveryfsm.BuildPlan(cfg.planFacts)
		return err
	}
	if strings.TrimSpace(cfg.stanza) == "" {
		return errors.New("--stanza is required")
	}
	if strings.TrimSpace(cfg.configPath) == "" || !filepath.IsAbs(cfg.configPath) {
		return errors.New("--config must be an absolute path")
	}
	if strings.TrimSpace(cfg.runtimeRoot) == "" || !filepath.IsAbs(cfg.runtimeRoot) {
		return errors.New("--runtime-root must be an absolute path")
	}
	if cfg.processMax <= 0 {
		return errors.New("--process-max must be positive")
	}
	if cfg.repo < 0 {
		return errors.New("--repo must be non-negative")
	}
	if cfg.action == actionBackup {
		switch cfg.backupType {
		case "full", "diff", "incr":
		default:
			return errors.New("--backup-type must be full, diff, or incr")
		}
	}
	if cfg.action == actionRestore {
		if strings.TrimSpace(cfg.pgData) == "" || !filepath.IsAbs(cfg.pgData) {
			return errors.New("--pgdata must be an absolute path for restore")
		}
		switch cfg.restoreType {
		case "default", "immediate", "time", "lsn", "name", "xid", "standby", "preserve", "none":
		default:
			return errors.New("--restore-type is invalid")
		}
		switch cfg.restoreType {
		case "time", "lsn", "name", "xid":
			if strings.TrimSpace(cfg.restoreTarget) == "" {
				return fmt.Errorf("--restore-target is required for --restore-type=%s", cfg.restoreType)
			}
		}
		switch cfg.targetAction {
		case "promote", "pause", "shutdown":
		default:
			return errors.New("--target-action must be promote, pause, or shutdown")
		}
	}
	return nil
}

func printPlan(facts recoveryfsm.Facts) error {
	plan, err := recoveryfsm.BuildPlan(facts)
	if err != nil {
		return err
	}
	return printJSON(plan)
}

func runPgBackRest(cfg config, commandArgs []string) error {
	binary := filepath.Join(cfg.runtimeRoot, "usr/bin/pgbackrest")
	if err := requireExecutable(binary); err != nil {
		return err
	}
	args := commonPgBackRestArgs(cfg, actionUsesProcessMax(cfg.action))
	args = append(args, commandArgs...)
	cmd := exec.Command(binary, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = commandEnv(cfg)
	return cmd.Run()
}

// runPgBackRestInfo captures pgBackRest's JSON stdout and replaces invalid
// UTF-8 bytes before forwarding. pgBackRest can emit a non-UTF-8 byte inside
// backup metadata (observed 0xc5), which crashes strict-UTF-8 JSON consumers.
// Scoped to info so other actions keep raw passthrough.
func runPgBackRestInfo(cfg config) error {
	binary := filepath.Join(cfg.runtimeRoot, "usr/bin/pgbackrest")
	if err := requireExecutable(binary); err != nil {
		return err
	}
	args := commonPgBackRestArgs(cfg, actionUsesProcessMax(cfg.action))
	args = append(args, "info", "--output=json")
	cmd := exec.Command(binary, args...)
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = os.Stderr
	cmd.Env = commandEnv(cfg)
	runErr := cmd.Run()
	if _, err := io.WriteString(os.Stdout, sanitizeUTF8(stdout.String())); err != nil {
		return err
	}
	return runErr
}

func sanitizeUTF8(s string) string {
	return strings.ToValidUTF8(s, "�")
}

func commonPgBackRestArgs(cfg config, includeProcessMax bool) []string {
	args := []string{
		"--config=" + cfg.configPath,
		"--stanza=" + cfg.stanza,
	}
	if includeProcessMax {
		args = append(args, "--process-max="+strconv.Itoa(cfg.processMax))
	}
	if cfg.repo > 0 {
		args = append(args, "--repo="+strconv.Itoa(cfg.repo))
	}
	return args
}

func actionUsesProcessMax(action string) bool {
	switch action {
	case actionBackup, actionRestore:
		return true
	default:
		return false
	}
}

func backupArgs(cfg config) []string {
	args := []string{"backup", "--type=" + cfg.backupType}
	if cfg.startFast {
		args = append(args, "--start-fast")
	}
	return args
}

func restoreArgs(cfg config) []string {
	args := []string{
		"restore",
		"--pg1-path=" + cfg.pgData,
		"--type=" + cfg.restoreType,
		"--target-action=" + cfg.targetAction,
	}
	if cfg.restoreTarget != "" {
		args = append(args, "--target="+cfg.restoreTarget)
	}
	return args
}

func checkRestoreAllowed(cfg config) error {
	// Check: pgBackRest restore is allowed against empty PGDATA because no
	// PostgreSQL cluster can be overwritten.
	empty, err := dataDirectoryEmpty(cfg.pgData)
	if err != nil {
		return err
	}
	if empty {
		return nil
	}
	// Check: replacing non-empty PGDATA requires an explicit flag so corrupt
	// cluster recovery cannot silently destroy evidence or valid data.
	if !cfg.destructiveRestoreAllowed {
		return errors.New("restore over non-empty PGDATA requires --destructive-restore-allowed")
	}
	return nil
}

func dataDirectoryEmpty(path string) (bool, error) {
	entries, err := os.ReadDir(path)
	if os.IsNotExist(err) {
		return true, nil
	}
	if err != nil {
		return false, fmt.Errorf("read PGDATA: %w", err)
	}
	return len(entries) == 0, nil
}

func requireExecutable(path string) error {
	stat, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("stat executable %s: %w", path, err)
	}
	if stat.IsDir() || stat.Mode().Perm()&0o111 == 0 {
		return fmt.Errorf("%s is not executable", path)
	}
	return nil
}

func commandEnv(cfg config) []string {
	libDir := filepath.Join(cfg.runtimeRoot, "usr/lib/x86_64-linux-gnu")
	pgLibDir := filepath.Join(cfg.runtimeRoot, "usr/lib/postgresql/16/lib")
	env := os.Environ()
	env = append(env, "LD_LIBRARY_PATH="+libDir+":"+pgLibDir)
	return env
}

func printJSON(value any) error {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}
