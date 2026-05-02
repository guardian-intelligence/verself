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
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/verself/deployment-tools/internal/deploydb"
	"github.com/verself/deployment-tools/internal/identity"
	"github.com/verself/deployment-tools/internal/runtime"
)

const (
	ociImageManifestMediaType   = "application/vnd.oci.image.manifest.v1+json"
	artifactConfigMediaType     = "application/vnd.verself.artifact.admission.config.v1+json"
	artifactBytesMediaType      = "application/vnd.verself.artifact.bytes.v1"
	artifactSignatureMediaType  = "application/vnd.verself.artifact.signature.v1"
	artifactScannerMediaType    = "application/vnd.verself.artifact.scanner-result.v1+json"
	artifactSBOMMediaType       = "application/vnd.cyclonedx+json"
	artifactProvenanceMediaType = "application/vnd.in-toto+json"
)

type ociDescriptor struct {
	MediaType   string            `json:"mediaType"`
	Digest      string            `json:"digest"`
	Size        int64             `json:"size"`
	Annotations map[string]string `json:"annotations,omitempty"`
}

type ociManifest struct {
	SchemaVersion int               `json:"schemaVersion"`
	MediaType     string            `json:"mediaType"`
	Config        ociDescriptor     `json:"config"`
	Layers        []ociDescriptor   `json:"layers"`
	Annotations   map[string]string `json:"annotations,omitempty"`
}

type artifactAdmissionConfig struct {
	Artifact              string `json:"artifact"`
	SourcePath            string `json:"source_path"`
	SourceKind            string `json:"source_kind"`
	Surface               string `json:"surface"`
	UpstreamURL           string `json:"upstream_url"`
	UpstreamDigest        string `json:"upstream_digest"`
	ReleasedAt            string `json:"released_at"`
	ObservedAt            string `json:"observed_at"`
	MinimumAgeResult      string `json:"minimum_age_result"`
	ScannerName           string `json:"scanner_name"`
	ScannerVersion        string `json:"scanner_version"`
	ScannerDatabaseDigest string `json:"scanner_database_digest"`
	ScannerResultDigest   string `json:"scanner_result_digest"`
	SBOMDigest            string `json:"sbom_digest"`
	ProvenanceDigest      string `json:"provenance_digest"`
	SignatureDigest       string `json:"signature_digest"`
	AttestationDigest     string `json:"attestation_digest"`
	GUACSubject           string `json:"guac_subject"`
}

type artifactAdmissionOutput struct {
	Admission artifactAdmissionMetadata `json:"admission"`
}

type artifactAdmissionMetadata struct {
	State                 string `json:"state"`
	UpstreamURL           string `json:"upstream_url"`
	Digest                string `json:"digest"`
	ReleasedAt            string `json:"released_at"`
	ObservedAt            string `json:"observed_at"`
	MinimumAgeResult      string `json:"minimum_age_result"`
	ScannerResults        string `json:"scanner_results"`
	ScannerName           string `json:"scanner_name"`
	ScannerVersion        string `json:"scanner_version"`
	ScannerDatabaseDigest string `json:"scanner_database_digest"`
	SBOMURI               string `json:"sbom_uri"`
	SBOMDigest            string `json:"sbom_digest"`
	ProvenanceURI         string `json:"provenance_uri"`
	ProvenanceDigest      string `json:"provenance_digest"`
	TUFTargetPath         string `json:"tuf_target_path"`
	StorageURI            string `json:"storage_uri"`
	OCIRepository         string `json:"oci_repository"`
	OCIManifestDigest     string `json:"oci_manifest_digest"`
	OCIMediaType          string `json:"oci_media_type"`
	SignatureDigest       string `json:"signature_digest"`
	AttestationDigest     string `json:"attestation_digest"`
	ScannerResultDigest   string `json:"scanner_result_digest"`
	GUACSubject           string `json:"guac_subject"`
}

type blobContent struct {
	MediaType   string
	Digest      string
	Size        int64
	Open        func() (io.ReadCloser, error)
	Annotations map[string]string
}

func runArtifacts(args []string) int {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "verself-deploy artifacts: missing subcommand (try `admit-url` or `assert-evidence`)")
		return 2
	}
	switch args[0] {
	case "admit-url":
		return runArtifactsAdmitURL(args[1:])
	case "verify-install":
		return runArtifactsVerifyInstall(args[1:])
	case "assert-evidence":
		return runArtifactsAssertEvidence(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "verself-deploy artifacts: unknown subcommand: %s\n", args[0])
		return 2
	}
}

func runArtifactsAdmitURL(args []string) int {
	fs := flag.NewFlagSet("artifacts admit-url", flag.ContinueOnError)
	site := fs.String("site", "prod", "deployment site")
	artifact := fs.String("artifact", "", "artifact name")
	sourcePath := fs.String("source-path", "", "repo source path that requested the artifact")
	sourceKind := fs.String("source-kind", "", "artifact source kind")
	surface := fs.String("surface", "", "artifact surface")
	upstreamURL := fs.String("upstream-url", "", "upstream URL to fetch")
	digest := fs.String("digest", "", "expected upstream digest, formatted sha256:<hex>")
	releasedAtRaw := fs.String("released-at", "", "upstream release time (RFC3339)")
	observedAtRaw := fs.String("observed-at", "", "admission observation time (RFC3339, defaults to now)")
	minimumAgeDays := fs.Int("minimum-age-days", 3, "minimum release age in whole days")
	scannerResultPath := fs.String("scanner-result", "", "scanner result JSON path")
	scannerName := fs.String("scanner-name", "", "scanner name")
	scannerVersion := fs.String("scanner-version", "", "scanner version")
	scannerDatabaseDigest := fs.String("scanner-database-digest", "", "scanner database digest")
	signaturePath := fs.String("signature", "", "signature evidence path")
	provenancePath := fs.String("provenance", "", "SLSA/in-toto provenance or attestation path")
	zotURL := fs.String("zot-url", "http://127.0.0.1:5080", "zot registry base URL")
	zotRepository := fs.String("zot-repository", "", "zot repository path (defaults to admitted/<artifact>)")
	zotUsername := fs.String("zot-username", "artifact-publisher", "zot publisher username")
	zotPasswordFile := fs.String("zot-password-file", "/etc/zot/publisher-password", "zot publisher password file")
	tufTargetPath := fs.String("tuf-target-path", "", "logical TUF target path")
	guacSubject := fs.String("guac-subject", "", "GUAC subject identifier")
	repoRoot := fs.String("repo-root", "", "verself-sh checkout root (defaults to cwd)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	rr, ok := resolveRepoRoot("verself-deploy artifacts admit-url", *repoRoot)
	if !ok {
		return 1
	}
	req := artifactAdmissionRequest{
		Site:                  *site,
		Artifact:              *artifact,
		SourcePath:            *sourcePath,
		SourceKind:            *sourceKind,
		Surface:               *surface,
		UpstreamURL:           *upstreamURL,
		ExpectedDigest:        *digest,
		ReleasedAtRaw:         *releasedAtRaw,
		ObservedAtRaw:         *observedAtRaw,
		MinimumAgeDays:        *minimumAgeDays,
		ScannerResultPath:     *scannerResultPath,
		ScannerName:           *scannerName,
		ScannerVersion:        *scannerVersion,
		ScannerDatabaseDigest: *scannerDatabaseDigest,
		SignaturePath:         *signaturePath,
		ProvenancePath:        *provenancePath,
		ZotURL:                *zotURL,
		ZotRepository:         *zotRepository,
		ZotUsername:           *zotUsername,
		ZotPasswordFile:       *zotPasswordFile,
		TUFTargetPath:         *tufTargetPath,
		GUACSubject:           *guacSubject,
	}
	if err := req.validate(); err != nil {
		fmt.Fprintf(os.Stderr, "verself-deploy artifacts admit-url: %v\n", err)
		return 2
	}
	snap := identity.FromEnv()
	if snap.RunKey() == "" {
		generated, err := identity.Generate(identity.GenerateOptions{
			Site:  *site,
			Scope: "artifacts",
			Kind:  "artifact-admission",
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "verself-deploy artifacts admit-url: derive identity: %v\n", err)
			return 1
		}
		generated.ApplyEnv()
		snap = generated
	}
	rt, err := runtime.Init(context.Background(), runtime.Options{
		ServiceName:    serviceName,
		ServiceVersion: serviceVersion,
		Site:           *site,
		RepoRoot:       rr,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "verself-deploy artifacts admit-url: %v\n", err)
		return 1
	}
	defer rt.Close()
	out, err := admitArtifactURL(rt.Ctx, rt, snap.RunKey(), req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "verself-deploy artifacts admit-url: %v\n", err)
		return 1
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(out); err != nil {
		fmt.Fprintf(os.Stderr, "verself-deploy artifacts admit-url: encode: %v\n", err)
		return 1
	}
	return 0
}

type artifactAdmissionRequest struct {
	Site                  string
	Artifact              string
	SourcePath            string
	SourceKind            string
	Surface               string
	UpstreamURL           string
	ExpectedDigest        string
	ReleasedAtRaw         string
	ObservedAtRaw         string
	MinimumAgeDays        int
	ScannerResultPath     string
	ScannerName           string
	ScannerVersion        string
	ScannerDatabaseDigest string
	SignaturePath         string
	ProvenancePath        string
	ZotURL                string
	ZotRepository         string
	ZotUsername           string
	ZotPasswordFile       string
	TUFTargetPath         string
	GUACSubject           string
}

func (r artifactAdmissionRequest) validate() error {
	var missing []string
	for name, value := range map[string]string{
		"--artifact":          r.Artifact,
		"--source-path":       r.SourcePath,
		"--source-kind":       r.SourceKind,
		"--surface":           r.Surface,
		"--upstream-url":      r.UpstreamURL,
		"--digest":            r.ExpectedDigest,
		"--released-at":       r.ReleasedAtRaw,
		"--scanner-result":    r.ScannerResultPath,
		"--scanner-name":      r.ScannerName,
		"--scanner-version":   r.ScannerVersion,
		"--signature":         r.SignaturePath,
		"--provenance":        r.ProvenancePath,
		"--zot-url":           r.ZotURL,
		"--zot-username":      r.ZotUsername,
		"--zot-password-file": r.ZotPasswordFile,
	} {
		if strings.TrimSpace(value) == "" {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing required flags: %s", strings.Join(missing, ", "))
	}
	if _, err := parseSHA256Digest(r.ExpectedDigest); err != nil {
		return err
	}
	if r.ScannerDatabaseDigest != "" {
		if _, err := parseSHA256Digest(r.ScannerDatabaseDigest); err != nil {
			return fmt.Errorf("--scanner-database-digest: %w", err)
		}
	}
	if r.MinimumAgeDays < 0 {
		return errors.New("--minimum-age-days must be >= 0")
	}
	parsed, err := url.Parse(r.UpstreamURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return fmt.Errorf("--upstream-url must be an absolute URL: %q", r.UpstreamURL)
	}
	base, err := url.Parse(r.ZotURL)
	if err != nil || base.Scheme == "" || base.Host == "" {
		return fmt.Errorf("--zot-url must be an absolute URL: %q", r.ZotURL)
	}
	return nil
}

func admitArtifactURL(ctx context.Context, rt *runtime.Runtime, runKey string, req artifactAdmissionRequest) (artifactAdmissionOutput, error) {
	ctx, span := rt.Tracer.Start(ctx, "verself_deploy.artifacts.admit",
		trace.WithSpanKind(trace.SpanKindInternal),
		trace.WithAttributes(
			attribute.String("verself.site", req.Site),
			attribute.String("verself.deploy_run_key", runKey),
			attribute.String("artifact.name", req.Artifact),
			attribute.String("artifact.upstream_url", req.UpstreamURL),
		),
	)
	defer span.End()
	releasedAt, observedAt, err := parseAdmissionTimes(req.ReleasedAtRaw, req.ObservedAtRaw)
	if err != nil {
		return artifactAdmissionOutput{}, recordSpanError(span, err)
	}
	if observedAt.Sub(releasedAt) < time.Duration(req.MinimumAgeDays)*24*time.Hour {
		err := fmt.Errorf("minimum age failed: released_at=%s observed_at=%s minimum_age_days=%d", releasedAt.Format(time.RFC3339), observedAt.Format(time.RFC3339), req.MinimumAgeDays)
		return artifactAdmissionOutput{}, recordSpanError(span, err)
	}
	fetched, cleanup, err := fetchUpstreamArtifact(ctx, req.UpstreamURL, req.ExpectedDigest)
	if err != nil {
		return artifactAdmissionOutput{}, recordSpanError(span, err)
	}
	defer cleanup()
	scannerBytes, scannerDigest, err := readEvidenceFile(req.ScannerResultPath)
	if err != nil {
		return artifactAdmissionOutput{}, recordSpanError(span, fmt.Errorf("scanner result: %w", err))
	}
	signatureBytes, signatureDigest, err := readEvidenceFile(req.SignaturePath)
	if err != nil {
		return artifactAdmissionOutput{}, recordSpanError(span, fmt.Errorf("signature: %w", err))
	}
	provenanceBytes, provenanceDigest, err := readEvidenceFile(req.ProvenancePath)
	if err != nil {
		return artifactAdmissionOutput{}, recordSpanError(span, fmt.Errorf("provenance: %w", err))
	}
	sbomBytes, err := generateCycloneDXSBOM(req, observedAt, fetched.Size)
	if err != nil {
		return artifactAdmissionOutput{}, recordSpanError(span, err)
	}
	sbomDigest := digestBytes(sbomBytes)
	attestationDigest := provenanceDigest
	if req.ZotRepository == "" {
		req.ZotRepository = "admitted/" + sanitizeArtifactPath(req.Artifact)
	}
	if req.TUFTargetPath == "" {
		req.TUFTargetPath = path.Join("admitted", sanitizeArtifactPath(req.Artifact), strings.TrimPrefix(req.ExpectedDigest, "sha256:"))
	}
	if req.GUACSubject == "" {
		req.GUACSubject = "pkg:generic/verself/" + sanitizeArtifactPath(req.Artifact) + "@" + req.ExpectedDigest
	}
	admissionConfig := artifactAdmissionConfig{
		Artifact:              req.Artifact,
		SourcePath:            req.SourcePath,
		SourceKind:            req.SourceKind,
		Surface:               req.Surface,
		UpstreamURL:           req.UpstreamURL,
		UpstreamDigest:        req.ExpectedDigest,
		ReleasedAt:            releasedAt.Format(time.RFC3339Nano),
		ObservedAt:            observedAt.Format(time.RFC3339Nano),
		MinimumAgeResult:      "passed",
		ScannerName:           req.ScannerName,
		ScannerVersion:        req.ScannerVersion,
		ScannerDatabaseDigest: req.ScannerDatabaseDigest,
		ScannerResultDigest:   scannerDigest,
		SBOMDigest:            sbomDigest,
		ProvenanceDigest:      provenanceDigest,
		SignatureDigest:       signatureDigest,
		AttestationDigest:     attestationDigest,
		GUACSubject:           req.GUACSubject,
	}
	configBytes, err := json.Marshal(admissionConfig)
	if err != nil {
		return artifactAdmissionOutput{}, recordSpanError(span, fmt.Errorf("marshal admission config: %w", err))
	}
	configDigest := digestBytes(configBytes)
	zotBase, zotStorageHost, closeZot, err := resolveZotEndpoint(ctx, rt, req.ZotURL)
	if err != nil {
		return artifactAdmissionOutput{}, recordSpanError(span, err)
	}
	defer func() { _ = closeZot() }()
	zotPassword, err := readZotPublisherPassword(ctx, rt, req.ZotPasswordFile)
	if err != nil {
		return artifactAdmissionOutput{}, recordSpanError(span, err)
	}
	auth := basicAuth{Username: req.ZotUsername, Password: zotPassword}
	manifestBytes, manifestDigest, err := publishAdmittedOCIArtifact(ctx, zotBase, req.ZotRepository, auth, []blobContent{
		{
			MediaType: artifactConfigMediaType,
			Digest:    configDigest,
			Size:      int64(len(configBytes)),
			Open:      bytesReadCloser(configBytes),
		},
		{
			MediaType: artifactBytesMediaType,
			Digest:    req.ExpectedDigest,
			Size:      fetched.Size,
			Open:      openFileReadCloser(fetched.Path),
			Annotations: map[string]string{
				"org.opencontainers.image.source": req.UpstreamURL,
			},
		},
		{
			MediaType: artifactSBOMMediaType,
			Digest:    sbomDigest,
			Size:      int64(len(sbomBytes)),
			Open:      bytesReadCloser(sbomBytes),
		},
		{
			MediaType: artifactProvenanceMediaType,
			Digest:    provenanceDigest,
			Size:      int64(len(provenanceBytes)),
			Open:      bytesReadCloser(provenanceBytes),
		},
		{
			MediaType: artifactSignatureMediaType,
			Digest:    signatureDigest,
			Size:      int64(len(signatureBytes)),
			Open:      bytesReadCloser(signatureBytes),
		},
		{
			MediaType: artifactScannerMediaType,
			Digest:    scannerDigest,
			Size:      int64(len(scannerBytes)),
			Open:      bytesReadCloser(scannerBytes),
		},
	}, req, configDigest)
	if err != nil {
		return artifactAdmissionOutput{}, recordSpanError(span, err)
	}
	storageURI := "oci://" + zotStorageHost + "/" + req.ZotRepository + "@" + manifestDigest
	sbomURI := storageURI + "#sbom"
	provenanceURI := storageURI + "#provenance"
	metadata := artifactAdmissionMetadata{
		State:                 "admitted",
		UpstreamURL:           req.UpstreamURL,
		Digest:                req.ExpectedDigest,
		ReleasedAt:            releasedAt.Format(time.RFC3339Nano),
		ObservedAt:            observedAt.Format(time.RFC3339Nano),
		MinimumAgeResult:      "passed",
		ScannerResults:        "passed",
		ScannerName:           req.ScannerName,
		ScannerVersion:        req.ScannerVersion,
		ScannerDatabaseDigest: req.ScannerDatabaseDigest,
		SBOMURI:               sbomURI,
		SBOMDigest:            sbomDigest,
		ProvenanceURI:         provenanceURI,
		ProvenanceDigest:      provenanceDigest,
		TUFTargetPath:         req.TUFTargetPath,
		StorageURI:            storageURI,
		OCIRepository:         req.ZotRepository,
		OCIManifestDigest:     manifestDigest,
		OCIMediaType:          ociImageManifestMediaType,
		SignatureDigest:       signatureDigest,
		AttestationDigest:     attestationDigest,
		ScannerResultDigest:   scannerDigest,
		GUACSubject:           req.GUACSubject,
	}
	traceID, spanID := spanIDs(span.SpanContext())
	row := deploydb.ArtifactAdmissionEventRow{
		EventAt:               time.Now().UTC(),
		DeployRunKey:          runKey,
		Site:                  req.Site,
		Artifact:              req.Artifact,
		SourcePath:            req.SourcePath,
		SourceKind:            req.SourceKind,
		UpstreamURL:           req.UpstreamURL,
		UpstreamDigest:        req.ExpectedDigest,
		ReleasedAt:            releasedAt,
		ObservedAt:            observedAt,
		MinimumAgeResult:      "passed",
		OCIRepository:         req.ZotRepository,
		OCIManifestDigest:     manifestDigest,
		OCIMediaType:          ociImageManifestMediaType,
		SignatureDigest:       signatureDigest,
		AttestationDigest:     attestationDigest,
		SBOMDigest:            sbomDigest,
		ProvenanceDigest:      provenanceDigest,
		ScannerResultDigest:   scannerDigest,
		ScannerName:           req.ScannerName,
		ScannerVersion:        req.ScannerVersion,
		ScannerDatabaseDigest: req.ScannerDatabaseDigest,
		TUFTargetPath:         req.TUFTargetPath,
		StorageURI:            storageURI,
		PolicyResult:          "accepted",
		PolicyReason:          "digest, minimum age, scanner evidence, signature/provenance evidence, and zot publication satisfied",
		GUACSubject:           req.GUACSubject,
		TraceID:               traceID,
		SpanID:                spanID,
		Evidence:              string(manifestBytes),
	}
	if err := rt.DeployDB.InsertArtifactAdmissionEvents(ctx, []deploydb.ArtifactAdmissionEventRow{row}); err != nil {
		return artifactAdmissionOutput{}, recordSpanError(span, err)
	}
	span.SetAttributes(
		attribute.String("artifact.oci_repository", req.ZotRepository),
		attribute.String("artifact.oci_manifest_digest", manifestDigest),
		attribute.String("artifact.storage_uri", storageURI),
	)
	span.SetStatus(codes.Ok, "")
	return artifactAdmissionOutput{Admission: metadata}, nil
}

type fetchedArtifact struct {
	Path string
	Size int64
}

func fetchUpstreamArtifact(ctx context.Context, rawURL, expectedDigest string) (fetchedArtifact, func(), error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return fetchedArtifact{}, nil, fmt.Errorf("build upstream request: %w", err)
	}
	client := &http.Client{Timeout: 2 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return fetchedArtifact{}, nil, fmt.Errorf("fetch upstream bytes: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fetchedArtifact{}, nil, fmt.Errorf("fetch upstream bytes: %s", resp.Status)
	}
	tmp, err := os.CreateTemp("", "verself-artifact-*")
	if err != nil {
		return fetchedArtifact{}, nil, fmt.Errorf("create temp artifact: %w", err)
	}
	cleanup := func() { _ = os.Remove(tmp.Name()) }
	hash := sha256.New()
	n, err := io.Copy(io.MultiWriter(tmp, hash), resp.Body)
	closeErr := tmp.Close()
	if err != nil {
		cleanup()
		return fetchedArtifact{}, nil, fmt.Errorf("write upstream artifact: %w", err)
	}
	if closeErr != nil {
		cleanup()
		return fetchedArtifact{}, nil, fmt.Errorf("close temp artifact: %w", closeErr)
	}
	actual := "sha256:" + hex.EncodeToString(hash.Sum(nil))
	if actual != expectedDigest {
		cleanup()
		return fetchedArtifact{}, nil, fmt.Errorf("digest mismatch: expected %s, observed %s", expectedDigest, actual)
	}
	return fetchedArtifact{Path: tmp.Name(), Size: n}, cleanup, nil
}

func publishAdmittedOCIArtifact(ctx context.Context, base *url.URL, repo string, auth basicAuth, blobs []blobContent, req artifactAdmissionRequest, configDigest string) ([]byte, string, error) {
	for _, blob := range blobs {
		if err := uploadOCIBlob(ctx, base, repo, auth, blob); err != nil {
			return nil, "", err
		}
	}
	layers := make([]ociDescriptor, 0, len(blobs)-1)
	var config ociDescriptor
	for i, blob := range blobs {
		desc := ociDescriptor{
			MediaType:   blob.MediaType,
			Digest:      blob.Digest,
			Size:        blob.Size,
			Annotations: blob.Annotations,
		}
		if i == 0 {
			config = desc
			continue
		}
		layers = append(layers, desc)
	}
	if config.Digest != configDigest {
		return nil, "", errors.New("artifact config digest mismatch before manifest publication")
	}
	manifest := ociManifest{
		SchemaVersion: 2,
		MediaType:     ociImageManifestMediaType,
		Config:        config,
		Layers:        layers,
		Annotations: map[string]string{
			"dev.verself.artifact.name":         req.Artifact,
			"dev.verself.artifact.source":       req.SourcePath,
			"dev.verself.artifact.sourceKind":   req.SourceKind,
			"dev.verself.artifact.surface":      req.Surface,
			"org.opencontainers.image.source":   req.UpstreamURL,
			"org.opencontainers.image.ref.name": strings.ReplaceAll(req.ExpectedDigest, ":", "-"),
		},
	}
	manifestBytes, err := json.Marshal(manifest)
	if err != nil {
		return nil, "", fmt.Errorf("marshal OCI manifest: %w", err)
	}
	manifestDigest := digestBytes(manifestBytes)
	ref := strings.ReplaceAll(req.ExpectedDigest, ":", "-")
	target := registryEndpoint(base, "v2", repo, "manifests", ref)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPut, target, bytes.NewReader(manifestBytes))
	if err != nil {
		return nil, "", fmt.Errorf("build OCI manifest request: %w", err)
	}
	httpReq.Header.Set("Content-Type", ociImageManifestMediaType)
	auth.apply(httpReq)
	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return nil, "", fmt.Errorf("publish OCI manifest: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		return nil, "", fmt.Errorf("publish OCI manifest: %s", resp.Status)
	}
	return manifestBytes, manifestDigest, nil
}

func uploadOCIBlob(ctx context.Context, base *url.URL, repo string, auth basicAuth, blob blobContent) error {
	postURL := registryEndpoint(base, "v2", repo, "blobs", "uploads") + "/"
	postReq, err := http.NewRequestWithContext(ctx, http.MethodPost, postURL, nil)
	if err != nil {
		return fmt.Errorf("build OCI blob upload request: %w", err)
	}
	auth.apply(postReq)
	postResp, err := http.DefaultClient.Do(postReq)
	if err != nil {
		return fmt.Errorf("start OCI blob upload: %w", err)
	}
	_ = postResp.Body.Close()
	if postResp.StatusCode != http.StatusAccepted {
		return fmt.Errorf("start OCI blob upload: %s", postResp.Status)
	}
	location := postResp.Header.Get("Location")
	if location == "" {
		return errors.New("start OCI blob upload: missing Location header")
	}
	uploadURL, err := resolveRegistryLocation(base, location)
	if err != nil {
		return err
	}
	query := uploadURL.Query()
	query.Set("digest", blob.Digest)
	uploadURL.RawQuery = query.Encode()
	body, err := blob.Open()
	if err != nil {
		return fmt.Errorf("open OCI blob content: %w", err)
	}
	defer body.Close()
	putReq, err := http.NewRequestWithContext(ctx, http.MethodPut, uploadURL.String(), body)
	if err != nil {
		return fmt.Errorf("build OCI blob commit request: %w", err)
	}
	putReq.Header.Set("Content-Type", mediaTypeOrOctet(blob.MediaType))
	putReq.ContentLength = blob.Size
	auth.apply(putReq)
	putResp, err := http.DefaultClient.Do(putReq)
	if err != nil {
		return fmt.Errorf("commit OCI blob: %w", err)
	}
	defer putResp.Body.Close()
	if putResp.StatusCode != http.StatusCreated {
		return fmt.Errorf("commit OCI blob %s: %s", blob.Digest, putResp.Status)
	}
	return nil
}

func runArtifactsVerifyInstall(args []string) int {
	fs := flag.NewFlagSet("artifacts verify-install", flag.ContinueOnError)
	site := fs.String("site", "prod", "deployment site")
	surface := fs.String("surface", "", "install surface")
	installer := fs.String("installer", "", "installer name")
	artifact := fs.String("artifact", "", "artifact name")
	zotURL := fs.String("zot-url", "http://127.0.0.1:5080", "zot registry base URL")
	ociRepository := fs.String("oci-repository", "", "zot repository path")
	ociManifestDigest := fs.String("oci-manifest-digest", "", "expected OCI manifest digest")
	signatureDigest := fs.String("signature-digest", "", "expected admission signature digest")
	attestationDigest := fs.String("attestation-digest", "", "expected admission attestation digest")
	repoRoot := fs.String("repo-root", "", "verself-sh checkout root (defaults to cwd)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	rr, ok := resolveRepoRoot("verself-deploy artifacts verify-install", *repoRoot)
	if !ok {
		return 1
	}
	req := artifactInstallVerificationRequest{
		Site:              *site,
		Surface:           *surface,
		Installer:         *installer,
		Artifact:          *artifact,
		ZotURL:            *zotURL,
		OCIRepository:     *ociRepository,
		OCIManifestDigest: *ociManifestDigest,
		SignatureDigest:   *signatureDigest,
		AttestationDigest: *attestationDigest,
	}
	if err := req.validate(); err != nil {
		fmt.Fprintf(os.Stderr, "verself-deploy artifacts verify-install: %v\n", err)
		return 2
	}
	snap := identity.FromEnv()
	if snap.RunKey() == "" {
		generated, err := identity.Generate(identity.GenerateOptions{
			Site:  *site,
			Scope: "artifacts",
			Kind:  "artifact-install-verification",
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "verself-deploy artifacts verify-install: derive identity: %v\n", err)
			return 1
		}
		generated.ApplyEnv()
		snap = generated
	}
	rt, err := runtime.Init(context.Background(), runtime.Options{
		ServiceName:    serviceName,
		ServiceVersion: serviceVersion,
		Site:           *site,
		RepoRoot:       rr,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "verself-deploy artifacts verify-install: %v\n", err)
		return 1
	}
	defer rt.Close()
	if err := verifyArtifactInstall(rt.Ctx, rt, snap.RunKey(), req); err != nil {
		fmt.Fprintf(os.Stderr, "verself-deploy artifacts verify-install: %v\n", err)
		return 1
	}
	fmt.Printf("artifact install verification ok: artifact=%s oci_repository=%s oci_manifest_digest=%s\n", req.Artifact, req.OCIRepository, req.OCIManifestDigest)
	return 0
}

type artifactInstallVerificationRequest struct {
	Site              string
	Surface           string
	Installer         string
	Artifact          string
	ZotURL            string
	OCIRepository     string
	OCIManifestDigest string
	SignatureDigest   string
	AttestationDigest string
}

func (r artifactInstallVerificationRequest) validate() error {
	var missing []string
	for name, value := range map[string]string{
		"--surface":             r.Surface,
		"--installer":           r.Installer,
		"--artifact":            r.Artifact,
		"--zot-url":             r.ZotURL,
		"--oci-repository":      r.OCIRepository,
		"--oci-manifest-digest": r.OCIManifestDigest,
		"--signature-digest":    r.SignatureDigest,
		"--attestation-digest":  r.AttestationDigest,
	} {
		if strings.TrimSpace(value) == "" {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing required flags: %s", strings.Join(missing, ", "))
	}
	for name, value := range map[string]string{
		"--oci-manifest-digest": r.OCIManifestDigest,
		"--signature-digest":    r.SignatureDigest,
		"--attestation-digest":  r.AttestationDigest,
	} {
		if _, err := parseSHA256Digest(value); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
	}
	base, err := url.Parse(r.ZotURL)
	if err != nil || base.Scheme == "" || base.Host == "" {
		return fmt.Errorf("--zot-url must be an absolute URL: %q", r.ZotURL)
	}
	return nil
}

func verifyArtifactInstall(ctx context.Context, rt *runtime.Runtime, runKey string, req artifactInstallVerificationRequest) error {
	ctx, span := rt.Tracer.Start(ctx, "verself_deploy.artifacts.install_verify",
		trace.WithSpanKind(trace.SpanKindInternal),
		trace.WithAttributes(
			attribute.String("verself.site", req.Site),
			attribute.String("verself.deploy_run_key", runKey),
			attribute.String("artifact.name", req.Artifact),
			attribute.String("artifact.oci_repository", req.OCIRepository),
			attribute.String("artifact.oci_manifest_digest", req.OCIManifestDigest),
		),
	)
	defer span.End()
	zotBase, zotStorageHost, closeZot, err := resolveZotEndpoint(ctx, rt, req.ZotURL)
	if err != nil {
		return recordSpanError(span, err)
	}
	defer func() { _ = closeZot() }()
	manifestURL := registryEndpoint(zotBase, "v2", req.OCIRepository, "manifests", req.OCIManifestDigest)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, manifestURL, nil)
	if err != nil {
		return recordSpanError(span, fmt.Errorf("build OCI manifest fetch request: %w", err))
	}
	httpReq.Header.Set("Accept", ociImageManifestMediaType)
	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return recordSpanError(span, fmt.Errorf("fetch OCI manifest: %w", err))
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return recordSpanError(span, fmt.Errorf("fetch OCI manifest: %s", resp.Status))
	}
	payload, err := io.ReadAll(resp.Body)
	if err != nil {
		return recordSpanError(span, fmt.Errorf("read OCI manifest: %w", err))
	}
	actualDigest := digestBytes(payload)
	if actualDigest != req.OCIManifestDigest {
		return recordSpanError(span, fmt.Errorf("OCI manifest digest mismatch: expected %s, observed %s", req.OCIManifestDigest, actualDigest))
	}
	traceID, spanID := spanIDs(span.SpanContext())
	ociReference := "oci://" + zotStorageHost + "/" + req.OCIRepository + "@" + req.OCIManifestDigest
	row := deploydb.ArtifactInstallVerificationEventRow{
		EventAt:           time.Now().UTC(),
		DeployRunKey:      runKey,
		Site:              req.Site,
		Surface:           req.Surface,
		Installer:         req.Installer,
		Artifact:          req.Artifact,
		OCIReference:      ociReference,
		OCIRepository:     req.OCIRepository,
		OCIManifestDigest: req.OCIManifestDigest,
		SignatureDigest:   req.SignatureDigest,
		AttestationDigest: req.AttestationDigest,
		PolicyResult:      "accepted",
		PolicyReason:      "digest-addressed OCI manifest fetched and matched expected admission metadata",
		TraceID:           traceID,
		SpanID:            spanID,
		Evidence:          string(payload),
	}
	if err := rt.DeployDB.InsertArtifactInstallVerificationEvents(ctx, []deploydb.ArtifactInstallVerificationEventRow{row}); err != nil {
		return recordSpanError(span, err)
	}
	span.SetAttributes(attribute.String("artifact.oci_reference", ociReference))
	span.SetStatus(codes.Ok, "")
	return nil
}

func runArtifactsAssertEvidence(args []string) int {
	fs := flag.NewFlagSet("artifacts assert-evidence", flag.ContinueOnError)
	site := fs.String("site", "prod", "deployment site")
	repoRoot := fs.String("repo-root", "", "verself-sh checkout root (defaults to cwd)")
	runKey := fs.String("run-key", "", "deploy run key to assert")
	expectedAdmissions := fs.Int("expected-admissions", 1, "expected artifact admission event count")
	expectedInstallVerifications := fs.Int("expected-install-verifications", 0, "expected artifact install verification event count")
	wait := fs.Duration("wait", 5*time.Second, "maximum time to wait for ClickHouse evidence")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *runKey == "" {
		fmt.Fprintln(os.Stderr, "verself-deploy artifacts assert-evidence: --run-key is required")
		return 2
	}
	if *wait <= 0 || *wait > 5*time.Second {
		fmt.Fprintln(os.Stderr, "verself-deploy artifacts assert-evidence: --wait must be >0 and <=5s")
		return 2
	}
	rr, ok := resolveRepoRoot("verself-deploy artifacts assert-evidence", *repoRoot)
	if !ok {
		return 1
	}
	generated, err := identity.Generate(identity.GenerateOptions{
		Site:  *site,
		Scope: "artifacts",
		Kind:  "artifact-assert-evidence",
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "verself-deploy artifacts assert-evidence: derive identity: %v\n", err)
		return 1
	}
	generated.ApplyEnv()
	rt, err := runtime.Init(context.Background(), runtime.Options{
		ServiceName:    serviceName,
		ServiceVersion: serviceVersion,
		Site:           *site,
		RepoRoot:       rr,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "verself-deploy artifacts assert-evidence: %v\n", err)
		return 1
	}
	defer rt.Close()
	summary, err := waitForArtifactEvidence(rt.Ctx, rt, *runKey, *expectedAdmissions, *expectedInstallVerifications, *wait)
	if err != nil {
		fmt.Fprintf(os.Stderr, "verself-deploy artifacts assert-evidence: %v\n", err)
		return 1
	}
	fmt.Printf(
		"artifact admission evidence ok: run_key=%s admissions=%d installs=%d rejected=%d empty_trace_ids=%d distinct_trace_ids=%d admission_spans=%d install_spans=%d trace_id=%s\n",
		summary.DeployRunKey,
		summary.AdmissionRows,
		summary.InstallRows,
		summary.RejectedAdmissions,
		summary.EmptyTraceID,
		summary.DistinctTraceID,
		summary.AdmissionSpans,
		summary.InstallSpans,
		summary.TraceID,
	)
	return 0
}

func waitForArtifactEvidence(ctx context.Context, rt *runtime.Runtime, runKey string, expectedAdmissions, expectedInstallVerifications int, wait time.Duration) (deploydb.ArtifactEvidenceSummary, error) {
	if rt == nil || rt.DeployDB == nil {
		return deploydb.ArtifactEvidenceSummary{}, errors.New("runtime ClickHouse client is required")
	}
	deadline := time.Now().Add(wait)
	var lastErr error
	for {
		summary, err := rt.DeployDB.ArtifactEvidenceSummary(ctx, runKey)
		if err != nil {
			lastErr = err
		} else if err := validateArtifactEvidence(summary, expectedAdmissions, expectedInstallVerifications); err != nil {
			lastErr = err
		} else {
			return summary, nil
		}
		if time.Now().After(deadline) {
			return deploydb.ArtifactEvidenceSummary{}, lastErr
		}
		timer := time.NewTimer(250 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return deploydb.ArtifactEvidenceSummary{}, ctx.Err()
		case <-timer.C:
		}
	}
}

func validateArtifactEvidence(summary deploydb.ArtifactEvidenceSummary, expectedAdmissions, expectedInstallVerifications int) error {
	var issues []string
	if summary.AdmissionRows != uint64(expectedAdmissions) {
		issues = append(issues, fmt.Sprintf("expected %d artifact admission rows, observed %d", expectedAdmissions, summary.AdmissionRows))
	}
	if summary.InstallRows != uint64(expectedInstallVerifications) {
		issues = append(issues, fmt.Sprintf("expected %d artifact install verification rows, observed %d", expectedInstallVerifications, summary.InstallRows))
	}
	if summary.RejectedAdmissions != 0 {
		issues = append(issues, fmt.Sprintf("expected zero rejected artifact admissions, observed %d", summary.RejectedAdmissions))
	}
	if summary.EmptyTraceID != 0 {
		issues = append(issues, fmt.Sprintf("expected zero empty trace IDs, observed %d", summary.EmptyTraceID))
	}
	if summary.DistinctTraceID != 1 {
		issues = append(issues, fmt.Sprintf("expected one artifact admission trace ID, observed %d", summary.DistinctTraceID))
	}
	if summary.AdmissionSpans == 0 {
		issues = append(issues, "missing OK artifacts.admit span")
	}
	if expectedInstallVerifications > 0 && summary.InstallSpans == 0 {
		issues = append(issues, "missing OK artifacts.install_verify span")
	}
	if len(issues) > 0 {
		return errors.New(strings.Join(issues, "; "))
	}
	return nil
}

func parseAdmissionTimes(releasedAtRaw, observedAtRaw string) (time.Time, time.Time, error) {
	releasedAt, err := time.Parse(time.RFC3339, releasedAtRaw)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("parse --released-at: %w", err)
	}
	observedAt := time.Now().UTC()
	if observedAtRaw != "" {
		observedAt, err = time.Parse(time.RFC3339, observedAtRaw)
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("parse --observed-at: %w", err)
		}
	}
	return releasedAt.UTC(), observedAt.UTC(), nil
}

func parseSHA256Digest(value string) (string, error) {
	value = strings.TrimSpace(value)
	if !strings.HasPrefix(value, "sha256:") {
		return "", fmt.Errorf("digest must be formatted sha256:<hex>, got %q", value)
	}
	hexPart := strings.TrimPrefix(value, "sha256:")
	if len(hexPart) != 64 {
		return "", fmt.Errorf("sha256 digest must contain 64 hex characters, got %d", len(hexPart))
	}
	if _, err := hex.DecodeString(hexPart); err != nil {
		return "", fmt.Errorf("sha256 digest has invalid hex: %w", err)
	}
	return value, nil
}

func readEvidenceFile(path string) ([]byte, string, error) {
	payload, err := os.ReadFile(path)
	if err != nil {
		return nil, "", err
	}
	if len(bytes.TrimSpace(payload)) == 0 {
		return nil, "", errors.New("evidence file is empty")
	}
	return payload, digestBytes(payload), nil
}

func resolveZotEndpoint(ctx context.Context, rt *runtime.Runtime, rawURL string) (*url.URL, string, func() error, error) {
	base, err := url.Parse(rawURL)
	if err != nil || base.Scheme == "" || base.Host == "" {
		return nil, "", nil, fmt.Errorf("--zot-url must be an absolute URL: %q", rawURL)
	}
	storageHost := base.Host
	if !isLoopbackHost(base.Hostname()) {
		return base, storageHost, func() error { return nil }, nil
	}
	port := 80
	if base.Scheme == "https" {
		port = 443
	}
	if rawPort := base.Port(); rawPort != "" {
		parsed, err := strconv.Atoi(rawPort)
		if err != nil || parsed <= 0 || parsed > 65535 {
			return nil, "", nil, fmt.Errorf("parse zot port %q", rawPort)
		}
		port = parsed
	}
	forward, err := rt.SSH.Forward(ctx, "zot", port)
	if err != nil {
		return nil, "", nil, fmt.Errorf("open zot SSH forward: %w", err)
	}
	forwarded := *base
	forwarded.Host = forward.ListenAddr
	return &forwarded, storageHost, forward.Close, nil
}

func isLoopbackHost(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func readZotPublisherPassword(ctx context.Context, rt *runtime.Runtime, passwordPath string) (string, error) {
	if !filepath.IsAbs(passwordPath) {
		return readSecretFile(passwordPath)
	}
	payload, err := rt.SSH.Exec(ctx, "sudo /bin/cat -- "+strconv.Quote(passwordPath))
	if err != nil {
		return "", fmt.Errorf("read remote zot publisher password %s: %w", passwordPath, err)
	}
	secret := strings.TrimSpace(string(payload))
	if secret == "" {
		return "", fmt.Errorf("remote zot publisher password %s is empty", passwordPath)
	}
	return secret, nil
}

func readSecretFile(path string) (string, error) {
	payload, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read secret file %s: %w", path, err)
	}
	secret := strings.TrimSpace(string(payload))
	if secret == "" {
		return "", fmt.Errorf("secret file %s is empty", path)
	}
	return secret, nil
}

func generateCycloneDXSBOM(req artifactAdmissionRequest, observedAt time.Time, size int64) ([]byte, error) {
	hexDigest := strings.TrimPrefix(req.ExpectedDigest, "sha256:")
	doc := map[string]any{
		"bomFormat":   "CycloneDX",
		"specVersion": "1.6",
		"version":     1,
		"metadata": map[string]any{
			"timestamp": observedAt.Format(time.RFC3339Nano),
			"tools": []map[string]any{
				{
					"vendor": "verself",
					"name":   "verself-deploy artifact admission",
				},
			},
			"component": map[string]any{
				"type":    "file",
				"name":    req.Artifact,
				"version": req.ExpectedDigest,
				"hashes": []map[string]string{
					{"alg": "SHA-256", "content": hexDigest},
				},
				"externalReferences": []map[string]string{
					{"type": "distribution", "url": req.UpstreamURL},
				},
			},
		},
		"components": []map[string]any{
			{
				"type":    "file",
				"name":    req.Artifact,
				"version": req.ExpectedDigest,
				"hashes": []map[string]string{
					{"alg": "SHA-256", "content": hexDigest},
				},
				"properties": []map[string]string{
					{"name": "verself:artifact:size_bytes", "value": fmt.Sprintf("%d", size)},
					{"name": "verself:artifact:surface", "value": req.Surface},
					{"name": "verself:artifact:source_kind", "value": req.SourceKind},
				},
			},
		},
	}
	return json.Marshal(doc)
}

func digestBytes(payload []byte) string {
	sum := sha256.Sum256(payload)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func bytesReadCloser(payload []byte) func() (io.ReadCloser, error) {
	return func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(payload)), nil
	}
}

func openFileReadCloser(path string) func() (io.ReadCloser, error) {
	return func() (io.ReadCloser, error) {
		return os.Open(path)
	}
}

type basicAuth struct {
	Username string
	Password string
}

func (a basicAuth) apply(req *http.Request) {
	if a.Username == "" && a.Password == "" {
		return
	}
	req.SetBasicAuth(a.Username, a.Password)
}

func registryEndpoint(base *url.URL, parts ...string) string {
	clone := *base
	var escaped []string
	for _, part := range parts {
		for _, segment := range strings.Split(part, "/") {
			if segment == "" {
				continue
			}
			escaped = append(escaped, url.PathEscape(segment))
		}
	}
	prefix := strings.TrimRight(clone.EscapedPath(), "/")
	clone.Path = prefix + "/" + strings.Join(escaped, "/")
	clone.RawPath = ""
	return clone.String()
}

func resolveRegistryLocation(base *url.URL, location string) (*url.URL, error) {
	parsed, err := url.Parse(location)
	if err != nil {
		return nil, fmt.Errorf("parse OCI upload location: %w", err)
	}
	return base.ResolveReference(parsed), nil
}

func mediaTypeOrOctet(value string) string {
	if value == "" {
		return "application/octet-stream"
	}
	if _, _, err := mime.ParseMediaType(value); err != nil {
		return "application/octet-stream"
	}
	return value
}

func sanitizeArtifactPath(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	replacer := strings.NewReplacer(":", "-", "@", "-", "_", "-", " ", "-", ".", "-")
	value = replacer.Replace(value)
	var out strings.Builder
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '/' {
			out.WriteRune(r)
		}
	}
	cleaned := path.Clean(out.String())
	cleaned = strings.Trim(cleaned, "./")
	if cleaned == "" || cleaned == "." {
		return "artifact"
	}
	return cleaned
}

func recordSpanError(span trace.Span, err error) error {
	span.RecordError(err)
	span.SetStatus(codes.Error, err.Error())
	return err
}

func spanIDs(spanCtx trace.SpanContext) (string, string) {
	if !spanCtx.IsValid() {
		return "", ""
	}
	return spanCtx.TraceID().String(), spanCtx.SpanID().String()
}
