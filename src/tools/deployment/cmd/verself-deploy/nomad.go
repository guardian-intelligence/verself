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
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/verself/deployment-tools/internal/deploydb"
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

type jobApplyIntent struct {
	Job      deploymodel.NomadJob
	Spec     *nomadclient.Spec
	Decision nomadclient.Decision
	Changed  bool
}

func applyNomadPlan(ctx context.Context, rt *runtime.Runtime, db *deploydb.Client, plan *deployPlan) ([]jobApplyResult, error) {
	if db == nil {
		return nil, fmt.Errorf("deploy evidence ClickHouse client is required")
	}
	forward, err := openNomadForward(ctx, rt, plan.SiteCfg.NomadAddr)
	if err != nil {
		return nil, err
	}
	defer func() { _ = forward.Close() }()
	client, err := nomadclient.New("http://" + forward.ListenAddr)
	if err != nil {
		return nil, err
	}
	intents := make([]jobApplyIntent, 0, len(plan.Jobs))
	for _, job := range plan.Jobs {
		intent, err := prepareNomadJob(ctx, rt, db, client, job)
		if err != nil {
			return applyResults(intents), fmt.Errorf("%s: %w", job.JobID, err)
		}
		intents = append(intents, intent)
	}
	preArtifactIntents, artifactIntents, err := splitIntentsByPhase(intents)
	if err != nil {
		return applyResults(intents), err
	}
	if err := applyNomadWave(ctx, rt, db, client, deployPhasePreArtifact, deploymodel.ArtifactDelivery{}, preArtifactIntents, nil); err != nil {
		return applyResults(intents), err
	}
	artifacts, err := artifactsForChangedJobs(plan, artifactIntents)
	if err != nil {
		return applyResults(intents), err
	}
	if err := applyNomadWave(ctx, rt, db, client, deployPhaseArtifact, plan.SiteCfg.ArtifactDelivery.ArtifactDelivery, artifactIntents, artifacts); err != nil {
		return applyResults(intents), err
	}
	results := applyResults(intents)
	return results, nil
}

func applyNomadWave(ctx context.Context, rt *runtime.Runtime, db *deploydb.Client, client *nomadclient.Client, wave string, delivery deploymodel.ArtifactDelivery, intents []jobApplyIntent, artifacts []deploymodel.Artifact) error {
	if len(intents) == 0 {
		return nil
	}
	ctx, span := rt.Tracer.Start(ctx, "verself_deploy.nomad.wave",
		trace.WithAttributes(
			attribute.String("verself.deploy_wave", wave),
			attribute.Int("verself.nomad_job_count", len(intents)),
			attribute.Int("verself.changed_job_count", changedIntentCount(intents)),
			attribute.Int("verself.artifact_count", len(artifacts)),
		),
	)
	defer span.End()
	started := time.Now()
	if err := recordDeployWaveStarted(ctx, span, db, rt.Identity.RunKey(), rt.Site, wave, intents, artifacts, started); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	if wave == deployPhaseArtifact {
		if err := publishArtifacts(ctx, rt, delivery, artifacts); err != nil {
			_ = recordDeployWaveFailed(ctx, span, db, rt.Identity.RunKey(), rt.Site, wave, intents, artifacts, started, err)
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			return err
		}
	}
	for i, intent := range intents {
		if !intent.Changed {
			continue
		}
		if err := submitNomadJob(ctx, rt, db, client, intent); err != nil {
			_ = recordDeployWaveFailed(ctx, span, db, rt.Identity.RunKey(), rt.Site, wave, intents[:i+1], artifacts, started, err)
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			return fmt.Errorf("%s: %w", intent.Job.JobID, err)
		}
	}
	if err := recordDeployWaveSucceeded(ctx, span, db, rt.Identity.RunKey(), rt.Site, wave, intents, artifacts, started); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	span.SetStatus(codes.Ok, "")
	return nil
}

func prepareNomadJob(ctx context.Context, rt *runtime.Runtime, db *deploydb.Client, client *nomadclient.Client, job deploymodel.NomadJob) (jobApplyIntent, error) {
	ctx, span := rt.Tracer.Start(ctx, "verself_deploy.nomad.apply")
	defer span.End()
	span.SetAttributes(
		attribute.String("nomad.job_id", job.JobID),
		attribute.String("verself.deploy_wave", job.DeployPhase),
		attribute.StringSlice("verself.artifact_outputs", job.ArtifactOutputs),
	)
	spec, err := nomadclient.ParseSpec(job.Spec, "nomad job "+job.JobID)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return jobApplyIntent{}, err
	}
	if spec.SpecDigest != job.SpecSHA256 {
		err := fmt.Errorf("job spec digest mismatch: descriptor=%s spec=%s", job.SpecSHA256, spec.SpecDigest)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return jobApplyIntent{}, err
	}
	if spec.ArtifactDigest != job.ArtifactSHA256 {
		err := fmt.Errorf("job artifact digest mismatch: descriptor=%s spec=%s", job.ArtifactSHA256, spec.ArtifactDigest)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return jobApplyIntent{}, err
	}
	decisionStarted := time.Now()
	decision, err := client.Decide(ctx, spec)
	if err != nil {
		recordNomadSubmitFailed(span, rt.Identity.RunKey(), rt.Site, job, decision, time.Since(decisionStarted), err)
		_ = persistNomadSubmitFailed(ctx, db, rt.Identity.RunKey(), rt.Site, job, decision, time.Since(decisionStarted), err)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return jobApplyIntent{}, err
	}
	recordNomadDecision(span, rt.Identity.RunKey(), rt.Site, job, decision, time.Since(decisionStarted))
	if err := persistNomadDecision(ctx, db, rt.Identity.RunKey(), rt.Site, job, decision, time.Since(decisionStarted)); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return jobApplyIntent{}, err
	}
	if decision.NoOp {
		recordNomadSkipped(span, rt.Identity.RunKey(), rt.Site, job, decision)
		fmt.Printf("verself-deploy: %s already at desired digests; no submit\n", job.JobID)
		span.SetStatus(codes.Ok, "")
		return jobApplyIntent{Job: job, Spec: spec, Decision: decision, Changed: false}, nil
	}
	span.SetAttributes(attribute.Bool("nomad.decision.noop", false))
	span.SetStatus(codes.Ok, "")
	return jobApplyIntent{Job: job, Spec: spec, Decision: decision, Changed: true}, nil
}

func submitNomadJob(ctx context.Context, rt *runtime.Runtime, db *deploydb.Client, client *nomadclient.Client, intent jobApplyIntent) error {
	ctx, span := rt.Tracer.Start(ctx, "verself_deploy.nomad.submit",
		trace.WithAttributes(
			attribute.String("nomad.job_id", intent.Job.JobID),
			attribute.String("verself.deploy_wave", intent.Job.DeployPhase),
		),
	)
	defer span.End()
	submitStarted := time.Now()
	submitted, err := client.Submit(ctx, intent.Spec, intent.Decision.PriorJobModifyIndex)
	if err != nil {
		recordNomadSubmitFailed(span, rt.Identity.RunKey(), rt.Site, intent.Job, intent.Decision, time.Since(submitStarted), err)
		_ = persistNomadSubmitFailed(ctx, db, rt.Identity.RunKey(), rt.Site, intent.Job, intent.Decision, time.Since(submitStarted), err)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	fmt.Printf("verself-deploy: %s submitted job_modify_index=%d eval_id=%s deployment_id=%s\n",
		submitted.JobID, submitted.JobModifyIndex, submitted.EvalID, submitted.DeploymentID)
	recordNomadSubmitted(span, rt.Identity.RunKey(), rt.Site, intent.Job, intent.Decision, submitted, time.Since(submitStarted))
	if err := persistNomadSubmitted(ctx, db, rt.Identity.RunKey(), rt.Site, intent.Job, intent.Decision, submitted, time.Since(submitStarted)); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	monitorStarted := time.Now()
	monitor, err := client.Monitor(ctx, submitted)
	if err != nil {
		recordNomadDeploymentFailed(span, rt.Identity.RunKey(), rt.Site, intent.Job, submitted, monitor, time.Since(monitorStarted), err)
		_ = persistNomadDeploymentFailed(ctx, db, rt.Identity.RunKey(), rt.Site, intent.Job, submitted, monitor, time.Since(monitorStarted), err)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	recordNomadDeploymentSucceeded(span, rt.Identity.RunKey(), rt.Site, intent.Job, submitted, monitor, time.Since(monitorStarted))
	if err := persistNomadDeploymentSucceeded(ctx, db, rt.Identity.RunKey(), rt.Site, intent.Job, submitted, monitor, time.Since(monitorStarted)); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	fmt.Printf("verself-deploy: %s healthy\n", submitted.JobID)
	span.SetAttributes(attribute.String("nomad.terminal_status", monitor.TerminalStatus))
	span.SetStatus(codes.Ok, "")
	return nil
}

func splitIntentsByPhase(intents []jobApplyIntent) ([]jobApplyIntent, []jobApplyIntent, error) {
	preArtifact := []jobApplyIntent{}
	artifact := []jobApplyIntent{}
	for _, intent := range intents {
		switch intent.Job.DeployPhase {
		case deployPhasePreArtifact:
			if len(intent.Job.ArtifactOutputs) > 0 {
				return nil, nil, fmt.Errorf("%s: %s jobs cannot reference artifacts", intent.Job.JobID, deployPhasePreArtifact)
			}
			preArtifact = append(preArtifact, intent)
		case deployPhaseArtifact:
			artifact = append(artifact, intent)
		default:
			return nil, nil, fmt.Errorf("%s: unsupported deploy phase %q", intent.Job.JobID, intent.Job.DeployPhase)
		}
	}
	return preArtifact, artifact, nil
}

func changedIntentCount(intents []jobApplyIntent) int {
	count := 0
	for _, intent := range intents {
		if intent.Changed {
			count++
		}
	}
	return count
}

func applyResults(intents []jobApplyIntent) []jobApplyResult {
	results := make([]jobApplyResult, 0, len(intents))
	for _, intent := range intents {
		results = append(results, jobApplyResult{JobID: intent.Job.JobID, Changed: intent.Changed})
	}
	return results
}

func artifactsForChangedJobs(plan *deployPlan, intents []jobApplyIntent) ([]deploymodel.Artifact, error) {
	outputs := map[string]bool{}
	for _, intent := range intents {
		if !intent.Changed {
			continue
		}
		for _, output := range intent.Job.ArtifactOutputs {
			outputs[output] = true
		}
	}
	if len(outputs) == 0 {
		return nil, nil
	}
	artifacts := make([]deploymodel.Artifact, 0, len(outputs))
	for _, artifact := range plan.Artifacts {
		if !outputs[artifact.Output] {
			continue
		}
		artifacts = append(artifacts, artifact)
		delete(outputs, artifact.Output)
	}
	if len(outputs) > 0 {
		missing := make([]string, 0, len(outputs))
		for output := range outputs {
			missing = append(missing, output)
		}
		sortStrings(missing)
		return nil, fmt.Errorf("changed jobs reference unknown artifacts: %s", strings.Join(missing, ", "))
	}
	return artifacts, nil
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
