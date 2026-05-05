package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/hashicorp/nomad/api"
	"go.opentelemetry.io/otel/codes"

	"github.com/verself/deployment-tools/internal/deploymodel"
	"github.com/verself/deployment-tools/internal/nomadclient"
	"github.com/verself/deployment-tools/internal/runtime"
	"github.com/verself/deployment-tools/internal/sshtun"
)

const defaultNomadRemotePort = 4646

func openNomadForward(ctx context.Context, rt *runtime.Runtime, addr string) (*sshtun.Forward, error) {
	port := defaultNomadRemotePort
	if addr != "" {
		parsed, err := url.Parse(addr)
		if err != nil {
			return nil, fmt.Errorf("parse nomad_addr %q: %w", addr, err)
		}
		if parsed.Port() != "" {
			p, err := strconv.Atoi(parsed.Port())
			if err != nil || p <= 0 || p > 65535 {
				return nil, fmt.Errorf("invalid nomad_addr port %q", parsed.Port())
			}
			port = p
		}
	}
	return rt.SSH.Forward(ctx, "nomad", port)
}

func bindArtifactsInSpec(job *api.Job, bindings map[string]artifactBinding) (map[string]bool, error) {
	seen := map[string]bool{}
	for _, group := range job.TaskGroups {
		for _, task := range group.Tasks {
			for _, artifact := range task.Artifacts {
				if artifact.GetterSource == nil {
					continue
				}
				source := *artifact.GetterSource
				if !strings.HasPrefix(source, artifactSourcePrefix) {
					continue
				}
				output := strings.TrimPrefix(source, artifactSourcePrefix)
				binding, ok := bindings[output]
				if !ok {
					return nil, fmt.Errorf("artifact %q is referenced by authored spec but not declared by nomad_component", output)
				}
				getterOptions := map[string]string{}
				for key, value := range binding.Artifact.GetterOptions {
					getterOptions[key] = value
				}
				getterOptions["checksum"] = binding.Checksum
				artifact.GetterSource = &binding.Artifact.GetterSource
				artifact.GetterOptions = getterOptions
				seen[output] = true
			}
		}
	}
	return seen, nil
}

func canonicalArtifactDigestInput(seen map[string]bool, bindings map[string]artifactBinding) []map[string]string {
	outputs := make([]string, 0, len(seen))
	for output := range seen {
		outputs = append(outputs, output)
	}
	sortStrings(outputs)
	rows := make([]map[string]string, 0, len(outputs))
	for _, output := range outputs {
		binding := bindings[output]
		rows = append(rows, map[string]string{
			"getter_source": binding.Artifact.GetterSource,
			"output":        output,
			"sha256":        binding.Artifact.SHA256,
		})
	}
	return rows
}

func stampNomadSpecMeta(job *api.Job, artifactDigest, runKey, sha string) (string, error) {
	if job.Meta == nil {
		job.Meta = map[string]string{}
	}
	job.Meta["artifact_sha256"] = artifactDigest
	delete(job.Meta, "spec_sha256")
	delete(job.Meta, "deploy_run_key")
	delete(job.Meta, "deploy_sha")
	specBody, err := json.Marshal(struct {
		Job *api.Job `json:"Job"`
	}{Job: job})
	if err != nil {
		return "", fmt.Errorf("encode spec digest input: %w", err)
	}
	specDigest := deploymodel.SHA256(specBody)
	job.Meta["spec_sha256"] = specDigest
	// These are intentionally excluded from spec_sha256 so no-op deploys do not
	// churn every job on repo-only SHA/run-key changes.
	job.Meta["deploy_run_key"] = runKey
	job.Meta["deploy_sha"] = sha
	return specDigest, nil
}

type jobApplyResult struct {
	JobID   string
	Changed bool
}

func applyNomadPlan(ctx context.Context, rt *runtime.Runtime, plan *deployPlan) ([]jobApplyResult, error) {
	forward, err := openNomadForward(ctx, rt, plan.SiteCfg.NomadAddr)
	if err != nil {
		return nil, err
	}
	defer func() { _ = forward.Close() }()
	client, err := nomadclient.New("http://" + forward.ListenAddr)
	if err != nil {
		return nil, err
	}
	results := make([]jobApplyResult, 0, len(plan.Jobs))
	for _, job := range plan.Jobs {
		changed, err := applyNomadJob(ctx, rt, client, job)
		results = append(results, jobApplyResult{JobID: job.JobID, Changed: changed})
		if err != nil {
			return results, fmt.Errorf("%s: %w", job.JobID, err)
		}
	}
	return results, nil
}

func applyNomadJob(ctx context.Context, rt *runtime.Runtime, client *nomadclient.Client, job deploymodel.NomadJob) (bool, error) {
	ctx, span := rt.Tracer.Start(ctx, "verself_deploy.nomad.apply")
	defer span.End()
	spec, err := nomadclient.ParseSpec(job.Spec, "nomad job "+job.JobID)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return false, err
	}
	if spec.SpecDigest != job.SpecSHA256 {
		err := fmt.Errorf("job spec digest mismatch: descriptor=%s spec=%s", job.SpecSHA256, spec.SpecDigest)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return false, err
	}
	if spec.ArtifactDigest != job.ArtifactSHA256 {
		err := fmt.Errorf("job artifact digest mismatch: descriptor=%s spec=%s", job.ArtifactSHA256, spec.ArtifactDigest)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return false, err
	}
	decisionStarted := time.Now()
	decision, err := client.Decide(ctx, spec)
	if err != nil {
		_ = recordNomadSubmitFailed(ctx, rt.DeployDB, rt.Identity.RunKey(), rt.Site, job, decision, time.Since(decisionStarted), err)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return false, err
	}
	if err := recordNomadDecision(ctx, rt.DeployDB, rt.Identity.RunKey(), rt.Site, job, decision, time.Since(decisionStarted)); err != nil {
		return false, err
	}
	if decision.NoOp {
		if err := recordNomadSkipped(ctx, rt.DeployDB, rt.Identity.RunKey(), rt.Site, job, decision); err != nil {
			return false, err
		}
		fmt.Printf("verself-deploy: %s already at desired digests; no submit\n", job.JobID)
		span.SetStatus(codes.Ok, "")
		return false, nil
	}
	submitStarted := time.Now()
	submitted, err := client.Submit(ctx, spec, decision.PriorJobModifyIndex)
	if err != nil {
		_ = recordNomadSubmitFailed(ctx, rt.DeployDB, rt.Identity.RunKey(), rt.Site, job, decision, time.Since(submitStarted), err)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return true, err
	}
	fmt.Printf("verself-deploy: %s submitted job_modify_index=%d eval_id=%s deployment_id=%s\n",
		submitted.JobID, submitted.JobModifyIndex, submitted.EvalID, submitted.DeploymentID)
	if err := recordNomadSubmitted(ctx, rt.DeployDB, rt.Identity.RunKey(), rt.Site, job, decision, submitted, time.Since(submitStarted)); err != nil {
		return true, err
	}
	monitorStarted := time.Now()
	monitor, err := client.Monitor(ctx, submitted)
	if err != nil {
		_ = recordNomadDeploymentFailed(ctx, rt.DeployDB, rt.Identity.RunKey(), rt.Site, job, decision, submitted, monitor, time.Since(monitorStarted), err)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return true, err
	}
	if err := recordNomadDeploymentSucceeded(ctx, rt.DeployDB, rt.Identity.RunKey(), rt.Site, job, decision, submitted, monitor, time.Since(monitorStarted)); err != nil {
		return true, err
	}
	fmt.Printf("verself-deploy: %s healthy\n", submitted.JobID)
	span.SetStatus(codes.Ok, "")
	return true, nil
}

func sortStrings(values []string) {
	if len(values) < 2 {
		return
	}
	for i := 1; i < len(values); i++ {
		for j := i; j > 0 && values[j] < values[j-1]; j-- {
			values[j], values[j-1] = values[j-1], values[j]
		}
	}
}
