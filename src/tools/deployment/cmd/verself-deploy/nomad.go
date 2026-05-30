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
				if binding.Pre {
					return nil, fmt.Errorf("pre_artifact %q must be referenced through a task env placeholder, not a Nomad artifact stanza", output)
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
			for key, value := range task.Env {
				if !strings.HasPrefix(value, artifactSourcePrefix) {
					continue
				}
				output := strings.TrimPrefix(value, artifactSourcePrefix)
				binding, ok := bindings[output]
				if !ok {
					return nil, fmt.Errorf("artifact %q is referenced by authored spec but not declared by nomad_component", output)
				}
				task.Env[key] = binding.Artifact.Key
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
	intents := make([]jobApplyIntent, 0, len(plan.Jobs))
	for _, job := range plan.Jobs {
		intent, err := prepareNomadJob(ctx, rt, client, job)
		if err != nil {
			return applyResults(intents), fmt.Errorf("%s: %w", job.JobID, err)
		}
		intents = append(intents, intent)
	}
	preArtifacts, garageArtifacts, err := artifactsForChangedIntents(plan, intents)
	if err != nil {
		return applyResults(intents), err
	}
	if len(preArtifacts) > 0 {
		if err := publishPreArtifacts(ctx, rt, preArtifacts); err != nil {
			return applyResults(intents), err
		}
	}
	if err := submitNomadJobs(ctx, rt, client, plan, intents, garageArtifacts); err != nil {
		return applyResults(intents), err
	}
	results := applyResults(intents)
	return results, nil
}

func submitNomadJobs(ctx context.Context, rt *runtime.Runtime, client *nomadclient.Client, plan *deployPlan, intents []jobApplyIntent, garageArtifacts []deploymodel.Artifact) error {
	artifactOriginReady := len(garageArtifacts) == 0
	for _, intent := range intents {
		if !intent.Changed {
			continue
		}
		needsArtifactOrigin, err := intentNeedsArtifactOrigin(plan, intent)
		if err != nil {
			return err
		}
		if needsArtifactOrigin && !artifactOriginReady {
			if err := ensureArtifactOriginAvailable(ctx, rt, client, plan.SiteCfg.ArtifactDelivery.ArtifactDelivery); err != nil {
				return err
			}
			if err := publishArtifacts(ctx, rt, plan.SiteCfg.ArtifactDelivery.ArtifactDelivery, garageArtifacts); err != nil {
				return err
			}
			artifactOriginReady = true
		}
		if err := submitNomadJob(ctx, rt, client, plan, intent); err != nil {
			return fmt.Errorf("%s: %w", intent.Job.JobID, err)
		}
	}
	return nil
}

func prepareNomadJob(ctx context.Context, rt *runtime.Runtime, client *nomadclient.Client, job deploymodel.NomadJob) (jobApplyIntent, error) {
	ctx, span := rt.Tracer.Start(ctx, "verself_deploy.nomad.apply")
	defer span.End()
	span.SetAttributes(
		attribute.String("nomad.job_id", job.JobID),
		attribute.StringSlice("verself.artifact_outputs", job.ArtifactOutputs),
		attribute.String("verself.input_sha256", job.InputSHA256),
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
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return jobApplyIntent{}, err
	}
	recordNomadDecision(span, rt.Identity.RunKey(), rt.Site, job, decision, time.Since(decisionStarted))
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

func submitNomadJob(ctx context.Context, rt *runtime.Runtime, client *nomadclient.Client, plan *deployPlan, intent jobApplyIntent) error {
	ctx, span := rt.Tracer.Start(ctx, "verself_deploy.nomad.submit",
		trace.WithAttributes(
			attribute.String("nomad.job_id", intent.Job.JobID),
		),
	)
	defer span.End()
	submitStarted := time.Now()
	submitted, err := client.Submit(ctx, intent.Spec, intent.Decision.PriorJobModifyIndex)
	if err != nil {
		recordNomadSubmitFailed(span, rt.Identity.RunKey(), rt.Site, intent.Job, intent.Decision, time.Since(submitStarted), err)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	fmt.Printf("verself-deploy: %s submitted job_modify_index=%d eval_id=%s deployment_id=%s\n",
		submitted.JobID, submitted.JobModifyIndex, submitted.EvalID, submitted.DeploymentID)
	recordNomadSubmitted(span, rt.Identity.RunKey(), rt.Site, intent.Job, intent.Decision, submitted, time.Since(submitStarted))
	monitorStarted := time.Now()
	monitor, err := client.Monitor(ctx, submitted)
	if err != nil {
		recordNomadDeploymentFailed(span, rt.Identity.RunKey(), rt.Site, intent.Job, submitted, monitor, time.Since(monitorStarted), err)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	recordNomadDeploymentSucceeded(span, rt.Identity.RunKey(), rt.Site, intent.Job, submitted, monitor, time.Since(monitorStarted))
	span.SetAttributes(attribute.String("nomad.terminal_status", monitor.TerminalStatus))
	canaryStarted := time.Now()
	if err := runPostDeployCanaries(ctx, rt, rt.RepoRoot, plan.Site, plan.SHA, intent.Job, plan.PostDeployChecks); err != nil {
		recordPostDeployCanariesFailed(span, rt.Identity.RunKey(), rt.Site, intent.Job, plan.PostDeployChecks, time.Since(canaryStarted), err)
		span.RecordError(err)
		rollbackErr := rollbackNomadJob(ctx, rt, client, intent, err)
		if rollbackErr != nil {
			span.RecordError(rollbackErr)
			span.SetStatus(codes.Error, rollbackErr.Error())
			return fmt.Errorf("%w; rollback failed: %v", err, rollbackErr)
		}
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	recordPostDeployCanariesSucceeded(span, rt.Identity.RunKey(), rt.Site, intent.Job, plan.PostDeployChecks, time.Since(canaryStarted))
	fmt.Printf("verself-deploy: %s healthy\n", submitted.JobID)
	span.SetStatus(codes.Ok, "")
	return nil
}

func rollbackNomadJob(ctx context.Context, rt *runtime.Runtime, client *nomadclient.Client, intent jobApplyIntent, cause error) error {
	ctx, span := rt.Tracer.Start(ctx, "verself_deploy.nomad.rollback",
		trace.WithAttributes(
			attribute.String("nomad.job_id", intent.Job.JobID),
			attribute.Bool("nomad.prior_exists", intent.Decision.PriorExists),
			attribute.Int64("nomad.target_version", int64FromUint64(intent.Decision.PriorVersion, "prior job version")),
			attribute.String("error.message", truncateError(cause)),
		),
	)
	defer span.End()
	started := time.Now()
	currentVersion, err := client.CurrentJobVersion(ctx, intent.Job.JobID)
	if err != nil {
		recordNomadRollbackFailed(span, rt.Identity.RunKey(), rt.Site, intent.Job, time.Since(started), err)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}

	var sub *nomadclient.SubmitResult
	if !intent.Decision.PriorExists {
		sub, err = client.Deregister(ctx, intent.Job.JobID, intent.Spec.JobType())
		if err == nil {
			err = client.WaitStopped(ctx, intent.Job.JobID)
		}
	} else {
		sub, err = client.Revert(ctx, intent.Job.JobID, intent.Spec.JobType(), intent.Decision.PriorVersion, currentVersion)
	}
	if err != nil {
		recordNomadRollbackFailed(span, rt.Identity.RunKey(), rt.Site, intent.Job, time.Since(started), err)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	if !intent.Decision.PriorExists {
		monitor := nomadclient.MonitorResult{TerminalStatus: "deregistered"}
		recordNomadRollbackSucceeded(span, rt.Identity.RunKey(), rt.Site, intent.Job, sub, monitor, time.Since(started))
		span.SetStatus(codes.Ok, "")
		fmt.Printf("verself-deploy: %s deregistered after failed first-deploy canary\n", intent.Job.JobID)
		return nil
	}
	monitor, err := client.Monitor(ctx, sub)
	if err != nil {
		recordNomadRollbackFailed(span, rt.Identity.RunKey(), rt.Site, intent.Job, time.Since(started), err)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	recordNomadRollbackSucceeded(span, rt.Identity.RunKey(), rt.Site, intent.Job, sub, monitor, time.Since(started))
	span.SetStatus(codes.Ok, "")
	fmt.Printf("verself-deploy: %s rolled back after failed canary\n", intent.Job.JobID)
	return nil
}

func ensureArtifactOriginAvailable(ctx context.Context, rt *runtime.Runtime, client *nomadclient.Client, delivery deploymodel.ArtifactDelivery) error {
	ctx, span := rt.Tracer.Start(ctx, "verself_deploy.artifacts.origin_health")
	defer span.End()

	artifactOrigin, err := waitForArtifactOriginServices(ctx, client, 30*time.Second)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	span.SetAttributes(attribute.Int("verself.artifact_origin.service_count", artifactOrigin))
	loaded, err := nomadGetterCredentialsLoaded(ctx, rt, delivery.GetterCredentials)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	span.SetAttributes(attribute.Bool("verself.artifact_origin.nomad_credentials_loaded", loaded))
	if !loaded {
		if err := restartNomadForArtifactCredentials(ctx, rt); err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			return err
		}
		span.SetAttributes(attribute.Bool("verself.artifact_origin.nomad_restarted_for_credentials", true))
		if _, err := waitForArtifactOriginServices(ctx, client, 60*time.Second); err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			return err
		}
		loaded, err = nomadGetterCredentialsLoaded(ctx, rt, delivery.GetterCredentials)
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			return err
		}
		if !loaded {
			err := fmt.Errorf("nomad artifact getter credentials were not loaded from %s after restart", delivery.GetterCredentials.EnvironmentFile)
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			return err
		}
	}
	span.SetStatus(codes.Ok, "")
	return nil
}

func waitForArtifactOriginServices(ctx context.Context, client *nomadclient.Client, timeout time.Duration) (int, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	var lastErr error
	for {
		count, err := artifactOriginServiceCount(ctx, client)
		if err == nil && count > 0 {
			return count, nil
		}
		if err != nil {
			lastErr = err
		}
		select {
		case <-ctx.Done():
			if lastErr != nil {
				return 0, fmt.Errorf("artifact origin is unavailable: %w", lastErr)
			}
			return 0, fmt.Errorf("artifact origin is unavailable: no healthy artifact-origin Nomad service registrations")
		case <-ticker.C:
		}
	}
}

func artifactOriginServiceCount(ctx context.Context, client *nomadclient.Client) (int, error) {
	services, err := client.ListServiceAddresses(ctx)
	if err != nil {
		return 0, err
	}
	var count int
	for _, service := range services {
		if service.Name == "artifact-origin" {
			count++
		}
	}
	return count, nil
}

func nomadGetterCredentialsLoaded(ctx context.Context, rt *runtime.Runtime, creds deploymodel.Credentials) (bool, error) {
	if creds.EnvironmentFile == "" || creds.AccessKeyIDEnv == "" || creds.SecretAccessKeyEnv == "" {
		return false, fmt.Errorf("artifact getter credentials require environment_file, access_key_id_env, and secret_access_key_env")
	}
	if !validEnvironmentName(creds.AccessKeyIDEnv) || !validEnvironmentName(creds.SecretAccessKeyEnv) {
		return false, fmt.Errorf("artifact getter credential env vars must be shell-safe names")
	}
	script := `env_file="$1"
access="$2"
secret="$3"
if [ ! -s "$env_file" ]; then
  echo missing
  exit 0
fi
pid="$(systemctl show -p MainPID --value nomad || true)"
if [ -z "$pid" ] || [ "$pid" = "0" ]; then
  echo missing
  exit 0
fi
if tr '\000' '\n' < "/proc/$pid/environ" | grep -q "^$access=" &&
   tr '\000' '\n' < "/proc/$pid/environ" | grep -q "^$secret="; then
  echo loaded
else
  echo missing
fi`
	quotedScript, err := shellQuote(script)
	if err != nil {
		return false, err
	}
	quotedFile, err := shellQuote(creds.EnvironmentFile)
	if err != nil {
		return false, err
	}
	quotedAccess, err := shellQuote(creds.AccessKeyIDEnv)
	if err != nil {
		return false, err
	}
	quotedSecret, err := shellQuote(creds.SecretAccessKeyEnv)
	if err != nil {
		return false, err
	}
	out, err := rt.SSH.Exec(ctx, "sudo /bin/sh -eu -c "+quotedScript+" -- "+quotedFile+" "+quotedAccess+" "+quotedSecret)
	if err != nil {
		return false, fmt.Errorf("inspect Nomad artifact getter credentials: %w", err)
	}
	switch strings.TrimSpace(string(out)) {
	case "loaded":
		return true, nil
	case "missing":
		return false, nil
	default:
		return false, fmt.Errorf("unexpected Nomad artifact credential probe output")
	}
}

func restartNomadForArtifactCredentials(ctx context.Context, rt *runtime.Runtime) error {
	if _, err := rt.SSH.Exec(ctx, "sudo /bin/systemctl restart nomad"); err != nil {
		return fmt.Errorf("restart Nomad after artifact credentials were generated: %w", err)
	}
	return nil
}

func validEnvironmentName(name string) bool {
	if name == "" {
		return false
	}
	for idx, r := range name {
		if idx == 0 {
			if r != '_' && (r < 'A' || r > 'Z') && (r < 'a' || r > 'z') {
				return false
			}
			continue
		}
		if r != '_' && (r < 'A' || r > 'Z') && (r < 'a' || r > 'z') && (r < '0' || r > '9') {
			return false
		}
	}
	return true
}

func applyResults(intents []jobApplyIntent) []jobApplyResult {
	results := make([]jobApplyResult, 0, len(intents))
	for _, intent := range intents {
		results = append(results, jobApplyResult{JobID: intent.Job.JobID, Changed: intent.Changed})
	}
	return results
}

func artifactsForChangedIntents(plan *deployPlan, intents []jobApplyIntent) ([]deploymodel.Artifact, []deploymodel.Artifact, error) {
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
		return nil, nil, nil
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
		return nil, nil, fmt.Errorf("nomad jobs reference unknown artifacts: %s", strings.Join(missing, ", "))
	}
	preArtifacts, garageArtifacts := splitArtifactsByDelivery(artifacts)
	return preArtifacts, garageArtifacts, nil
}

func intentNeedsArtifactOrigin(plan *deployPlan, intent jobApplyIntent) (bool, error) {
	artifactByOutput := make(map[string]deploymodel.Artifact, len(plan.Artifacts))
	for _, artifact := range plan.Artifacts {
		artifactByOutput[artifact.Output] = artifact
	}
	for _, output := range intent.Job.ArtifactOutputs {
		artifact, ok := artifactByOutput[output]
		if !ok {
			return false, fmt.Errorf("%s references unknown artifact %q", intent.Job.JobID, output)
		}
		if !isPreArtifact(artifact) {
			return true, nil
		}
	}
	return false, nil
}

func splitArtifactsByDelivery(artifacts []deploymodel.Artifact) ([]deploymodel.Artifact, []deploymodel.Artifact) {
	preArtifacts := []deploymodel.Artifact{}
	garageArtifacts := []deploymodel.Artifact{}
	for _, artifact := range artifacts {
		if strings.HasPrefix(artifact.Key, preArtifactRemoteRoot+"/") {
			preArtifacts = append(preArtifacts, artifact)
			continue
		}
		garageArtifacts = append(garageArtifacts, artifact)
	}
	return preArtifacts, garageArtifacts
}

func isPreArtifact(artifact deploymodel.Artifact) bool {
	return strings.HasPrefix(artifact.Key, preArtifactRemoteRoot+"/")
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
