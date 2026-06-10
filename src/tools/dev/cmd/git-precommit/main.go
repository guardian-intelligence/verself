// Command git-precommit is the repo pre-commit hook. It runs `aspect tidy`
// against the working tree exactly as it stands — no index isolation, since
// stashing around a mutating whole-tree formatter only adds restore-conflict
// risk. Correctness checks (`aspect check`) are CI's job, not the hook's.
package main

import (
	"fmt"
	"os"
	"os/exec"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "pre-commit:", err)
		os.Exit(1)
	}
}

func run() error {
	before, err := worktreeStatus()
	if err != nil {
		return err
	}
	if err := aspect("tidy"); err != nil {
		return err
	}
	after, err := worktreeStatus()
	if err != nil {
		return err
	}
	if before != after {
		return fmt.Errorf("aspect tidy modified the working tree; stage the formatter edits and commit again")
	}
	return nil
}

func aspect(task string) error {
	cmd := exec.Command("aspect", task)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("aspect %s: %w", task, err)
	}
	return nil
}

func worktreeStatus() (string, error) {
	out, err := exec.Command("git", "status", "--porcelain").Output()
	if err != nil {
		return "", fmt.Errorf("git status: %w", err)
	}
	return string(out), nil
}
