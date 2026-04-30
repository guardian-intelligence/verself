// Command nomad-deploy submits a rendered Nomad job spec to the local
// Nomad agent's HTTP API and watches the deployment to completion.
//
// It is the only software-deploy primitive on the bare-metal node: the
// substrate Ansible playbook stops at /etc/verself/jobs/<id>.nomad.json
// and /etc/verself/<id>/auth_audience; this binary takes the spec, fills
// in deploy-time-dynamic values (binary sha256, auth audience), submits,
// and polls the deployment until it's healthy or the deadline expires.
//
// Aspect-deploy invokes one nomad-deploy per opted-in component over SSH
// after the substrate playbook returns. Ansible no longer touches Nomad's
// API or the alloc lifecycle.
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

const (
	defaultNomadAddr = "http://127.0.0.1:4646"
	defaultTimeout   = 5 * time.Minute
	pollInterval     = 2 * time.Second

	authAudienceSentinel = "{{ component_auth_audience }}"
)

func main() {
	var (
		component  = flag.String("component", "", "component name (snake_case)")
		specPath   = flag.String("spec", "", "rendered job spec path; default /etc/verself/jobs/<id>.nomad.json")
		binaryPath = flag.String("binary", "", "service binary path; default /opt/verself/profile/bin/<id>")
		audPath    = flag.String("auth-audience-file", "", "auth audience file; default /etc/verself/<id>/auth_audience")
		nomadAddr  = flag.String("nomad-addr", defaultNomadAddr, "Nomad agent HTTP address")
		timeout    = flag.Duration("timeout", defaultTimeout, "deployment poll timeout")
	)
	flag.Parse()

	if *component == "" {
		fail("--component is required")
	}
	jobID := strings.ReplaceAll(*component, "_", "-")
	if *specPath == "" {
		*specPath = "/etc/verself/jobs/" + jobID + ".nomad.json"
	}
	if *binaryPath == "" {
		*binaryPath = "/opt/verself/profile/bin/" + jobID
	}
	if *audPath == "" {
		*audPath = "/etc/verself/" + jobID + "/auth_audience"
	}

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	if err := run(ctx, runArgs{
		jobID:      jobID,
		specPath:   *specPath,
		binaryPath: *binaryPath,
		audPath:    *audPath,
		nomadAddr:  *nomadAddr,
	}); err != nil {
		fail(err.Error())
	}
}

type runArgs struct {
	jobID      string
	specPath   string
	binaryPath string
	audPath    string
	nomadAddr  string
}

func run(ctx context.Context, args runArgs) error {
	specBytes, err := os.ReadFile(args.specPath)
	if err != nil {
		return fmt.Errorf("read spec: %w", err)
	}

	digest, err := sha256File(args.binaryPath)
	if err != nil {
		return fmt.Errorf("digest binary: %w", err)
	}

	audience, audErr := os.ReadFile(args.audPath)
	if audErr != nil && !errors.Is(audErr, os.ErrNotExist) {
		return fmt.Errorf("read auth audience: %w", audErr)
	}
	audienceStr := strings.TrimSpace(string(audience))

	var spec map[string]any
	if err := json.Unmarshal(specBytes, &spec); err != nil {
		return fmt.Errorf("parse spec: %w", err)
	}

	if err := substituteAuthAudience(spec, audienceStr); err != nil {
		return err
	}
	if err := injectMeta(spec, "binary_sha256", digest); err != nil {
		return err
	}

	currentDigest, currentVersion, err := currentJobMeta(ctx, args.nomadAddr, args.jobID)
	if err != nil {
		return fmt.Errorf("inspect current job: %w", err)
	}
	if currentDigest == digest {
		fmt.Fprintf(os.Stdout, "nomad-deploy: %s already at binary_sha256=%s (job version %d); no submit\n",
			args.jobID, digest[:12], currentVersion)
		return nil
	}

	deployment, err := submit(ctx, args.nomadAddr, spec)
	if err != nil {
		return fmt.Errorf("submit: %w", err)
	}
	fmt.Fprintf(os.Stdout, "nomad-deploy: %s submitted job_modify_index=%d eval_id=%s\n",
		args.jobID, deployment.JobModifyIndex, deployment.EvalID)

	if err := awaitHealthy(ctx, args.nomadAddr, args.jobID, deployment.JobModifyIndex); err != nil {
		return fmt.Errorf("await healthy: %w", err)
	}
	fmt.Fprintf(os.Stdout, "nomad-deploy: %s healthy at binary_sha256=%s\n", args.jobID, digest[:12])
	return nil
}

func sha256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// substituteAuthAudience walks Tasks > Env and replaces the
// `{{ component_auth_audience }}` sentinel with the concrete audience
// read off disk. The renderer leaves only this sentinel; everything
// else is already a literal value.
func substituteAuthAudience(spec map[string]any, audience string) error {
	job, ok := spec["Job"].(map[string]any)
	if !ok {
		return errors.New("spec.Job: missing or wrong type")
	}
	groups, _ := job["TaskGroups"].([]any)
	for _, g := range groups {
		group, _ := g.(map[string]any)
		tasks, _ := group["Tasks"].([]any)
		for _, t := range tasks {
			task, _ := t.(map[string]any)
			env, _ := task["Env"].(map[string]any)
			needsAudience := false
			for _, v := range env {
				if s, ok := v.(string); ok && strings.Contains(s, authAudienceSentinel) {
					needsAudience = true
					break
				}
			}
			if needsAudience && audience == "" {
				return errors.New("spec references {{ component_auth_audience }} but auth-audience-file is missing or empty")
			}
			for k, v := range env {
				if s, ok := v.(string); ok {
					env[k] = strings.ReplaceAll(s, authAudienceSentinel, audience)
				}
			}
		}
	}
	return nil
}

// injectMeta sets a single key on Job.Meta, overwriting any prior value
// at that key. Other Meta keys are preserved so future per-component
// metadata (e.g. provenance, change-id) coexists.
func injectMeta(spec map[string]any, key, value string) error {
	job, ok := spec["Job"].(map[string]any)
	if !ok {
		return errors.New("spec.Job: missing or wrong type")
	}
	meta, _ := job["Meta"].(map[string]any)
	if meta == nil {
		meta = map[string]any{}
	}
	meta[key] = value
	job["Meta"] = meta
	return nil
}

// currentJobMeta returns the binary_sha256 + Version of the currently-
// registered job, or "" / 0 when the job is unknown to the Nomad agent.
func currentJobMeta(ctx context.Context, nomadAddr, jobID string) (string, int64, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, nomadAddr+"/v1/job/"+jobID, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return "", 0, nil
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", 0, fmt.Errorf("GET /v1/job/%s: %d %s", jobID, resp.StatusCode, body)
	}
	var payload struct {
		Meta    map[string]string `json:"Meta"`
		Version int64             `json:"Version"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", 0, err
	}
	return payload.Meta["binary_sha256"], payload.Version, nil
}

type submitResult struct {
	EvalID         string `json:"EvalID"`
	JobModifyIndex int64  `json:"JobModifyIndex"`
}

func submit(ctx context.Context, nomadAddr string, spec map[string]any) (submitResult, error) {
	body, err := json.Marshal(spec)
	if err != nil {
		return submitResult{}, err
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, nomadAddr+"/v1/jobs", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return submitResult{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		out, _ := io.ReadAll(resp.Body)
		return submitResult{}, fmt.Errorf("POST /v1/jobs: %d %s", resp.StatusCode, out)
	}
	var out submitResult
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return submitResult{}, err
	}
	return out, nil
}

// awaitHealthy polls the latest deployment for the job. For service-type
// jobs Nomad creates a deployment per submission; we wait for that
// deployment to either reach Status="successful" or fail.
func awaitHealthy(ctx context.Context, nomadAddr, jobID string, jobModifyIndex int64) error {
	deadline := time.NewTimer(0)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
		}
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, nomadAddr+"/v1/job/"+jobID+"/deployment", nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return err
		}
		var payload struct {
			ID     string `json:"ID"`
			Status string `json:"Status"`
			TaskGroups map[string]struct {
				DesiredTotal     int  `json:"DesiredTotal"`
				HealthyAllocs    int  `json:"HealthyAllocs"`
				UnhealthyAllocs  int  `json:"UnhealthyAllocs"`
				PlacedAllocs     int  `json:"PlacedAllocs"`
				RequireProgressBy string `json:"RequireProgressBy"`
				ProgressDeadline string `json:"ProgressDeadline"`
			} `json:"TaskGroups"`
			JobVersion       int64  `json:"JobVersion"`
			StatusDescription string `json:"StatusDescription"`
		}
		if resp.StatusCode == http.StatusOK {
			if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
				resp.Body.Close()
				return err
			}
			resp.Body.Close()
			switch payload.Status {
			case "successful":
				return nil
			case "failed", "cancelled":
				return fmt.Errorf("deployment %s ended with status=%s: %s", payload.ID, payload.Status, payload.StatusDescription)
			}
		} else if resp.StatusCode == http.StatusNotFound {
			// service-type jobs always create a deployment, but on the
			// very first submit there can be a brief window before the
			// deployment object exists.
			resp.Body.Close()
		} else {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			return fmt.Errorf("GET /v1/job/%s/deployment: %d %s", jobID, resp.StatusCode, body)
		}
		deadline.Reset(pollInterval)
	}
}

func fail(msg string) {
	fmt.Fprintf(os.Stderr, "ERROR: %s\n", msg)
	os.Exit(1)
}
