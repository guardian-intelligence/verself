package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const nomadHTTPPort = 4646

type mkskPublishPayload struct {
	PackageName  string `json:"package"`
	Channel      string `json:"channel"`
	Version      string `json:"version"`
	SourceRef    string `json:"source_ref"`
	SourceCommit string `json:"source_commit"`
	Platform     string `json:"platform"`
	BuilderID    string `json:"builder_id"`
}

type nomadDispatchRequest struct {
	Meta             map[string]string `json:"Meta"`
	Payload          []byte            `json:"Payload"`
	IdPrefixTemplate string            `json:"IdPrefixTemplate"`
}

type nomadDispatchResponse struct {
	EvalID          string `json:"EvalID"`
	DispatchedJobID string `json:"DispatchedJobID"`
}

type nomadAllocation struct {
	ID            string                    `json:"ID"`
	ClientStatus  string                    `json:"ClientStatus"`
	DesiredStatus string                    `json:"DesiredStatus"`
	TaskStates    map[string]nomadTaskState `json:"TaskStates"`
}

type nomadTaskState struct {
	State  string           `json:"State"`
	Failed bool             `json:"Failed"`
	Events []nomadTaskEvent `json:"Events"`
}

type nomadTaskEvent struct {
	Type           string `json:"Type"`
	DisplayMessage string `json:"DisplayMessage"`
	Message        string `json:"Message"`
	ExitCode       int    `json:"ExitCode"`
}

func dispatchMkskRelease(ctx context.Context, cfg mkskConfig) error {
	if strings.TrimSpace(cfg.channel) == "" {
		return fmt.Errorf("--channel is required")
	}
	if err := validateDispatchVersion(cfg.channel, cfg.version); err != nil {
		return err
	}
	if _, _, err := parsePlatform(cfg.platform); err != nil {
		return err
	}
	sourceCommit, err := resolveDispatchCommit(ctx, cfg)
	if err != nil {
		return err
	}
	if cfg.sourceRef == "HEAD" && sourceCommit == mustResolveHead(ctx, cfg.repoRoot) {
		dirty, err := dirtyWorkspace(ctx, cfg.repoRoot)
		if err != nil {
			return err
		}
		if dirty {
			return fmt.Errorf("prod dispatch requires a clean source commit; use --local for dirty workspace inspection")
		}
	}
	payload := mkskPublishPayload{
		PackageName:  mkskPackageName,
		Channel:      cfg.channel,
		Version:      strings.TrimSpace(cfg.version),
		SourceRef:    cfg.sourceRef,
		SourceCommit: sourceCommit,
		Platform:     cfg.platform,
		BuilderID:    cfg.builderID,
	}
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	nomadAddr := strings.TrimSpace(cfg.nomadAddr)
	var closer io.Closer
	if nomadAddr == "" {
		access, err := resolveSiteSSHAccess(cfg.repoRoot, cfg.site)
		if err != nil {
			return err
		}
		forward, err := openNomadSSHForward(ctx, access)
		if err != nil {
			return err
		}
		closer = forward
		defer func() { _ = closer.Close() }()
		nomadAddr = "http://" + forward.ListenAddr
	}
	resp, err := dispatchNomad(ctx, nomadAddr, cfg.nomadJobID, payloadBytes, map[string]string{
		"package":       mkskPackageName,
		"channel":       cfg.channel,
		"version":       payload.Version,
		"source_commit": sourceCommit,
	}, "mksk-"+cfg.channel+"-"+shortSHA(sourceCommit))
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(os.Stdout, "dispatched release job: %s eval=%s\n", resp.DispatchedJobID, resp.EvalID); err != nil {
		return err
	}
	if !cfg.wait {
		return nil
	}
	waitCtx, cancel := context.WithTimeout(ctx, cfg.waitTimeout)
	defer cancel()
	if err := waitNomadJobComplete(waitCtx, nomadAddr, resp.DispatchedJobID); err != nil {
		return err
	}
	return stdoutf("release job completed: %s\n", resp.DispatchedJobID)
}

func resolveDispatchCommit(ctx context.Context, cfg mkskConfig) (string, error) {
	sourceRef := strings.TrimSpace(cfg.sourceRef)
	if sourceRef == "" || sourceRef == "HEAD" || gitSHARE.MatchString(sourceRef) {
		return resolveCommit(ctx, cfg.repoRoot, sourceRef)
	}
	remoteCommit, err := resolveRemoteCommit(ctx, cfg.repoRoot, sourceRef)
	if err == nil {
		return remoteCommit, nil
	}
	return "", fmt.Errorf("resolve source ref %q from origin: %w", sourceRef, err)
}

func resolveRemoteCommit(ctx context.Context, repoRoot, sourceRef string) (string, error) {
	candidates := remoteRefCandidates(sourceRef)
	args := append([]string{"ls-remote", "--exit-code", "origin"}, candidates...)
	result, err := runCommand(ctx, repoRoot, "git", args...)
	if err != nil {
		return "", err
	}
	lines := nonEmptyLines(result.stdout)
	if len(lines) == 0 {
		return "", fmt.Errorf("origin returned no refs for %q", sourceRef)
	}
	for _, candidate := range candidates {
		for _, line := range lines {
			commit, ref, ok := strings.Cut(line, "\t")
			if !ok || !gitSHARE.MatchString(commit) {
				continue
			}
			if ref == candidate {
				return commit, nil
			}
		}
	}
	for _, line := range lines {
		commit, _, ok := strings.Cut(line, "\t")
		if ok && gitSHARE.MatchString(commit) {
			return commit, nil
		}
	}
	return "", fmt.Errorf("origin returned no commit for %q", sourceRef)
}

func remoteRefCandidates(sourceRef string) []string {
	if strings.HasPrefix(sourceRef, "refs/") {
		return []string{sourceRef}
	}
	return []string{
		"refs/heads/" + sourceRef,
		"refs/tags/" + sourceRef + "^{}",
		"refs/tags/" + sourceRef,
	}
}

func validateDispatchVersion(channel, version string) error {
	channel = strings.TrimSpace(channel)
	version = strings.TrimSpace(version)
	switch channel {
	case "nightly":
		if version != "" && !nightlySemVerRE.MatchString(version) {
			return fmt.Errorf("nightly version must match MAJOR.MINOR.PATCH-nightly.YYYYMMDD.N")
		}
	case "rc":
		if !rcSemVerRE.MatchString(version) {
			return fmt.Errorf("rc version must match MAJOR.MINOR.PATCH-rc.N")
		}
	case "stable":
		if !finalSemVerRE.MatchString(version) {
			return fmt.Errorf("stable version must be final SemVer MAJOR.MINOR.PATCH")
		}
	default:
		return fmt.Errorf("--channel must be nightly, rc, or stable")
	}
	return nil
}

func mustResolveHead(ctx context.Context, repoRoot string) string {
	head, err := resolveCommit(ctx, repoRoot, "HEAD")
	if err != nil {
		return ""
	}
	return head
}

func dispatchNomad(ctx context.Context, addr, jobID string, payload []byte, meta map[string]string, prefix string) (nomadDispatchResponse, error) {
	if strings.TrimSpace(addr) == "" {
		return nomadDispatchResponse{}, fmt.Errorf("nomad address is required")
	}
	if strings.TrimSpace(jobID) == "" {
		return nomadDispatchResponse{}, fmt.Errorf("nomad job id is required")
	}
	body, err := json.Marshal(nomadDispatchRequest{
		Meta:             meta,
		Payload:          payload,
		IdPrefixTemplate: prefix,
	})
	if err != nil {
		return nomadDispatchResponse{}, err
	}
	endpoint := strings.TrimRight(addr, "/") + "/v1/job/" + url.PathEscape(jobID) + "/dispatch"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nomadDispatchResponse{}, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nomadDispatchResponse{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nomadDispatchResponse{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nomadDispatchResponse{}, fmt.Errorf("nomad dispatch %s: status=%d body=%s", jobID, resp.StatusCode, strings.TrimSpace(string(respBody)))
	}
	var decoded nomadDispatchResponse
	if err := json.Unmarshal(respBody, &decoded); err != nil {
		return nomadDispatchResponse{}, err
	}
	if decoded.DispatchedJobID == "" {
		return nomadDispatchResponse{}, fmt.Errorf("nomad dispatch %s returned empty dispatched job id", jobID)
	}
	return decoded, nil
}

func waitNomadJobComplete(ctx context.Context, addr, jobID string) error {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		allocs, err := nomadJobAllocations(ctx, addr, jobID)
		if err != nil {
			return err
		}
		if done, err := allocationsDone(allocs); done || err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("wait for nomad job %s: %w", jobID, ctx.Err())
		case <-ticker.C:
		}
	}
}

func nomadJobAllocations(ctx context.Context, addr, jobID string) ([]nomadAllocation, error) {
	endpoint := strings.TrimRight(addr, "/") + "/v1/job/" + url.PathEscape(jobID) + "/allocations"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("nomad allocations %s: status=%d body=%s", jobID, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var allocs []nomadAllocation
	if err := json.Unmarshal(body, &allocs); err != nil {
		return nil, err
	}
	return allocs, nil
}

func allocationsDone(allocs []nomadAllocation) (bool, error) {
	if len(allocs) == 0 {
		return false, nil
	}
	for _, alloc := range allocs {
		switch alloc.ClientStatus {
		case "complete":
			return true, nil
		case "failed", "lost":
			return true, fmt.Errorf("nomad allocation %s %s: %s", alloc.ID, alloc.ClientStatus, taskFailureSummary(alloc.TaskStates))
		}
	}
	return false, nil
}

func taskFailureSummary(states map[string]nomadTaskState) string {
	var parts []string
	for task, state := range states {
		if !state.Failed && state.State != "dead" {
			continue
		}
		message := state.State
		if len(state.Events) > 0 {
			event := state.Events[len(state.Events)-1]
			if event.DisplayMessage != "" {
				message = event.DisplayMessage
			} else if event.Message != "" {
				message = event.Message
			} else if event.Type != "" {
				message = event.Type
			}
			if event.ExitCode != 0 {
				message += " exit_code=" + strconv.Itoa(event.ExitCode)
			}
		}
		parts = append(parts, task+"="+message)
	}
	if len(parts) == 0 {
		return "no task failure details"
	}
	return strings.Join(parts, "; ")
}

type siteSSHAccess struct {
	Targets []sshTarget
}

type sshTarget struct {
	Label string
	Host  string
	User  string
	Port  int
}

func resolveSiteSSHAccess(repoRoot, site string) (siteSSHAccess, error) {
	path := filepath.Join(repoRoot, "src", "host", "sites", site, "inventory.ini")
	body, err := os.ReadFile(path)
	if err != nil {
		return siteSSHAccess{}, fmt.Errorf("read %s: %w", path, err)
	}
	section := ""
	ansibleUser := ""
	var primary sshTarget
	var recovery sshTarget
	for _, raw := range strings.Split(string(body), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.Trim(line, "[]")
			continue
		}
		switch section {
		case "infra":
			if primary.Host != "" {
				continue
			}
			fields := strings.Fields(line)
			if len(fields) == 0 {
				continue
			}
			primary = sshTarget{Label: "primary", Host: fields[0], Port: 22}
			for _, field := range fields[1:] {
				key, value, ok := strings.Cut(field, "=")
				if !ok {
					continue
				}
				switch key {
				case "ansible_host":
					primary.Host = value
				case "verself_recovery_ssh_host":
					recovery.Host = value
				case "verself_recovery_ssh_user":
					recovery.User = value
				case "verself_recovery_ssh_port":
					port, err := strconv.Atoi(value)
					if err != nil {
						return siteSSHAccess{}, fmt.Errorf("invalid recovery ssh port %q: %w", value, err)
					}
					recovery.Port = port
				}
			}
		case "all:vars":
			key, value, ok := strings.Cut(line, "=")
			if ok && key == "ansible_user" {
				ansibleUser = value
			}
		}
	}
	if ansibleUser == "" {
		return siteSSHAccess{}, fmt.Errorf("%s does not declare ansible_user", path)
	}
	primary.User = ansibleUser
	targets := []sshTarget{}
	if primary.Host != "" {
		targets = append(targets, primary)
	}
	if recovery.Host != "" {
		if recovery.User == "" {
			return siteSSHAccess{}, fmt.Errorf("%s recovery SSH host is missing user", path)
		}
		if recovery.Port == 0 {
			recovery.Port = 22
		}
		recovery.Label = "recovery"
		targets = append(targets, recovery)
	}
	if len(targets) == 0 {
		return siteSSHAccess{}, fmt.Errorf("%s does not declare an infra SSH target", path)
	}
	return siteSSHAccess{Targets: targets}, nil
}
