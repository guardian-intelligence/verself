package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/verself/deployment-tools/internal/ansible"
	"github.com/verself/deployment-tools/internal/deploydb"
	"github.com/verself/deployment-tools/internal/identity"
	"github.com/verself/deployment-tools/internal/runtime"
)

const (
	securityPatchPlaybook  = "playbooks/security-patch.yml"
	securityPatchPhase     = "security_patch"
	securityPatchComponent = "security-patch"
)

var securityPatchSSHPorts = []int{2222, 22}

type securityPatchResult struct {
	StartedAt time.Time
	EndedAt   time.Time
	Result    *ansible.Result
	Err       error
}

func runSecurityPatchPreflight(ctx context.Context, site, repoRoot, runKey string) securityPatchResult {
	startedAt := time.Now()
	var last securityPatchResult
	for idx, port := range securityPatchSSHPorts {
		res, err := runSecurityPatchPlaybook(ctx, site, repoRoot, runKey, port)
		last = securityPatchResult{
			StartedAt: startedAt,
			EndedAt:   time.Now(),
			Result:    res,
			Err:       err,
		}
		if securityPatchOK(last) {
			return last
		}
		if idx == len(securityPatchSSHPorts)-1 || !securityPatchUnreachable(last) {
			return last
		}
	}
	return last
}

func runSecurityPatchPlaybook(ctx context.Context, site, repoRoot, runKey string, sshPort int) (*ansible.Result, error) {
	inventoryPath := authoredInventoryPath(repoRoot, site)
	if _, err := os.Stat(inventoryPath); err != nil {
		return nil, fmt.Errorf("inventory missing at %s: %w", inventoryPath, err)
	}
	ansibleDir := filepath.Join(repoRoot, "src", "host", "ansible")
	// Port 2222 exists after operator_ssh_access runs; fresh cloud-init hosts
	// only have public 22 until the bootstrap role creates the recovery socket.
	return ansible.Run(ctx, nil, ansible.Options{
		Playbook:      securityPatchPlaybook,
		Inventory:     inventoryPath,
		AnsibleDir:    ansibleDir,
		ExtraArgs:     []string{"-e", "verself_site=" + site, "-e", fmt.Sprintf("ansible_port=%d", sshPort)},
		Site:          site,
		Phase:         securityPatchPhase,
		RunKey:        runKey,
		AdditionalEnv: []string{"VERSELF_SITE=" + site},
	})
}

func securityPatchUnreachable(res securityPatchResult) bool {
	if res.Err != nil {
		return false
	}
	if res.Result == nil {
		return false
	}
	for _, stats := range res.Result.Recap.Hosts {
		if stats.Unreachable > 0 {
			return true
		}
	}
	return false
}

func securityPatchOK(res securityPatchResult) bool {
	return res.Err == nil && res.Result != nil && res.Result.ExitCode == 0
}

func securityPatchFailureMessage(res securityPatchResult) string {
	return ansibleFailureMessage(securityPatchPlaybook, res.Result, res.Err)
}

func recordSecurityPatchSummary(ctx context.Context, db *deploydb.Client, site, runKey string, res securityPatchResult, status, message string) error {
	duration := res.EndedAt.Sub(res.StartedAt)
	if res.EndedAt.IsZero() || res.StartedAt.IsZero() || duration < 0 {
		duration = 0
	}
	if message == "" {
		message = securityPatchSummaryMessage(res)
	}
	row := deploydb.AnsibleTaskEventRow{
		EventAt:      time.Now().UTC(),
		DeployRunKey: runKey,
		Site:         site,
		Layer:        securityPatchPhase,
		Playbook:     securityPatchPlaybook,
		Play:         "Apply security patches",
		Task:         "Security patch preflight",
		Host:         "all",
		Status:       status,
		DurationMS:   durationMillis(duration, "security patch duration"),
		Message:      message,
	}
	return db.InsertAnsibleTaskEvents(ctx, []deploydb.AnsibleTaskEventRow{row})
}

type securityPostPreflightCheck struct {
	Name      string
	Skippable bool
	Run       func(context.Context, *runtime.Runtime) (securityPostPreflightCheckResult, error)
}

type securityPostPreflightCheckResult struct {
	Fingerprint string
	Message     string
}

func runSecurityPostPreflight(ctx context.Context, rt *runtime.Runtime, site string, patch securityPatchResult) error {
	if err := recordSecurityPatchSummary(ctx, rt.DeployDB, site, rt.Identity.RunKey(), patch, "ok", ""); err != nil {
		return err
	}
	for _, check := range securityPostPreflightChecks() {
		result, err := check.Run(ctx, rt)
		status := "ok"
		message := result.Message
		if err != nil {
			status = "failed"
			message = err.Error()
		}
		row := deploydb.AnsibleTaskEventRow{
			EventAt:      time.Now().UTC(),
			DeployRunKey: rt.Identity.RunKey(),
			Site:         site,
			Layer:        securityPatchPhase,
			Playbook:     securityPatchPlaybook,
			Play:         "Security post-preflight",
			Task:         check.Name,
			Host:         "all",
			Status:       status,
			Item:         result.Fingerprint,
			Message:      fmt.Sprintf("skippable=%t %s", check.Skippable, message),
		}
		insertErr := rt.DeployDB.InsertAnsibleTaskEvents(ctx, []deploydb.AnsibleTaskEventRow{row})
		if err != nil || insertErr != nil {
			return errors.Join(err, insertErr)
		}
	}
	return nil
}

func securityPostPreflightChecks() []securityPostPreflightCheck {
	return []securityPostPreflightCheck{
		{
			Name:      "CVE-2026-31431 Copy Fail hardening",
			Skippable: true,
			Run:       checkCopyFailHardening,
		},
	}
}

func checkCopyFailHardening(ctx context.Context, rt *runtime.Runtime) (securityPostPreflightCheckResult, error) {
	// CVE-2026-31431 hardening requires algif_aead to stay unloaded and unautoloadable after patching.
	out, err := rt.SSH.Exec(ctx, strings.Join([]string{
		`set -eu`,
		`kernel="$(uname -r)"`,
		`printf 'kernel=%s\n' "$kernel"`,
		`dpkg-query -W -f='${Package}=${Version}\n' kmod linux-image-generic "linux-image-$kernel"`,
		`printf 'algif_aead_loaded='`,
		`if grep -q '^algif_aead ' /proc/modules; then printf yes; else printf no; fi`,
		`printf '\nalgif_aead_modprobe='`,
		`/usr/sbin/modprobe -n -v algif_aead | tr '\n' '|'`,
		`printf '\n'`,
	}, "; "))
	result := securityPostPreflightCheckResult{
		Fingerprint: fingerprintBytes(out),
		Message:     strings.TrimSpace(string(out)),
	}
	if err != nil {
		return result, err
	}
	text := string(out)
	if !strings.Contains(text, "algif_aead_loaded=no\n") {
		return result, fmt.Errorf("CVE-2026-31431 hardening failed: algif_aead is loaded")
	}
	if !strings.Contains(text, "/bin/false") {
		return result, fmt.Errorf("CVE-2026-31431 hardening failed: algif_aead autoload is not blocked")
	}
	return result, nil
}

func fingerprintBytes(b []byte) string {
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func securityPatchSummaryMessage(res securityPatchResult) string {
	if res.Result == nil {
		if res.Err == nil {
			return "security patch preflight did not return an Ansible result"
		}
		return res.Err.Error()
	}
	return fmt.Sprintf(
		"exit_code=%d changed=%d failed=%d tasks=%d",
		res.Result.ExitCode,
		res.Result.ChangedCount,
		res.Result.FailedCount,
		res.Result.TaskCount,
	)
}

func recordSecurityPatchFailureBestEffort(parentCtx context.Context, site, repoRoot, sha, scope string, snap identity.Snapshot, res securityPatchResult) {
	rt, err := runtime.Init(parentCtx, runtime.Options{
		ServiceName:    serviceName,
		ServiceVersion: serviceVersion,
		Site:           site,
		RepoRoot:       repoRoot,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "verself-deploy: WARN: security patch failure evidence runtime init failed: %v\n", err)
		return
	}
	defer rt.Close()

	ctx := rt.Ctx
	msg := securityPatchFailureMessage(res)
	if err := recordSecurityPatchSummary(ctx, rt.DeployDB, site, snap.RunKey(), res, "failed", msg); err != nil {
		fmt.Fprintf(os.Stderr, "verself-deploy: WARN: security patch failure summary insert failed: %v\n", err)
	}
	writeFailedDeployEvent(ctx, rt.DeployDB, site, sha, scope, snap, res.StartedAt, []string{securityPatchComponent}, msg)
}
