package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/hashicorp/nomad/api"
	ocidigest "github.com/opencontainers/go-digest"
	"github.com/spiffe/go-spiffe/v2/workloadapi"
	distributioninternalclient "github.com/verself/distribution-service/internalclient"
	workloadauth "github.com/verself/service-runtime/workload"
	"go.opentelemetry.io/otel/trace"
)

const workloadOCIAttestationInsert = "INSERT INTO verself.workload_oci_attestations"

const (
	workloadOCIStateAvailable          = "available"
	workloadOCIDistributionAllowed     = "allowed"
	workloadOCISourceRepository        = "guardian-intelligence/verself"
	workloadOCIEvidenceSLSA            = "slsa_provenance"
	workloadOCIPredicateSLSAProvenance = "https://slsa.dev/provenance/v1"
)

type WorkloadOCIAttestationRow struct {
	Site                             string    `ch:"site"`
	NomadNamespace                   string    `ch:"nomad_namespace"`
	NomadJobID                       string    `ch:"nomad_job_id"`
	NomadGroup                       string    `ch:"nomad_group"`
	NomadTask                        string    `ch:"nomad_task"`
	AllocID                          string    `ch:"alloc_id"`
	NodeID                           string    `ch:"node_id"`
	NodeName                         string    `ch:"node_name"`
	ImageRef                         string    `ch:"image_ref"`
	DeclaredDigest                   string    `ch:"declared_digest"`
	MeasuredDigest                   string    `ch:"measured_digest"`
	DistributionArtifactDigest       string    `ch:"distribution_artifact_digest"`
	DistributionArtifactState        string    `ch:"distribution_artifact_state"`
	DistributionOCIRepository        string    `ch:"distribution_oci_repository"`
	DistributionPublicOCIReference   string    `ch:"distribution_public_oci_reference"`
	DistributionVerificationDecision string    `ch:"distribution_verification_decision"`
	DistributionVerificationReason   string    `ch:"distribution_verification_reason"`
	DistributionPolicyRef            string    `ch:"distribution_policy_ref"`
	DistributionBuilderID            string    `ch:"distribution_builder_id"`
	DistributionSignerIdentity       string    `ch:"distribution_signer_identity"`
	DistributionSourceRepository     string    `ch:"distribution_source_repository"`
	DistributionSourceCommit         string    `ch:"distribution_source_commit"`
	DistributionSourceRef            string    `ch:"distribution_source_ref"`
	SLSAPredicateType                string    `ch:"slsa_predicate_type"`
	SLSASubjectDigest                string    `ch:"slsa_subject_digest"`
	SLSADocumentDigest               string    `ch:"slsa_document_digest"`
	SLSAOCIReferrerDigest            string    `ch:"slsa_oci_referrer_digest"`
	SourceCommit                     string    `ch:"source_commit"`
	DeployRunKey                     string    `ch:"deploy_run_key"`
	SpecSHA256                       string    `ch:"spec_sha256"`
	ArtifactSHA256                   string    `ch:"artifact_sha256"`
	AllocClientStatus                string    `ch:"alloc_client_status"`
	TaskState                        string    `ch:"task_state"`
	Decision                         string    `ch:"decision"`
	MeasurementSource                string    `ch:"measurement_source"`
	Reason                           string    `ch:"reason"`
	AllocModifyIndex                 uint64    `ch:"alloc_modify_index"`
	TraceID                          string    `ch:"trace_id"`
	SpanID                           string    `ch:"span_id"`
	ObservedAt                       time.Time `ch:"observed_at"`
}

func (o *observer) recordWorkloadOCIEvidence(ctx context.Context, alloc *api.Allocation, meta deployMeta) {
	if o.ch == nil || alloc == nil {
		return
	}
	recordCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	detailedAlloc, err := o.allocationWithJob(recordCtx, alloc)
	if err != nil {
		o.logger.WarnContext(recordCtx, "nomad.oci_attestation.alloc_lookup_failed",
			slog.String("nomad.namespace", normalizeNamespace(alloc.Namespace, o.cfg.namespace)),
			slog.String("nomad.alloc_id", alloc.ID),
			slog.String("nomad.job_id", alloc.JobID),
			slog.String("error", err.Error()),
		)
		return
	}
	if meta.empty() {
		meta = metadataFromAlloc(detailedAlloc)
	}
	rows := workloadOCIAttestationRows(recordCtx, o.cfg.site, normalizeNamespace(detailedAlloc.Namespace, o.cfg.namespace), detailedAlloc, meta, time.Now().UTC(), trace.SpanContextFromContext(ctx), o.measureWorkloadOCIRuntime, o.resolve)
	if len(rows) == 0 {
		return
	}
	if err := appendWorkloadOCIAttestations(recordCtx, o.ch, rows); err != nil {
		o.logger.WarnContext(recordCtx, "nomad.oci_attestation.insert_failed",
			slog.String("nomad.namespace", normalizeNamespace(detailedAlloc.Namespace, o.cfg.namespace)),
			slog.String("nomad.alloc_id", detailedAlloc.ID),
			slog.String("nomad.job_id", detailedAlloc.JobID),
			slog.Int("rows", len(rows)),
			slog.String("error", err.Error()),
		)
		return
	}
	o.logger.InfoContext(recordCtx, "nomad.oci_attestation.inserted",
		slog.String("nomad.namespace", normalizeNamespace(detailedAlloc.Namespace, o.cfg.namespace)),
		slog.String("nomad.alloc_id", detailedAlloc.ID),
		slog.String("nomad.job_id", detailedAlloc.JobID),
		slog.Int("rows", len(rows)),
	)
}

func (o *observer) allocationWithJob(ctx context.Context, alloc *api.Allocation) (*api.Allocation, error) {
	if alloc.Job != nil {
		return alloc, nil
	}
	namespace := normalizeNamespace(alloc.Namespace, o.cfg.namespace)
	detailedAlloc, _, err := o.client.Allocations().Info(alloc.ID, (&api.QueryOptions{Namespace: namespace}).WithContext(ctx))
	if err != nil {
		return nil, err
	}
	if detailedAlloc == nil {
		return nil, fmt.Errorf("allocation %s not found", alloc.ID)
	}
	if detailedAlloc.Job == nil {
		return nil, fmt.Errorf("allocation %s has no job payload", alloc.ID)
	}
	return detailedAlloc, nil
}

type workloadOCIRuntimeMeasurer func(context.Context, *api.Allocation, allocationOCITask) workloadOCIRuntimeMeasurement

type workloadOCIRuntimeMeasurement struct {
	digest string
	source string
	reason string
}

type workloadOCIArtifactResolver func(context.Context, string) workloadOCIArtifactEvidence

type workloadOCIArtifactEvidence struct {
	digest                string
	state                 string
	repository            string
	publicRef             string
	policyRef             string
	verificationDecision  string
	verificationReason    string
	builderID             string
	signerIdentity        string
	sourceRepository      string
	sourceCommit          string
	sourceRef             string
	slsaPredicateType     string
	slsaSubjectDigest     string
	slsaDocumentDigest    string
	slsaOCIReferrerDigest string
	reason                string
}

func workloadOCIAttestationRows(ctx context.Context, site, namespace string, alloc *api.Allocation, meta deployMeta, observedAt time.Time, spanContext trace.SpanContext, measure workloadOCIRuntimeMeasurer, resolve workloadOCIArtifactResolver) []WorkloadOCIAttestationRow {
	if alloc == nil || alloc.Job == nil {
		return nil
	}
	tasks := allocationOCITasks(alloc)
	if len(tasks) == 0 {
		return nil
	}
	rows := make([]WorkloadOCIAttestationRow, 0, len(tasks))
	for _, task := range tasks {
		declaredDigest, digestErr := imageDigest(task.imageRef)
		measurement := workloadOCIRuntimeMeasurement{
			source: "podman_measurement_unconfigured",
			reason: "runtime container digest measurement is not configured",
		}
		if measure != nil {
			measurement = measure(ctx, alloc, task)
		}
		if strings.TrimSpace(measurement.source) == "" {
			measurement.source = "podman_measurement_unavailable"
		}
		artifact := workloadOCIArtifactEvidence{}
		decision, reason := workloadOCIDecision(declaredDigest, measurement.digest)
		if digestErr != nil {
			reason = digestErr.Error()
		} else if measurement.reason != "" && decision == "unmeasured" {
			reason = measurement.reason
		} else if decision == "matched" {
			artifact = resolveWorkloadOCIArtifact(ctx, declaredDigest, resolve)
			copyWorkloadOCIArtifactDecision(declaredDigest, meta.DeploySHA, artifact, &decision, &reason)
		}
		taskState := ""
		if state := alloc.TaskStates[task.name]; state != nil {
			taskState = state.State
		}
		row := WorkloadOCIAttestationRow{
			Site:                             site,
			NomadNamespace:                   namespace,
			NomadJobID:                       alloc.JobID,
			NomadGroup:                       alloc.TaskGroup,
			NomadTask:                        task.name,
			AllocID:                          alloc.ID,
			NodeID:                           alloc.NodeID,
			NodeName:                         alloc.NodeName,
			ImageRef:                         task.imageRef,
			DeclaredDigest:                   declaredDigest,
			MeasuredDigest:                   measurement.digest,
			DistributionArtifactDigest:       artifact.digest,
			DistributionArtifactState:        artifact.state,
			DistributionOCIRepository:        artifact.repository,
			DistributionPublicOCIReference:   artifact.publicRef,
			DistributionVerificationDecision: artifact.verificationDecision,
			DistributionVerificationReason:   artifact.verificationReason,
			DistributionPolicyRef:            artifact.policyRef,
			DistributionBuilderID:            artifact.builderID,
			DistributionSignerIdentity:       artifact.signerIdentity,
			DistributionSourceRepository:     artifact.sourceRepository,
			DistributionSourceCommit:         artifact.sourceCommit,
			DistributionSourceRef:            artifact.sourceRef,
			SLSAPredicateType:                artifact.slsaPredicateType,
			SLSASubjectDigest:                artifact.slsaSubjectDigest,
			SLSADocumentDigest:               artifact.slsaDocumentDigest,
			SLSAOCIReferrerDigest:            artifact.slsaOCIReferrerDigest,
			SourceCommit:                     meta.DeploySHA,
			DeployRunKey:                     meta.DeployRunKey,
			SpecSHA256:                       meta.SpecSHA256,
			ArtifactSHA256:                   meta.ArtifactSHA256,
			AllocClientStatus:                alloc.ClientStatus,
			TaskState:                        taskState,
			Decision:                         decision,
			MeasurementSource:                measurement.source,
			Reason:                           reason,
			AllocModifyIndex:                 alloc.ModifyIndex,
			ObservedAt:                       observedAt,
		}
		if spanContext.IsValid() {
			row.TraceID = spanContext.TraceID().String()
			row.SpanID = spanContext.SpanID().String()
		}
		rows = append(rows, row)
	}
	return rows
}

func resolveWorkloadOCIArtifact(ctx context.Context, declaredDigest string, resolve workloadOCIArtifactResolver) workloadOCIArtifactEvidence {
	if resolve == nil {
		return workloadOCIArtifactEvidence{reason: "distribution artifact resolver is not configured"}
	}
	artifact := resolve(ctx, declaredDigest)
	artifact.reason = strings.TrimSpace(artifact.reason)
	return artifact
}

func copyWorkloadOCIArtifactDecision(declaredDigest string, expectedCommit string, artifact workloadOCIArtifactEvidence, decision *string, reason *string) {
	switch {
	case artifact.digest == "":
		*decision = "unadmitted"
		*reason = firstNonEmpty(artifact.reason, "distribution artifact was not found")
	case artifact.digest != "":
		*decision, *reason = workloadOCIDistributionDecision(declaredDigest, expectedCommit, artifact)
	}
}

func workloadOCIDistributionDecision(declaredDigest string, expectedCommit string, artifact workloadOCIArtifactEvidence) (string, string) {
	switch {
	case artifact.digest == "":
		return "unadmitted", firstNonEmpty(artifact.reason, "distribution artifact was not found")
	case artifact.digest != declaredDigest:
		return "distribution_digest_mismatch", "distribution artifact digest differs from declared image digest"
	case artifact.state != workloadOCIStateAvailable:
		return "unavailable", "distribution artifact state is " + firstNonEmpty(artifact.state, "unknown")
	case artifact.verificationDecision != workloadOCIDistributionAllowed:
		return "unverified", "distribution verification decision is " + firstNonEmpty(artifact.verificationDecision, "unknown")
	case artifact.sourceRepository != workloadOCISourceRepository:
		return "source_repository_mismatch", "distribution source repository differs from expected repository"
	case artifact.sourceCommit == "":
		return "source_commit_missing", "distribution source commit is missing"
	case expectedCommit == "":
		return "source_commit_missing", "nomad deploy source commit is missing"
	case artifact.sourceCommit != expectedCommit:
		return "source_commit_mismatch", "distribution source commit differs from nomad deploy source commit"
	case artifact.slsaPredicateType == "":
		return "slsa_provenance_missing", "distribution SLSA provenance evidence is missing"
	case artifact.slsaSubjectDigest == "":
		return "slsa_subject_missing", "distribution SLSA provenance subject digest is missing"
	case artifact.slsaSubjectDigest != artifact.digest:
		return "slsa_subject_mismatch", "distribution SLSA provenance subject digest differs from artifact digest"
	default:
		return "verified", ""
	}
}

type distributionArtifactResolver struct {
	client *distributioninternalclient.Client
}

func newDistributionArtifactResolver(source *workloadapi.X509Source) (workloadOCIArtifactResolver, error) {
	if source == nil {
		return nil, errors.New("spiffe source is required")
	}
	httpClient, err := workloadauth.MTLSClientForServiceWithTimeouts(source, workloadauth.ServiceDistribution, nil, workloadauth.ServiceClientTimeouts{
		Dial:           500 * time.Millisecond,
		TLSHandshake:   time.Second,
		ResponseHeader: 5 * time.Second,
		Total:          10 * time.Second,
	})
	if err != nil {
		return nil, fmt.Errorf("distribution mtls client: %w", err)
	}
	client, err := distributioninternalclient.NewClient(workloadauth.InternalURL(workloadauth.ServiceDistribution), distributioninternalclient.WithHTTPClient(httpClient))
	if err != nil {
		return nil, err
	}
	return distributionArtifactResolver{client: client}.Resolve, nil
}

func (r distributionArtifactResolver) Resolve(ctx context.Context, digest string) workloadOCIArtifactEvidence {
	if r.client == nil {
		return workloadOCIArtifactEvidence{reason: "distribution client is not configured"}
	}
	resp, err := r.client.GetArtifact(ctx, workloadOCIDigestRef(digest))
	if err != nil {
		return workloadOCIArtifactEvidence{reason: "distribution artifact lookup failed: " + err.Error()}
	}
	if resp.Problem != nil {
		return workloadOCIArtifactEvidence{reason: "distribution artifact lookup rejected: " + distributionProblemDetail(resp.Problem)}
	}
	if resp.Result == nil {
		return workloadOCIArtifactEvidence{reason: "distribution artifact lookup returned no body"}
	}
	return workloadOCIArtifactEvidenceFromRecord(resp.Result.Artifact)
}

func workloadOCIArtifactEvidenceFromRecord(artifact distributioninternalclient.ArtifactRecord) workloadOCIArtifactEvidence {
	evidence := workloadOCIArtifactEvidence{
		digest:               artifact.OCIDigest,
		state:                artifact.State,
		repository:           artifact.OCIRepository,
		publicRef:            artifact.PublicOCIReference,
		policyRef:            artifact.PolicyRef,
		verificationDecision: artifact.Verification.Decision,
		verificationReason:   artifact.Verification.Reason,
		builderID:            artifact.Verification.BuilderID,
		signerIdentity:       artifact.Verification.SignerIdentity,
		sourceRepository:     artifact.Verification.SourceRepository,
		sourceCommit:         artifact.Verification.SourceCommit,
		sourceRef:            artifact.Verification.SourceRef,
	}
	for _, item := range artifact.Verification.Evidence {
		if item.EvidenceKind != workloadOCIEvidenceSLSA && item.PredicateType != workloadOCIPredicateSLSAProvenance {
			continue
		}
		evidence.slsaPredicateType = item.PredicateType
		evidence.slsaSubjectDigest = item.SubjectDigest
		evidence.slsaDocumentDigest = item.DocumentDigest
		evidence.slsaOCIReferrerDigest = item.OCIReferrerDigest
		break
	}
	return evidence
}

func workloadOCIDigestRef(digest string) string {
	return strings.Replace(strings.TrimSpace(digest), ":", "-", 1)
}

func distributionProblemDetail(problem *distributioninternalclient.ErrorModel) string {
	if problem == nil {
		return "unknown distribution problem"
	}
	for _, value := range []*string{problem.Code, problem.Detail, problem.Title} {
		if value != nil && strings.TrimSpace(*value) != "" {
			return strings.TrimSpace(*value)
		}
	}
	if problem.Status != nil {
		if statusText := http.StatusText(int(*problem.Status)); statusText != "" {
			return statusText
		}
	}
	return "unknown distribution problem"
}

type allocationOCITask struct {
	name     string
	imageRef string
}

func allocationOCITasks(alloc *api.Allocation) []allocationOCITask {
	taskGroup := taskGroupForAllocation(alloc)
	if taskGroup == nil {
		return nil
	}
	tasks := make([]allocationOCITask, 0, len(taskGroup.Tasks))
	for _, task := range taskGroup.Tasks {
		if task == nil || task.Driver != "podman" {
			continue
		}
		imageRef, ok := task.Config["image"].(string)
		if !ok || strings.TrimSpace(imageRef) == "" {
			continue
		}
		tasks = append(tasks, allocationOCITask{name: task.Name, imageRef: strings.TrimSpace(imageRef)})
	}
	return tasks
}

func taskGroupForAllocation(alloc *api.Allocation) *api.TaskGroup {
	if alloc == nil || alloc.Job == nil {
		return nil
	}
	for _, group := range alloc.Job.TaskGroups {
		if group == nil {
			continue
		}
		if ptrValue(group.Name) == alloc.TaskGroup {
			return group
		}
	}
	return nil
}

func imageDigest(imageRef string) (string, error) {
	before, after, ok := strings.Cut(strings.TrimSpace(imageRef), "@")
	if !ok || before == "" || after == "" {
		return "", errors.New("image reference is not pinned by digest")
	}
	parsed, err := ocidigest.Parse(after)
	if err != nil {
		return "", fmt.Errorf("image reference digest is invalid: %w", err)
	}
	return parsed.String(), nil
}

func workloadOCIDecision(declaredDigest, measuredDigest string) (string, string) {
	if declaredDigest == "" {
		return "declared_digest_missing", "image reference is not pinned by digest"
	}
	if measuredDigest == "" {
		return "unmeasured", "runtime container digest measurement is not available"
	}
	if declaredDigest == measuredDigest {
		return "matched", ""
	}
	return "mismatched", "measured runtime digest differs from declared digest"
}

func appendWorkloadOCIAttestations(ctx context.Context, ch clickhouse.Conn, rows []WorkloadOCIAttestationRow) error {
	batch, err := ch.PrepareBatch(ctx, workloadOCIAttestationInsert)
	if err != nil {
		return err
	}
	for i := range rows {
		if err := batch.AppendStruct(&rows[i]); err != nil {
			_ = batch.Abort()
			return err
		}
	}
	return batch.Send()
}
