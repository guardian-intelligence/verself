package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"go.opentelemetry.io/otel/trace"

	"github.com/verself/distribution-service/internal/distribution"
	auth "github.com/verself/service-runtime/auth"
	workloadauth "github.com/verself/service-runtime/workload"
)

type Config struct {
	Service        *distribution.Service
	InstallationID string
	OriginClient   *http.Client
}

func RegisterPublicRoutes(mux *http.ServeMux, cfg Config) {
	api := publicAPI{cfg: cfg}
	mux.HandleFunc("POST /api/v1/distribution/updates:check", api.checkUpdate)
	mux.HandleFunc("POST /api/v1/distribution/upgrades:recordVerifiedDownload", api.recordUpgradeVerification)
	mux.HandleFunc("GET /api/v1/distribution/packages/{package_name}/channels/{channel_name}/resolve", api.resolveTarget)
	mux.HandleFunc("GET /api/v1/distribution/artifacts/{artifact_digest_ref}", api.getArtifact)
	mux.HandleFunc("GET /api/v1/distribution/packages/{package_name}/channels/{channel_name}/targets", api.listTargets)
	mux.HandleFunc("GET /v2/{name...}", api.registryRead)
}

func RegisterInternalRoutes(mux *http.ServeMux, cfg Config) {
	api := internalAPI{cfg: cfg}
	mux.HandleFunc("POST /internal/v1/distribution/artifacts:admit", api.admitArtifact)
	mux.HandleFunc("POST /internal/v1/distribution/packages/{package_name}/channels/{channel_name}/targets/promote", api.promoteTarget)
	mux.HandleFunc("POST /internal/v1/distribution/artifacts/{artifact_digest_ref}/quarantine", api.quarantineArtifact)
	mux.HandleFunc("POST /internal/v1/distribution/artifacts/{artifact_digest_ref}/replicate", api.ensureReplication)
	mux.HandleFunc("GET /internal/v1/distribution/artifacts/{artifact_digest_ref}", api.getArtifact)
}

type publicAPI struct {
	cfg Config
}

type internalAPI struct {
	cfg Config
}

func (a publicAPI) checkUpdate(w http.ResponseWriter, r *http.Request) {
	var body checkUpdateBody
	if !decodeJSON(w, r, &body) {
		return
	}
	target, available, err := a.cfg.Service.CheckUpdate(r.Context(), publicPrincipal(r.Context()), distribution.CheckUpdateRequest{
		PackageName:      body.PackageName,
		ChannelName:      body.ChannelName,
		PlatformOS:       body.PlatformOS,
		PlatformArch:     body.PlatformArch,
		InstalledDigest:  body.InstalledDigest,
		InstalledVersion: body.InstalledVersion,
	})
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"update_available": available, "target": targetDTO(a.cfg.InstallationID, target), "traceparent": traceparent(r.Context())})
}

func (a publicAPI) recordUpgradeVerification(w http.ResponseWriter, r *http.Request) {
	var body upgradeVerificationBody
	if !decodeJSON(w, r, &body) {
		return
	}
	if err := a.cfg.Service.RecordUpgradeDownloadVerified(r.Context(), publicPrincipal(r.Context()), distribution.UpgradeVerificationRequest{
		PackageName:      body.PackageName,
		ChannelName:      body.ChannelName,
		PlatformOS:       body.PlatformOS,
		PlatformArch:     body.PlatformArch,
		ArtifactDigest:   body.ArtifactDigest,
		LayerDigest:      body.LayerDigest,
		InstalledVersion: body.InstalledVersion,
	}); err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"traceparent": traceparent(r.Context())})
}

func (a publicAPI) resolveTarget(w http.ResponseWriter, r *http.Request) {
	target, err := a.cfg.Service.ResolveTarget(r.Context(), publicPrincipal(r.Context()), distribution.ResolveTargetRequest{
		PackageName:  r.PathValue("package_name"),
		ChannelName:  r.PathValue("channel_name"),
		PlatformOS:   r.URL.Query().Get("platform_os"),
		PlatformArch: r.URL.Query().Get("platform_arch"),
	})
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"target": targetDTO(a.cfg.InstallationID, target), "traceparent": traceparent(r.Context())})
}

func (a publicAPI) getArtifact(w http.ResponseWriter, r *http.Request) {
	artifact, err := a.cfg.Service.GetArtifactByRef(r.Context(), publicPrincipal(r.Context()), r.PathValue("artifact_digest_ref"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"artifact": artifactDTO(a.cfg.InstallationID, artifact), "traceparent": traceparent(r.Context())})
}

func (a publicAPI) listTargets(w http.ResponseWriter, r *http.Request) {
	limit := int32(100)
	if raw := strings.TrimSpace(r.URL.Query().Get("page_size")); raw != "" {
		parsed, err := strconv.ParseInt(raw, 10, 32)
		if err != nil {
			writeError(w, r, distribution.ErrInvalid)
			return
		}
		limit = int32(parsed)
	}
	targets, err := a.cfg.Service.ListTargets(r.Context(), publicPrincipal(r.Context()), r.PathValue("package_name"), r.PathValue("channel_name"), r.URL.Query().Get("platform_os"), r.URL.Query().Get("platform_arch"), limit)
	if err != nil {
		writeError(w, r, err)
		return
	}
	out := make([]targetRecord, 0, len(targets))
	for _, target := range targets {
		out = append(out, targetDTO(a.cfg.InstallationID, target))
	}
	writeJSON(w, http.StatusOK, map[string]any{"targets": out, "traceparent": traceparent(r.Context())})
}

func (a publicAPI) registryPing(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Docker-Distribution-API-Version", "registry/2.0")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("{}\n"))
}

func (a publicAPI) registryRead(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/v2/" {
		a.registryPing(w, r)
		return
	}
	repository, kind, reference, ok := parseRegistryPath(r.PathValue("name"))
	if !ok {
		writeProblem(w, r, http.StatusNotFound, "distribution.oci.not_found", "unsupported registry path")
		return
	}
	if kind == "manifests" && !strings.HasPrefix(reference, "sha256:") {
		writeProblem(w, r, http.StatusForbidden, "distribution.oci.mutable_reference_denied", "mutable OCI references must resolve through the distribution API")
		return
	}
	if !strings.HasPrefix(reference, "sha256:") {
		writeProblem(w, r, http.StatusBadRequest, "request.validation_failed", "digest reference is required")
		return
	}
	artifact, err := a.artifactForOCIRead(r.Context(), repository, kind, reference)
	if err != nil {
		writeError(w, r, err)
		return
	}
	if artifact.State == distribution.StateQuarantined {
		writeError(w, r, distribution.ErrQuarantined)
		return
	}
	if a.proxyOCI(w, r, artifact, kind, reference) {
		a.cfg.Service.RecordOCIServed(r.Context(), artifact, kind, reference)
	}
}

func (a publicAPI) artifactForOCIRead(ctx context.Context, repository string, kind string, reference string) (distribution.Artifact, error) {
	artifact, err := a.cfg.Service.Store.GetArtifactByRepositoryDigest(ctx, repository, reference)
	if err == nil {
		return artifact, nil
	}
	if kind != "blobs" || !errors.Is(err, distribution.ErrNotFound) {
		return distribution.Artifact{}, err
	}
	artifacts, err := a.cfg.Service.Store.ListArtifactsByRepository(ctx, repository)
	if err != nil {
		return distribution.Artifact{}, err
	}
	for _, candidate := range artifacts {
		allowed, err := a.manifestAllowsDescriptor(ctx, candidate, reference)
		if err != nil {
			return distribution.Artifact{}, err
		}
		if allowed {
			return candidate, nil
		}
	}
	return distribution.Artifact{}, distribution.ErrNotFound
}

func (a publicAPI) manifestAllowsDescriptor(ctx context.Context, artifact distribution.Artifact, digest string) (bool, error) {
	client := a.cfg.OriginClient
	if client == nil {
		client = http.DefaultClient
	}
	target := strings.TrimSuffix(artifact.OriginRegistryURL, "/") + "/v2/" + artifact.OCIRepository + "/manifests/" + artifact.OCIDigest
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return false, err
	}
	req.Header.Set("Accept", "application/vnd.oci.image.manifest.v1+json")
	resp, err := client.Do(req)
	if err != nil {
		return false, distribution.ErrRegistryUnavailable
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return false, distribution.ErrRegistryUnavailable
	}
	var manifest ociManifest
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&manifest); err != nil {
		return false, distribution.ErrRegistryUnavailable
	}
	if manifest.Config.Digest == digest {
		return true, nil
	}
	for _, layer := range manifest.Layers {
		if layer.Digest == digest {
			return true, nil
		}
	}
	return false, nil
}

func (a publicAPI) proxyOCI(w http.ResponseWriter, r *http.Request, artifact distribution.Artifact, kind string, reference string) bool {
	client := a.cfg.OriginClient
	if client == nil {
		client = http.DefaultClient
	}
	target := strings.TrimSuffix(artifact.OriginRegistryURL, "/") + "/v2/" + artifact.OCIRepository + "/" + kind + "/" + reference
	req, err := http.NewRequestWithContext(r.Context(), r.Method, target, nil)
	if err != nil {
		writeError(w, r, err)
		return false
	}
	req.Header.Set("Accept", r.Header.Get("Accept"))
	resp, err := client.Do(req)
	if err != nil {
		writeError(w, r, distribution.ErrRegistryUnavailable)
		return false
	}
	defer func() { _ = resp.Body.Close() }()
	copyHeaders(w.Header(), resp.Header)
	// Public OCI reads only serve digest-addressed content.
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.WriteHeader(resp.StatusCode)
	if r.Method != http.MethodHead {
		_, _ = io.Copy(w, resp.Body)
	}
	return resp.StatusCode >= 200 && resp.StatusCode < 300
}

type ociManifest struct {
	Config ociDescriptor   `json:"config"`
	Layers []ociDescriptor `json:"layers"`
}

type ociDescriptor struct {
	Digest string `json:"digest"`
}

func (a internalAPI) admitArtifact(w http.ResponseWriter, r *http.Request) {
	var body admitArtifactBody
	if !decodeJSON(w, r, &body) {
		return
	}
	artifact, err := a.cfg.Service.AdmitArtifact(r.Context(), internalPrincipal(r.Context()), distribution.AdmitArtifactRequest{
		PackageName:       body.PackageName,
		PackageVersion:    body.PackageVersion,
		ChannelName:       body.ChannelName,
		PlatformOS:        body.PlatformOS,
		PlatformArch:      body.PlatformArch,
		OriginRegistryURL: body.OriginRegistryURL,
		PublicRegistryURL: body.PublicRegistryURL,
		OCIRepository:     body.OCIRepository,
		OCIDigest:         body.OCIDigest,
		OCIMediaType:      body.OCIMediaType,
		OCISizeBytes:      body.OCISizeBytes,
		BuilderID:         body.BuilderID,
		SignerIdentity:    body.SignerIdentity,
		SourceRepository:  body.SourceRepository,
		SourceCommit:      body.SourceCommit,
		SourceRef:         body.SourceRef,
		BazelTarget:       body.BazelTarget,
		PolicyRef:         body.PolicyRef,
		Evidence:          evidenceFromDTO(body.Evidence),
		SubmittedBy:       body.SubmittedBy,
		IdempotencyKey:    r.Header.Get("Idempotency-Key"),
	})
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"artifact": artifactDTO(a.cfg.InstallationID, artifact), "traceparent": traceparent(r.Context())})
}

func (a internalAPI) promoteTarget(w http.ResponseWriter, r *http.Request) {
	var body promoteTargetBody
	if !decodeJSON(w, r, &body) {
		return
	}
	target, err := a.cfg.Service.PromoteTarget(r.Context(), internalPrincipal(r.Context()), distribution.PromoteTargetRequest{
		PackageName:    r.PathValue("package_name"),
		ChannelName:    r.PathValue("channel_name"),
		ArtifactDigest: body.ArtifactDigest,
		PlatformOS:     body.PlatformOS,
		PlatformArch:   body.PlatformArch,
		PolicyRef:      body.PolicyRef,
		PromotedBy:     body.PromotedBy,
		Reason:         body.Reason,
		IdempotencyKey: r.Header.Get("Idempotency-Key"),
	})
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"target": targetDTO(a.cfg.InstallationID, target), "traceparent": traceparent(r.Context())})
}

func (a internalAPI) quarantineArtifact(w http.ResponseWriter, r *http.Request) {
	var body actorReasonBody
	if !decodeJSON(w, r, &body) {
		return
	}
	artifact, err := a.cfg.Service.QuarantineArtifact(r.Context(), internalPrincipal(r.Context()), distribution.QuarantineArtifactRequest{ArtifactDigestRef: r.PathValue("artifact_digest_ref"), ActorID: body.ActorID, Reason: body.Reason, IdempotencyKey: r.Header.Get("Idempotency-Key")})
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"artifact": artifactDTO(a.cfg.InstallationID, artifact), "traceparent": traceparent(r.Context())})
}

func (a internalAPI) ensureReplication(w http.ResponseWriter, r *http.Request) {
	var body actorReasonBody
	if !decodeJSON(w, r, &body) {
		return
	}
	artifact, err := a.cfg.Service.EnsureReplication(r.Context(), internalPrincipal(r.Context()), distribution.EnsureReplicationRequest{ArtifactDigestRef: r.PathValue("artifact_digest_ref"), ActorID: body.ActorID, Reason: body.Reason, IdempotencyKey: r.Header.Get("Idempotency-Key")})
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"artifact": artifactDTO(a.cfg.InstallationID, artifact), "traceparent": traceparent(r.Context())})
}

func (a internalAPI) getArtifact(w http.ResponseWriter, r *http.Request) {
	artifact, err := a.cfg.Service.GetArtifactByRef(r.Context(), internalPrincipal(r.Context()), r.PathValue("artifact_digest_ref"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"artifact": artifactDTO(a.cfg.InstallationID, artifact), "traceparent": traceparent(r.Context())})
}

func publicPrincipal(ctx context.Context) distribution.Principal {
	identity := auth.FromContext(ctx)
	if identity == nil {
		return distribution.Principal{Actor: "anonymous"}
	}
	return distribution.Principal{Actor: identity.Subject}
}

func internalPrincipal(ctx context.Context) distribution.Principal {
	peerID, ok := workloadauth.PeerIDFromContext(ctx)
	if !ok {
		return distribution.Principal{Actor: "unknown-workload"}
	}
	return distribution.Principal{Actor: peerID.String()}
}

func decodeJSON(w http.ResponseWriter, r *http.Request, out any) bool {
	defer func() { _ = r.Body.Close() }()
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(out); err != nil {
		writeProblem(w, r, http.StatusBadRequest, "request.validation_failed", err.Error())
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, distribution.ErrInvalid):
		writeProblem(w, r, http.StatusBadRequest, "request.validation_failed", err.Error())
	case errors.Is(err, distribution.ErrNotFound):
		writeProblem(w, r, http.StatusNotFound, "resource.not_found", err.Error())
	case errors.Is(err, distribution.ErrIdempotencyMismatch):
		writeProblem(w, r, http.StatusConflict, "conflict.idempotency_payload_mismatch", err.Error())
	case errors.Is(err, distribution.ErrConflict):
		writeProblem(w, r, http.StatusConflict, "conflict.state", err.Error())
	case errors.Is(err, distribution.ErrMissingReferrers):
		writeProblem(w, r, http.StatusConflict, "distribution.missing_referrers", err.Error())
	case errors.Is(err, distribution.ErrDigestMismatch):
		writeProblem(w, r, http.StatusConflict, "distribution.digest_mismatch", err.Error())
	case errors.Is(err, distribution.ErrUntrustedBuilder):
		writeProblem(w, r, http.StatusForbidden, "distribution.untrusted_builder", err.Error())
	case errors.Is(err, distribution.ErrUntrustedSigner):
		writeProblem(w, r, http.StatusForbidden, "distribution.untrusted_signer", err.Error())
	case errors.Is(err, distribution.ErrSourcePolicyFailure):
		writeProblem(w, r, http.StatusForbidden, "distribution.source_policy_failure", err.Error())
	case errors.Is(err, distribution.ErrChannelPolicyFailure):
		writeProblem(w, r, http.StatusForbidden, "distribution.channel_policy_failure", err.Error())
	case errors.Is(err, distribution.ErrQuarantined):
		writeProblem(w, r, http.StatusForbidden, "distribution.artifact_quarantined", err.Error())
	case errors.Is(err, distribution.ErrReplication):
		writeProblem(w, r, http.StatusServiceUnavailable, "distribution.replication_failure", err.Error())
	default:
		writeProblem(w, r, http.StatusServiceUnavailable, "service.unavailable", err.Error())
	}
}

func writeProblem(w http.ResponseWriter, r *http.Request, status int, code string, detail string) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(problemDetails{
		Type:        problemType(code),
		Title:       http.StatusText(status),
		Status:      status,
		Detail:      detail,
		Code:        code,
		Traceparent: traceparent(r.Context()),
	})
}

func problemType(code string) string {
	return "urn:verself:problem:" + strings.ReplaceAll(code, ".", ":")
}

func traceparent(ctx context.Context) string {
	spanContext := trace.SpanContextFromContext(ctx)
	if !spanContext.IsValid() {
		return ""
	}
	return "00-" + spanContext.TraceID().String() + "-" + spanContext.SpanID().String() + "-" + spanContext.TraceFlags().String()
}

func parseRegistryPath(path string) (repository string, kind string, reference string, ok bool) {
	for _, marker := range []string{"/manifests/", "/blobs/", "/referrers/"} {
		if before, after, found := strings.Cut(path, marker); found && before != "" && after != "" {
			return before, strings.Trim(marker, "/"), after, true
		}
	}
	return "", "", "", false
}

func copyHeaders(dst http.Header, src http.Header) {
	for key, values := range src {
		if strings.EqualFold(key, "Connection") || strings.EqualFold(key, "Transfer-Encoding") {
			continue
		}
		for _, value := range values {
			dst.Add(key, value)
		}
	}
}

type problemDetails struct {
	Type        string `json:"type"`
	Title       string `json:"title"`
	Status      int    `json:"status"`
	Detail      string `json:"detail,omitempty"`
	Code        string `json:"code"`
	Traceparent string `json:"traceparent,omitempty"`
}
