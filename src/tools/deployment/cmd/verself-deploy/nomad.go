package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/hashicorp/nomad/api"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

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

func stampNomadSpecMeta(job *api.Job, artifactDigest, inputDigest, runKey, sha string) (string, error) {
	if job.Meta == nil {
		job.Meta = map[string]string{}
	}
	job.Meta["artifact_sha256"] = artifactDigest
	delete(job.Meta, "input_sha256")
	if inputDigest != "" {
		job.Meta["input_sha256"] = inputDigest
	}
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

type jobRunResult struct {
	JobID string
}

func applyNomadPlan(ctx context.Context, rt *runtime.Runtime, plan *deployPlan) ([]jobRunResult, error) {
	forward, err := openNomadForward(ctx, rt, plan.SiteCfg.NomadAddr)
	if err != nil {
		return nil, err
	}
	defer func() { _ = forward.Close() }()
	client, err := nomadclient.New("http://" + forward.ListenAddr)
	if err != nil {
		return nil, err
	}
	jobsByPhase, err := groupJobsByPhase(plan.Jobs)
	if err != nil {
		return runResults(plan.Jobs), err
	}
	for _, phase := range deployPhaseOrder {
		phaseJobs := jobsByPhase[phase]
		var artifacts []deploymodel.Artifact
		if phase != deployPhasePreArtifact {
			artifacts, err = artifactsForJobs(plan, phaseJobs)
			if err != nil {
				return runResults(plan.Jobs), err
			}
		}
		if err := applyNomadWave(ctx, rt, client, forward.ListenAddr, phase, plan.SiteCfg.ArtifactDelivery.ArtifactDelivery, phaseJobs, artifacts); err != nil {
			return runResults(plan.Jobs), err
		}
	}
	return runResults(plan.Jobs), nil
}

func applyNomadWave(ctx context.Context, rt *runtime.Runtime, client *nomadclient.Client, nomadAddr, wave string, delivery deploymodel.ArtifactDelivery, jobs []deploymodel.NomadJob, artifacts []deploymodel.Artifact) error {
	if len(jobs) == 0 {
		return nil
	}
	ctx, span := rt.Tracer.Start(ctx, "verself_deploy.nomad.wave",
		trace.WithAttributes(
			attribute.String("verself.deploy_wave", wave),
			attribute.Int("verself.nomad_job_count", len(jobs)),
			attribute.Int("verself.submitted_job_count", len(jobs)),
			attribute.Int("verself.artifact_count", len(artifacts)),
		),
	)
	defer span.End()
	started := time.Now()
	recordDeployWaveStarted(span, rt.Identity.RunKey(), rt.Site, wave, jobs, artifacts, started)
	if wave != deployPhasePreArtifact && len(artifacts) > 0 {
		if err := ensureArtifactOriginAvailable(ctx, rt, client); err != nil {
			recordDeployWaveFailed(span, rt.Identity.RunKey(), rt.Site, wave, jobs, artifacts, started, err)
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			return err
		}
		if err := publishArtifacts(ctx, rt, delivery, artifacts); err != nil {
			recordDeployWaveFailed(span, rt.Identity.RunKey(), rt.Site, wave, jobs, artifacts, started, err)
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			return err
		}
	}
	for i, job := range jobs {
		if err := runNomadJob(ctx, rt, nomadAddr, job); err != nil {
			recordDeployWaveFailed(span, rt.Identity.RunKey(), rt.Site, wave, jobs[:i+1], artifacts, started, err)
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			return fmt.Errorf("%s: %w", job.JobID, err)
		}
	}
	recordDeployWaveSucceeded(span, rt.Identity.RunKey(), rt.Site, wave, jobs, artifacts, started)
	span.SetStatus(codes.Ok, "")
	return nil
}

func runNomadJob(ctx context.Context, rt *runtime.Runtime, nomadAddr string, job deploymodel.NomadJob) error {
	ctx, span := rt.Tracer.Start(ctx, "verself_deploy.nomad.job_run",
		trace.WithAttributes(
			attribute.String("nomad.job_id", job.JobID),
			attribute.String("verself.deploy_wave", job.DeployPhase),
			attribute.String("verself.spec_sha256", job.SpecSHA256),
			attribute.String("verself.artifact_sha256", job.ArtifactSHA256),
			attribute.String("verself.input_sha256", job.InputSHA256),
		),
	)
	defer span.End()

	specPath, cleanup, err := writeNomadJobSpec(job)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	defer cleanup()

	bin, err := nomadBin()
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}

	started := time.Now()
	recordNomadRunStarted(span, rt.Identity.RunKey(), rt.Site, job, started)
	cmd := exec.CommandContext(ctx, bin, "job", "run", "-json", specPath)
	cmd.Env = nomadCommandEnv(os.Environ(), nomadAddr)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = io.MultiWriter(os.Stdout, &stdout)
	cmd.Stderr = io.MultiWriter(os.Stderr, &stderr)
	if err := cmd.Run(); err != nil {
		recordNomadRunFailed(span, rt.Identity.RunKey(), rt.Site, job, time.Since(started), stderr.String(), err)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return fmt.Errorf("nomad job run: %w", err)
	}
	recordNomadRunSucceeded(span, rt.Identity.RunKey(), rt.Site, job, time.Since(started), stdout.String())
	span.SetStatus(codes.Ok, "")
	return nil
}

func writeNomadJobSpec(job deploymodel.NomadJob) (string, func(), error) {
	pattern := "verself-nomad-" + strings.NewReplacer("/", "_", ":", "_").Replace(job.JobID) + "-*.json"
	f, err := os.CreateTemp("", pattern)
	if err != nil {
		return "", func() {}, fmt.Errorf("create temporary Nomad spec: %w", err)
	}
	cleanup := func() { _ = os.Remove(f.Name()) }
	if _, err := f.Write(job.Spec); err != nil {
		_ = f.Close()
		cleanup()
		return "", func() {}, fmt.Errorf("write temporary Nomad spec: %w", err)
	}
	if err := f.Close(); err != nil {
		cleanup()
		return "", func() {}, fmt.Errorf("close temporary Nomad spec: %w", err)
	}
	return f.Name(), cleanup, nil
}

func nomadBin() (string, error) {
	if configured := strings.TrimSpace(os.Getenv("VERSELF_NOMAD_BIN")); configured != "" {
		if filepath.IsAbs(configured) {
			return configured, nil
		}
		resolved, err := exec.LookPath(configured)
		if err != nil {
			return "", fmt.Errorf("resolve VERSELF_NOMAD_BIN %q: %w", configured, err)
		}
		return resolved, nil
	}
	resolved, err := exec.LookPath("nomad")
	if err != nil {
		return "", fmt.Errorf("resolve nomad binary: %w", err)
	}
	return resolved, nil
}

func nomadCommandEnv(parent []string, addr string) []string {
	env := make([]string, 0, len(parent)+2)
	for _, item := range parent {
		if strings.HasPrefix(item, "NOMAD_ADDR=") || strings.HasPrefix(item, "NOMAD_CLI_NO_COLOR=") {
			continue
		}
		env = append(env, item)
	}
	env = append(env, "NOMAD_ADDR=http://"+addr)
	env = append(env, "NOMAD_CLI_NO_COLOR=1")
	return env
}

func groupJobsByPhase(jobs []deploymodel.NomadJob) (map[string][]deploymodel.NomadJob, error) {
	byPhase := map[string][]deploymodel.NomadJob{}
	for _, job := range jobs {
		if !validDeployPhase(job.DeployPhase) {
			return nil, fmt.Errorf("%s: unsupported deploy phase %q", job.JobID, job.DeployPhase)
		}
		if job.DeployPhase == deployPhasePreArtifact && len(job.ArtifactOutputs) > 0 {
			return nil, fmt.Errorf("%s: %s jobs cannot reference artifacts", job.JobID, deployPhasePreArtifact)
		}
		byPhase[job.DeployPhase] = append(byPhase[job.DeployPhase], job)
	}
	return byPhase, nil
}

func ensureArtifactOriginAvailable(ctx context.Context, rt *runtime.Runtime, client *nomadclient.Client) error {
	ctx, span := rt.Tracer.Start(ctx, "verself_deploy.artifacts.origin_health")
	defer span.End()

	services, err := client.ListServiceAddresses(ctx)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	var garageS3 int
	for _, service := range services {
		if service.Name == "garage-s3" {
			garageS3++
		}
	}
	span.SetAttributes(attribute.Int("verself.artifact_origin.garage_s3_count", garageS3))
	if garageS3 == 0 {
		err := fmt.Errorf("artifact origin is unavailable: no healthy garage-s3 Nomad service registrations")
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	span.SetStatus(codes.Ok, "")
	return nil
}

func runResults(jobs []deploymodel.NomadJob) []jobRunResult {
	results := make([]jobRunResult, 0, len(jobs))
	for _, job := range jobs {
		results = append(results, jobRunResult{JobID: job.JobID})
	}
	return results
}

func artifactsForJobs(plan *deployPlan, jobs []deploymodel.NomadJob) ([]deploymodel.Artifact, error) {
	outputs := map[string]bool{}
	for _, job := range jobs {
		for _, output := range job.ArtifactOutputs {
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
		return nil, fmt.Errorf("jobs reference unknown artifacts: %s", strings.Join(missing, ", "))
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
